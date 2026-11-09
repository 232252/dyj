# pkg/discovery 设计说明（task-2 实测修正，非文档目录）

> 本文档**不是** `docs/`（属其他任务），只是本包内部的工程笔记 + 实证记录。

## 为什么不用 `hashicorp/mdns.NewServer`（虽然它是可用的）

`mdns.NewServer` 在本机（Chrome 占 5353、多网段、默认组播接口是 `optun`）有三个**不可绕过的**结构性问题：

| 问题 | 源码位置（v1.0.7） | 后果 |
| --- | --- | --- |
| `${IFACE}` 变量替换**不实现** | `server.go` `sendResponse()` 直接 `WriteToUDP(buf, from)`；`zone.go` 里没有 `${IFACE}` 处理器 | 回包走内核选路，源地址不可控 |
| **关闭 `IP_MULTICAST_LOOP`** | `net.ListenMulticastUDP` → `setIPv4MulticastLoopback(fd,false)` | **断言失败**：`main.tcpAddr`（主机名）查不到，回查服务名 `_ipp._tcp.local.` 也查不到，因为服务器收不到自己的组播 → 客户端拿到 SRV 但解析不了主机名 |
| `NewServer` **忽略监听错误** | `ipv4List, _ := net.ListenMulticastUDP(...)` | IPv4 失败会静默降级成"只监听 IPv6" |

另外 `Config.Iface` 只能传一个接口，**不支持多接口白名单**，而本机有 4 个真实网段 + 3 个 APIPA 接口。

## 本包的做法：自管 socket + `golang.org/x/net/ipv4.PacketConn`

用已经存在于依赖树里的 `golang.org/x/net/ipv4` / `ipv6`（不新增依赖）：

- `net.ListenConfig{Control: setSO_REUSEADDR}` 自建 5353 socket（Windows 上与 Chrome 共存，附录 D 已实证）；
- **显式 `IP_ADD_MEMBERSHIP`**，每个白名单接口一次（绕过"默认接口是 optun"这个坑）；
- `SetMulticastInterface(ifi)` —— 控制**出向**组播接口；
- `SetMulticastLoopback(true)` —— 关键：不依赖"自己问自己"来发现；
- `PacketConn.SetControlMessage(FlagDst|FlagInterface, true)` + `WriteTo(cm, ...)`：
  回包时**把源地址/源接口固定成收到包的那个**，等效于 Avahi/CUPS 的 `${IFACE}`。

通告：**主动发一次 unsolicited announcement**（RFC 6762 §8.3，PTR/SRV/TXT/A/AAAA，cache-flush 位），
这样零查询客户端也能看到；之后对任何匹配查询做**定向单播应答**（QU 语义，RFC 6762 §5.4）。

`hashicorp/mdns` 仍保留为依赖，但**只用于本包里那个离线自检浏览器**（`Browse()`，见 `browse.go`），
不参与服务端路径。
