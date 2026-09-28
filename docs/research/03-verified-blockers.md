# 附录 A：对调研简报两个"待验证阻塞项"的实证结论

> 本文档由 Team Lead 直接查证一手源码/API 后写入。
> 调研员 454e317d 在简报 §B.6 把以下两点列为"动工前必须验证的阻塞项"，此处给出确定答案。
> 查证方式：GitHub REST API + `raw.githubusercontent.com` 直接读取源码原文（非搜索摘要）。

---

## 结论 1：`phin1x/go-ipp` **具备服务端能力** ✅（阻塞解除）

**问题**：该库是否只偏客户端，无法用于实现 IPP **服务端**？

**答案**：具备。它同时提供**服务端所需的编解码对**。

实测文件清单（`phin1x/go-ipp`，Apache-2.0，172★，size 136 KB，最后推送 2026-08-09，未归档）：

| 文件 | 用途 | 对应角色 |
| --- | --- | --- |
| `request_decoder.go` | 解析**收到的** IPP 请求 | **服务端必需** |
| `response_encoder.go` | 构造**发出的** IPP 响应 | **服务端必需** |
| `attribute_decoder.go` | 解析属性组 | 双向 |
| `attribute_encoder.go` | 构造属性组 | 双向 |
| `request_encoder.go` | 构造请求 | 客户端 |
| `response_decoder.go` | 解析响应 | 客户端 |
| `client_ipp.go` / `client_cups.go` | 高层客户端封装 | 客户端 |
| `adapter_http.go` / `adapter_socket.go` | 传输适配器 | 双向 |

关键证据（实际读取到的函数签名）：

```go
func NewRequestDecoder(r io.Reader) *RequestDecoder     // 服务端：解析客户端请求
func NewResponseDecoder(reader io.Reader) *ResponseDecoder
func newRequestEncoder(w io.Writer) *requestEncoder     // 注意：小写 = 包内私有
func newResponseEncoder(w io.Writer) *responseEncoder   // 注意：小写 = 包内私有
func NewAttributeDecoder(r io.Reader) *AttributeDecoder
```

**重要细节（必须提前知道）**：`newRequestEncoder` 与 `newResponseEncoder` 是**小写开头的包内私有函数**。
也就是说，从库外**无法直接**调用响应编码器。这意味着：

- 解析客户端请求：**直接用** `NewRequestDecoder`，没问题。
- 构造服务端响应：可能需要**自行补齐**导出入口（fork 后加一行导出包装，或按 RFC 8010 自己写编码器）。
  `Response`/`Request`/`Attribute` 等**类型**是导出的，所以更可能的做法是构造好结构体后，自己按 RFC 8010 序列化。

**对项目排期的影响**：IPP 响应编码这一小块仍有自研/打补丁的可能，**但它是一个边界清晰、可用 `ipptool` 对拍的小模块**，
不构成"从零手写 IPP 协议栈"的量级。这也再次印证了简报的结论：**IPP 服务端生态是空缺的，别指望开箱即用**。

---

## 结论 2：`hashicorp/mdns` **支持 DNS-SD 服务注册** ✅（阻塞解除，但简报的怀疑需修正）

**问题**：该库是否只支持"发现别人"（client），不支持"通告自己"（advertise）？

**答案**：**支持注册。简报中标注的 `[未验证]` 应改为"已验证支持"。**

**证据 1 —— README 明确给出了"publishing a service entry"的示例**：

```go
// Setup our service export
host, _ := os.Hostname()
info := []string{"My awesome service"}
service, _ := mdns.NewMDNSService(host, "_foobar._tcp", "", "", 8000, nil, info)

// Create the mDNS server, defer shutdown
server, _ := mdns.NewServer(&mdns.Config{Zone: service})
defer server.Shutdown()
```

**证据 2 —— `zone.go` 实现了完整的 DNS-SD 记录构造**（实际读取的签名）：

```go
type Zone interface { ... }
type MDNSService struct {
    Instance string
    ...
    TXT      []string // Service TXT records
}
func NewMDNSService(instance, service, domain, hostName string, port int, ips []net.IP, txt []string) (*MDNSService, error)
func (m *MDNSService) Records(q dns.Question) []dns.RR
func (m *MDNSService) serviceEnum(q dns.Question) []dns.RR      // 枚举服务
func (m *MDNSService) serviceRecords(q dns.Question) []dns.RR   // PTR 记录
func (m *MDNSService) instanceRecords(q dns.Question) []dns.RR  // SRV + TXT + A/AAAA
```

`instanceRecords` 内在 `dns.TypeANY` 分支中明确同时构造 **SRV 与 TXT**：

