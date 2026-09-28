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

**⚠️ 关于 D.2 结论 3 的归因（已在独立复核后修正，见下）**

我最初把"bind 成功但收不到包"归因于"Go 不设置 `SO_REUSEADDR`"。**该归因是错误的，已撤回。**
正确解释见本节末尾的 **[D.6 更正记录](#d6-更正记录重要)**。

**→ 仍然成立的架构要求（但理由不同）**：`discovery/` 模块**应当显式管理 socket 与组播接口**——
不是因为标准库有缺陷，而是因为**默认接口选择在本机是错的**（见 D.3），
且 `net.ListenMulticastUDP` 会关闭 `IP_MULTICAST_LOOP`。**这是稳健性选择，不是绕开库缺陷。**

## D.3 附带确认：接口选择风险是真实的（**这才是真正的根因**）

收到的回包来源是 **`10.2.3.27`（`optun` 接口）**，而不是主 LAN 的 `192.168.110.40`（以太网）。

**这印证了附录 C.2 的担忧**：本机有多个网段，组播默认接口**不是**主 LAN 接口。
而不显式指定接口时（`ifi == nil`），`net.ListenMulticastUDP` **只在系统默认接口上加入组播组**。
在本机上那就是 `optun` —— **加入了错误的组播组，自然收不到主 LAN 上的包**，
这恰好完整解释了观察到的"bind 成功、静默收不到包"现象。

**→ 架构要求：`discovery/` 必须支持按接口显式指定组播发送/加入（`IP_MULTICAST_IF` + per-interface `IP_ADD_MEMBERSHIP`），
并提供接口白/黑名单配置。** 这从"可选优化"升级为**必需功能**（本机实测默认走错接口）。

## D.4 对架构决策的净影响

| 项 | 变化 |
| --- | --- |
| P0 #3 状态 | ⚠️ 阻塞 → ✅ **解除**（多进程共存可行） |
| `discovery/` 实现方式 | ✅ **可用标准库**；但**必须显式指定接口**（`ifi` 参数）并考虑开启 `IP_MULTICAST_LOOP` |
| `hashicorp/mdns` 可用性 | ⚠️ **有保留地可用**：`zone.go` 的 PTR+SRV+TXT 构造正确；但 `NewServer` **忽略监听错误**（`ipv4List, _ := ...`），且**不能可靠地按接口固定成员关系**；另需显式传入 FQDN hostName + IP 列表（见 D.6 / [附录 F](08-independent-review.md)） |
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

# 3. 对照实验
net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 5353})   // WSAEADDRINUSE（监听类 socket 在 Windows 上是 no-op）
net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: grp, Port: 5353})
//   ^ bind 成功，但 ifi==nil 时只在“系统默认接口”加入组播组；
//     本机默认接口是 optun，不是主 LAN，因此收不到包 —— 这才是根因（见 D.6）

# 4. 防火墙检查
Get-NetFirewallRule -Direction Inbound -Enabled True |
  Where-Object { $_.DisplayName -match 'mDNS' } | Select-Object DisplayName,Action,Profile
```

## D.6 更正记录（**重要**）

> 本文档原先的 D.2 结论 3 断言："Go 的 `net.ListenMulticastUDP` **不设置 `SO_REUSEADDR`**，
> 因此在 Chrome 占用 5353 时表现为 bind 成功但收不到包"。
>
> **该因果归因经独立复核后被推翻，现已撤回。** 保留此记录以防后人重蹈覆辙。

### 事实：Go **确实**为组播 UDP 设置了 `SO_REUSEADDR`（含 Windows）

一手源码证据（Go 主线，Lead 已亲自抓取核对）：

`src/net/sockopt_windows.go`：

```go
func setDefaultMulticastSockopts(s syscall.Handle) error {
	// Allow multicast UDP and raw IP datagram sockets to listen
	// concurrently across multiple listeners.
	return os.NewSyscallError("setsockopt", syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1))
}
```

`src/net/sock_posix.go` 的 `listenDatagram` 会调用它：

```go
case *UDPAddr:
	if addr.IP != nil && addr.IP.IsMulticast() {
		if err := setDefaultMulticastSockopts(fd.pfd.Sysfd); err != nil {
			return err
		}
		addr := *addr
		switch fd.family {
		case syscall.AF_INET:
			addr.IP = IPv4zero        // 重绑到通配地址
		...
```

调用链：`ListenMulticastUDP` → `internetSocket` → `socket` → `listenDatagram` → `setDefaultMulticastSockopts`。
Linux/BSD 同理（BSD 还额外设 `SO_REUSEPORT`）。
**只有面向流式监听（`setDefaultListenerSockopts`）在 Windows 上才是刻意的 no-op** ——
这恰好解释了为什么 `net.ListenUDP` 会 `WSAEADDRINUSE`，而 `ListenMulticastUDP` 不会。

> 注：`setDefaultListenerSockopts` 在 Windows 上的注释明确写道
> "Windows will reuse recently-used addresses by default. SO_REUSEADDR should not be used here..."
> 这与 D.2 结论 1（自建 socket 设 `SO_REUSEADDR` 成功）**并不矛盾**：
> 我用的是**数据报** socket（`ListenPacket`），走的是 `listenDatagram` 路径。

### 真正的根因：接口选择（而非 `SO_REUSEADDR`）

`net.ListenMulticastUDP` 在 `ifi == nil` 时，**只在系统默认接口上加入组播组**。
本机（D.3 实测）默认组播接口是 **`optun` (10.2.3.27)**，不是主 LAN `以太网` (192.168.110.40)。
**加入错误的组播组 ⇒ 收不到主 LAN 的包 ⇒ 表现为"bind 成功、静默无收包"。**

另外 `udpsock.go` 中 `listenMulticastUDP` 会调用 `setIPv4MulticastLoopback(c.fd, false)`，
即**关闭 `IP_MULTICAST_LOOP`** —— 这解释了同进程/本机自环回测试为何收不到自己的包。

### 结论如何变化

| 项 | 原结论（已撤回） | 更正后 |
| --- | --- | --- |
| 根因 | Go 不设 `SO_REUSEADDR` | **默认组播接口选错**（+ `IP_MULTICAST_LOOP=0`） |
| 能否用 `net.ListenMulticastUDP` | ❌ 不能用 | ✅ **可以用**，但**必须显式传 `ifi`** |
| 对 `hashicorp/mdns` 的判断 | ❌ 不能直接用 `NewServer` | ⚠️ **可用**，但需注意它忽略监听错误、难以按接口固定成员关系 |
| 仍需自建 socket？ | 必须 | **非必须**；若需要精细控制接口/回环才自建 |

**对"共存"这一核心问题的答案不变**：多进程共享 UDP 5353 **可行**（D.2 结论 1/2 的实测输出有效，
未被推翻）。变的只是**归因**与**对标准库的可用性判断**。

### 教训

我最初的多次探针失败**原因各不相同**（Go 单进程内两个 `ListenMulticastUDP` 相互竞争导致
`use of closed network connection`；非 ASCII 接口名传参导致 `no such network interface`）。
**把多个异源失败归并成单一解释，是本次误诊的根源。**
后续所有"某库有缺陷"的结论都必须先读源码再下断言。
