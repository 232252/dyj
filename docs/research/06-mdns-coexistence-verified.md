# 附录 D：P0 #3 实证结论 —— mDNS 与 Chrome 共存（**已解除阻塞**）

> **结论：Chrome 占用 UDP 5353 不构成阻塞。** 但**绝不能使用 Go 的 `net.ListenMulticastUDP`**。
>
> 采集方式：Team Lead 在本机（Windows 11）编写并运行真实 Go 探针程序，实测得到结论。
> 相关背景：[附录 C.5](05-environment-probe.md) 发现 Chrome 占用 5353；[P0 清单](README.md) 将此项列为阻塞项。

## D.1 问题

本机实测发现 **`chrome.exe`（PID 7776）已占用 UDP 5353**（Chrome 内置 mDNS，用于 Cast / 私有网络访问）。
调研简报未覆盖此风险。需回答：我们的 IPP 服务端**还能不能做 DNS-SD 通告**？

## D.2 实测结论（三条，均有输出为证）

### ✅ 结论 1：`SO_REUSEADDR` 可让多进程共享 5353

```
A. bound 0.0.0.0:5353 with SO_REUSEADDR -> OK (Chrome holds 5353 too)
B. SECOND bind on same 5353 -> OK (multi-listener coexistence proven)
```

**两个独立 socket 同时 bind 5353 成功**，且此时 Chrome 仍持有该端口。
→ **共存不是问题。** 附录 C.5 提出的风险**已排除**。

### ✅ 结论 2：显式加入组播组后，组播投递正常

```
C. IP_ADD_MEMBERSHIP 224.0.0.251 -> <nil>
D. sent datagram to 224.0.0.251:5353 from ephemeral port
E. *** socket A RECEIVED 10 bytes from 10.2.3.27:51782 ***
```

**组播收发链路完全通畅**：发送到 `224.0.0.251:5353` 的数据报被成功接收。

### ❌ 结论 3（**最重要的工程发现**）：Go 的 `net.ListenMulticastUDP` **不可用**

实测对比：

| 方式 | 结果 |
| --- | --- |
| `net.ListenUDP("udp4", 0.0.0.0:5353)` | ❌ **FAIL**：`bind: Only one usage of each socket address ... normally permitted`（WSAEADDRINUSE） |
| `net.ListenMulticastUDP(...)` 单独使用 | ⚠️ bind 能成功，**但收不到发往该组的单播/组播回包**（多次实测客户端与响应端均超时） |
| **自建 `net.ListenConfig{Control: setsockopt(SO_REUSEADDR)}` + 显式 `IP_ADD_MEMBERSHIP`** | ✅ **完全成功**（结论 1 与 2） |

**原因**：Go 标准库的 `net.ListenMulticastUDP` **不设置 `SO_REUSEADDR`**。
在 Windows 上，若端口已被 Chrome 占用，它的行为不可靠 —— 会表现为"bind 成功但收不到包"，
这是**最难排查的一类故障**（无报错、静默丢包）。

**→ 架构要求：`discovery/` 模块必须自己用 `ListenConfig.Control` 设置 `SO_REUSEADDR`，
并显式调用 `IP_ADD_MEMBERSHIP`，不能用 `net.ListenMulticastUDP`。**

这也意味着**不能直接用 `hashicorp/mdns` 的 `NewServer`**（它内部用的正是 `net.ListenMulticastUDP`），
需要在它之上提供自定义 socket，或替换 `discovery/` 的实现。

## D.3 附带确认：接口选择风险是真实的

收到的回包来源是 **`10.2.3.27`（`optun` 接口）**，而不是主 LAN 的 `192.168.110.40`（以太网）。

**这印证了附录 C.2 的担忧**：本机有多个网段，组播默认接口**不是**主 LAN 接口。
若不显式指定接口：
- 通告会发到错误子网，**客户端发现不了**；
- 且行为随路由表变化而漂移，**难以复现**。

**→ 架构要求：`discovery/` 必须支持按接口显式指定组播发送/加入（`IP_MULTICAST_IF` + per-interface `IP_ADD_MEMBERSHIP`），
并提供接口白/黑名单配置。** 这从"可选优化"升级为**必需功能**。

## D.4 对架构决策的净影响

| 项 | 变化 |
| --- | --- |
| P0 #3 状态 | ⚠️ 阻塞 → ✅ **解除**（共存可行） |
| `discovery/` 实现方式 | ❌ 不可用 `net.ListenMulticastUDP`；✅ 必须自建 socket（`SO_REUSEADDR` + `IP_ADD_MEMBERSHIP`） |
| `hashicorp/mdns` 可用性 | ⚠️ **打折**：可用其 DNS-SD 记录构造（`zone.go`），但**不能直接用其 `NewServer`**；需自定义 socket 层或自研 |
| 接口选择 | ⚠️ 从"建议支持"升级为**必需功能**（本机实测默认走错接口） |
| 防火墙 | ✅ 已有 `mDNS (UDP-In) Allow` 规则（Domain/Private/Public 全放行），无需额外配置 |

## D.5 采集/复现方式

```powershell
# 1. 确认 5353 被谁占用
Get-NetUDPEndpoint -LocalPort 5353 | Select-Object LocalAddress,LocalPort,OwningProcess
Get-Process -Id <PID> | Select-Object Id,ProcessName,Path

# 2. Go 探针（关键片段）—— 必须自建 socket
lc := net.ListenConfig{Control: func(n, a string, c syscall.RawConn) error {
    var serr error
    c.Control(func(fd uintptr) {
        serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
    })
    return serr
}}
pc, _ := lc.ListenPacket(ctx, "udp4", "0.0.0.0:5353")
uc := pc.(*net.UDPConn)
raw, _ := uc.SyscallConn()
raw.Control(func(fd uintptr) {
    windows.SetsockoptIPMreq(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_ADD_MEMBERSHIP,
        &windows.IPMreq{Multiaddr: [4]byte{224,0,0,251}})
})

# 3. 对照实验（会失败，用于确认不能用标准库方式）
net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 5353})   // WSAEADDRINUSE
net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: grp, Port: 5353}) // bind ok 但收不到包

# 4. 防火墙检查
Get-NetFirewallRule -Direction Inbound -Enabled True |
  Where-Object { $_.DisplayName -match 'mDNS' } | Select-Object DisplayName,Action,Profile
```