```go
case dns.TypeANY:
    // Get the SRV, which includes A and AAAA
    recs := m.instanceRecords(dns.Question{Name: m.instanceAddr, Qtype: dns.TypeSRV})
    // Add the TXT record
    recs = append(recs, m.instanceRecords(dns.Question{Name: m.instanceAddr, Qtype: dns.TypeTXT})...)
```

并且 `case dns.TypeTXT` 分支返回 `&dns.TXT{Txt: m.TXT}`。

**`server.go` 的注册路径**：

```go
type Config struct {
    Zone Zone   // "Zone must be provided to support responding to queries"
    ...
}
func NewServer(config *Config) (*Server, error)
func (s *Server) handleQuestion(q dns.Question) (multicastRecs, unicastRecs []dns.RR) {
    records := s.config.Zone.Records(q)
    ...
}
```

**所以 PTR + SRV + TXT 三件套齐全，足以通告 `_ipp._tcp`。**

### ⚠️ 但发现了一个简报遗漏的真实陷阱：TXT 值需要自己按 DNS-SD 规范编码

`MDNSService.TXT` 的类型是 `[]string`，且被**原样**放进 `dns.TXT{Txt: m.TXT}`。

DNS-SD（RFC 6763 §6）要求 TXT 记录里是 **`key=value` 形式的字符串**，例如 `rp=ipp/print`、`ty=My Printer`。
而 `hashicorp/mdns` **不做这层拼装** —— 它只把你给的字符串塞进记录。

**落地要求**：调用方必须自己传入已拼好 `key=value` 的字符串数组，例如：

```go
txt := []string{
    "txtvers=1",
    "qtotal=1",
    "rp=ipp/print",
    "ty=dyj shared printer",
    "pdl=application/pdf,image/urf,image/pwg-raster",
    "adminurl=http://host:631/",
    "note=shared via dyj",
    "product=(dyj)",
    "UUID=<uuid>",
}
```

**另一个已知缺口**：该库的 README 自述是 "Simple mDNS **client/server** library"，仓库仅 4 个 `.go` 文件
（`client.go` / `server.go` / `zone.go` + 测试），**没有**实现以下 DNS-SD 高级语义：

- ❌ **服务名冲突自动解决**（RFC 6762 §9 的 probing / "My Printer (2)" 重命名）——`ipp-usb` README 特别强调这是它必须自己做的事；
- ❌ **`_universal._sub._ipp._tcp` 子类型广播**（AirPrint 所需，见简报 P0 缺口 1）；
- ❌ 关闭时的 **goodbye 包**（TTL=0 通告）。

**这三项都需要自行实现或换库**。它们都是**有界的小功能**，但必须在架构里预留位置（放在 `discovery/` 模块，不要散落）。

---

## 对调研结论的净影响

| 阻塞项 | 原状态 | 实测结论 | 对决策的影响 |
| --- | --- | --- | --- |
| `go-ipp` 服务端能力 | `[未验证]` | ✅ 具备（`request_decoder` + `response_encoder`） | **支持 Go 选型**，但响应编码可能需自研补齐 |
| `hashicorp/mdns` 注册能力 | `[未验证]` | ✅ 支持（`NewMDNSService` + `Zone`） | **支持 Go 选型**；但 TXT 编码 / 冲突解决 / 子类型需自研 |
| Go 整体选型 | 推荐 | **未被推翻** | 两个阻塞项均解除，**Go 选型成立** |

**同时确认简报对 `grandcat/zeroconf` 的警告值得采纳**：本附录的实测显示 `hashicorp/mdns`（MIT，1373★，活跃）
确实可用，而 `grandcat/zeroconf`（905★）已停更近两年且 license 为 `NOASSERTION`。

---

## 参考来源（本次实测直接读取）

- `phin1x/go-ipp` 元数据与文件树：<https://api.github.com/repos/phin1x/go-ipp>
- go-ipp 源码（编解码器）：<https://github.com/phin1x/go-ipp>
- `hashicorp/mdns` 仓库元数据（默认分支 `main`、MIT、未归档）：<https://api.github.com/repos/hashicorp/mdns>
- hashicorp/mdns README（服务发布示例）：<https://raw.githubusercontent.com/hashicorp/mdns/main/README.md>
- hashicorp/mdns `zone.go`（DNS-SD 记录构造）：<https://raw.githubusercontent.com/hashicorp/mdns/main/zone.go>
- hashicorp/mdns `server.go`（Zone 注册路径）：<https://raw.githubusercontent.com/hashicorp/mdns/main/server.go>
- DNS-SD TXT 记录 `key=value` 格式：RFC 6763 §6 <https://www.rfc-editor.org/rfc/rfc6763#section-6>
