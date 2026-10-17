# 附录 H：发现域诊断 —— 跨网段（mesh/VPN）扫不到共享打印机

**状态**：✅ Lead 实测（2026-09-30）。**P1/P4 已在 v0.4.1 实施，P2 已在 v0.4.2
实施。P0（el 升到当前构建）仍是现场运维，仓库里没有那台机器。** P3 网段扫描
继续不做。

用户症状：接收端"添加打印机"页显示"没有发现共享打印机 / 确认对方的 dyj 正在共享，
且两台电脑在同一个网络（同一 WiFi/路由器）"，而用户认为"我们明明在同一个网络
（10.126.126.x mesh）"，且"换别的网络也扫不到"。

## 1. 结论（TL;DR）

1. **mDNS 是链路本地协议**（224.0.0.251，TTL 1，路由器不得转发，RFC 6762）。
   10.126.126.x 是 mesh/VPN overlay：**组播进不去、也出不来**。所以"从
   10.126.126.x 扫描"永远扫不到任何东西——这是协议边界，不是 dyj 的 bug。
   UI 文案里的"同一个网络"精确含义是"**同一个二层广播域**（同一 WiFi/同一路由器
   的 LAN 口）"，mesh/VPN 不算。
2. **发现路径曾经只有 mDNS（GAP-1，已关闭）**。v0.4.1 客户端页在扫描列表下
   （含空态）提供「按地址添加」：地址 + 可选 4 位码，缺省端口 6631，接受
   `[IPv6]`。v0.4.2 再把已经添加过的地址用 IPP Get-Printer-Attributes 重探测
   并置顶（P2）。CLI 仍然没有单独的 `pair` 子命令——管理页是入口。跨网段
   **不是**靠转发 mDNS。
3. **mesh 单播完全可用**（本次实测：`dyj status 10.126.126.1:6631` 应用层正常
   返回 el 的 IPP 属性）。也就是说只要"知道 host:port"，跨网段配对/打印在协议
   层今天就能走通——缺的只是把地址递给程序的入口。
4. **当前同一 LAN 上发现实际是好的**：本机 UI 同款扫描接口回环实测返回了 el 的
   记录（§3 证据 ③）。"换别的网络也扫不到"与"当时不与共享端在同一广播域"自洽
   ——判定流程见 §5。

## 2. 环境与拓扑（2026-09-30 实测）

| 节点 | 接口 | 地址 | dyj 版本 |
| --- | --- | --- | --- |
| 本机（Win11"生命"，扫描发起方） | 以太网 | 192.168.110.40 | v0.4.0（dyj.exe，dist 构建） |
| | mesh/VPN（非组播网卡） | 10.126.126.4 | — |
| | 内网（非组播网卡） | 10.2.3.x | — |
| el（Debian，共享方） | LAN | 192.168.110.214 | **9/29 07:13 旧 v0.2 构建**（调研 12 §3：无管理页、TXT 无 dyj/pair 标记） |
| | mesh | 10.126.126.1 | — |
| 10.126.126.2（Win，真实打印机宿主） | mesh | 10.126.126.2 | 未纳入（无 SSH 凭据，调研 12 §4 runbook） |

10.126.126.x mesh 是 relay 型 overlay：TCP 单播可达，但对**无本地监听端口会做
"假握手"**（调研 12 §4 一手证据），且不承载组播。

## 3. 一手实测证据（本次采集）

| # | 命令 / 操作 | 结果 | 说明 |
| --- | --- | --- | --- |
| ① | `dyj list-interfaces`（本机） | 仅 `以太网 mtu=1460 addrs=192.168.110.40` | **mesh/VPN/内网网卡都不具备组播能力**。通告（`node.go startMDNS` 的 `IPs: n.ips`）与扫描（`apiScan` 的 `SelectInterfaces(nil)`）都只覆盖这张卡 |
| ② | `dyj browse`（本机，LAN） | 扫到 `dyj-debian-e2e._ipp._tcp.local` @ 192.168.110.214:6631 | el 的旧构建在 LAN 上**正在正常宣告** |
| ③ | `GET http://127.0.0.1:6631/admin/api/scan`（本机 UI 同款接口，回环免令牌） | 返回 2 条 el 记录，`reachable:true` | **UI 扫描链路本身没坏**；"扫不到"只能发生在"不在同一广播域"时 |
| ④ | `dyj status 10.126.126.1:6631`（本机→el，走 mesh） | 应用层完整返回（printer/state/formats） | **mesh 单播 IPP 可用** → 跨网段配对只缺入口（GAP-1） |
| ⑤ | ②③ 的 TXT / addrs 细节 | el 通告了 7 个地址（含 172.17-20.0.1 docker 网桥×4、10.2.3.29、192.168.101.6）；TXT **无 `dyj=1`/`pair=1`/`dyjver` 键** | 旧构建全量通告地址（新代码 `scoreIface` 已防）；无标记 → UI 判为"普通打印机" |
| ⑥ | ③ 的返回条目 | 同一台 el 出现**两条**（host 分别为 `…-br-e57f74e0bde4.local` 与 `…-optun.local`） | `apiScan` 去重键含 per-interface host 名，raw/reference 两路各算一条（小缺陷 D-1） |

## 4. 根因分层

### L1 协议层：mDNS 不能跨网段（设计使然，不修）

- mDNS 用 IPv4 链路本地组播 224.0.0.251 / IPv6 ff02::fb（`pkg/discovery/mdns.go:36-38`），
  TTL 1，路由器**不得**转发（RFC 6762）；它只覆盖一个 L2 广播域。
- mesh/VPN overlay（本例 10.126.126.x）通常根本不转发组播——即使客户端想转发，
  relay 型网络也没有"组播给所有成员"的语义。
- 推论：**凡是"两端只有 mesh/VPN 相连"的场景，mDNS 发现必然为空**。这与 dyj
  实现质量无关；Bonjour/Avahi/打印机 AirPrint 在同样拓扑下一样扫不到。

### L2 产品层：跨网段当时没有第二发现路径（GAP-1，v0.4.1 已关闭）

> 下面三段是 2026-09-30 的缺口描述。入口已经有了：UI「按地址添加」+
> v0.4.2 已知地址重发现。CLI 仍无独立 `pair` 子命令。

- 发现唯一入口是 `apiScan` → `discovery.Browse`（`cmd/dyj/nodeapi.go:172-190`），
  只在组播网卡上发查询（证据 ①）→ mesh 网段连"尝试"都不会发生。
- 而配对请求 `apiPairProxy` 本身就接受 `host+port`（`cmd/dyj/nodeapi.go:273-295`）
  ——**后端已具备手动配对能力，只是 UI/CLI 都没有入口**：
  - UI 添加页只有扫描列表，无地址输入（`page-client.js` 仅渲染 scan 结果）；
  - CLI 命令集（serve/open/browse/status/print/jobs/cancel/service/doctor/tray/
    list-interfaces）无 `pair`。
- 即修复面很小：把"已知 host:port"递进现有 `apiPairProxy` 即可打通 mesh 场景。

### L3 环境层：el 必须先升级（GAP-2，运维）

el 跑的是 9/29 07:13 旧 v0.2 构建（调研 12 §3 已记录）：

- TXT 无 `dyj=1`/`pair=1`/`dyjver=3`（证据 ⑤）→ v0.4 UI 把它显示为"普通打印机"
  而非"dyj·需配对码"。点击后的路径与 v0.3+ 的 PIN 配对协议不匹配，配对会失败
  或行为不明。
- 旧构建全量通告本机地址（含 4 个 docker 网桥），接收端 `firstReachable`
  （`nodeapi.go:255-265`）按序 800ms/地址串行探测，最坏多等 ~5s 才命中
  192.168.110.214（本次靠 10.2.3.29 内网互通侥幸秒回）。**这是 el 侧的问题，
  升级 v0.4 后 `scoreIface` 会自动不再通告网桥地址。**
- 处理：按调研 12 §3——root 下停旧进程、用 v0.4 linux-amd64 二进制原参数
  （`-backend file`）重启。**未升级前不要在 UI 上对 el 走配对流程**。

### L4 次生缺陷（顺带记录，低优先）

- **D-1 列表重复**：同一实例因 per-interface host 名（`${IFACE}` 机制）在
  raw/reference 两路各出一条（证据 ⑥）。`apiScan` 去重键应改用 instance label
  （截断 `._ipp` 之后的 host 不同不影响），或合并同 UUID 条目。
- **D-2 rtt 假象**：`reachable:true rtt_ms:0` 命中的是 10.2.3.29（本机与 el 同在
  内网 10.2.3.x）。URI 安装时会优先用探测命中的地址（内网地址）——换网络后队列
  失效。低概率场景，记录备查。

## 5. "为什么我当时扫不到"判定流程（给用户的口径）

按序自检（全部用现有工具，无需改码）：

1. **两端是否真在同一广播域**：共享端与接收端各看自己的"局域网地址"是否同一网段
   （如都是 192.168.110.x）。**只有 mesh/VPN 相连（10.126.126.x）≠ 同一网络**，
   这是 mDNS 协议边界（L1）。
2. 共享端是否在共享：管理页状态页"正在共享的打印机"非空、`mdns.announce` 有日志。
3. 接收端点"重新扫描"（3s 窗口），同时共享端抓 `scan.result` 对端日志。
4. 共享端 `dyj doctor`：防火墙是否放行 UDP 5353（三 profile）、TCP 6631。
5. 若第 1 条不满足（只有 mesh 相连）→ 用客户端页「按地址添加」填对方地址
   （例如 `10.126.126.1:6631`）和配对码。以前添加过、地址没变的，打开添加页
   会自动再探测并置顶。这不是 mDNS 故障。

## 6. 修复方案（P1/P2/P4 已实施；P0 仍是现场）

> **实施记录**
>
> - **2026-09-30，v0.4.1**：P1 与 P4。客户端页扫描列表下/空态有「按地址添加」
>   （地址 + 可选配对码，缺省端口 6631，支持 `[IPv6]`；配对码错误不再误触发
>   管理令牌横幅）。`apiScan` 合并键改为实例名+端口并并集两路地址
>   （`scanEntries`，带单测）。确认点 1：`apiPairProxy` 以请求里的 host 回显
>   `ipp://<host>:<port><uriPath>`，不改成服务端自选地址。
> - **2026-10-17，v0.4.2**：P2。已知节点就是接收端 `added-printers.json` 里的
>   `server_host` / `server_port` / `uri_path`（与共享端 `pair.List` 的 device
>   记录不是同一张表：那是「谁连过我」，这是「我连过谁」）。打开添加页时对这些
>   地址做 Get-Printer-Attributes，活着的置顶并标「以前添加过」。HTTP 401
>   视为「机器在、配对失效」，行上要求新配对码；裸 TCP 成功但没有 IPP 响应
>   的（mesh 假握手）不展示、状态记为离线。事件 `scan.known`。
> - **PIN 空格子**：当时记成「内嵌浏览器自定义元素升级异常、组件源码正常」。
>   根因是 `dyj-pin-input.js` 在构造函数里调用 `t()` 却没有 import，升级抛
>   `ReferenceError`，影子树是空的。v0.4.2 已补上 import。任何浏览器都会空白，
>   不是某一家内嵌浏览器的特例。
> - **P0 el 升级**仍然没做：仓库接触不到那台 Debian。步骤见调研 12 §3。

| 方案 | 内容 | 成本 | 覆盖 | 建议 |
| --- | --- | --- | --- | --- |
| **P0 el 升级**（运维，零代码） | root 停旧进程 → v0.4 linux-amd64 原参数重启（调研 12 §3/§4） | 0 | GAP-2 | 立即 |
| **P1 按地址添加**（UI 小改） | 添加页扫描列表下加"按地址添加"输入框（host:port），提交走现有 `apiPairProxy`；空态文案 `client_none_hint` 追加该入口指引 | 小（一个输入框 + 提交逻辑） | GAP-1 全部场景（mesh/跨网段/AP 隔离兜底） | **v0.4.1 已实施** |
| **P2 已知节点重发现** | 接收端用 `added-printers.json` 里的 host:port；打开添加页时对已知地址做 **IPP Get-Printer-Attributes**（禁用裸 TCP connect——mesh 假握手会假阳性，调研 12 §4）并置顶展示 | 中 | 断线重连/换网后"再发现" | **v0.4.2 已实施** |
| **P3 网段直连扫描**（可选） | 用户给 CIDR/host 列表 → 逐 IP 应用层探测 6631（并发限幅）。仅手动触发 | 中 | 无 mDNS 环境的兜底 | 缓，P2 通常够用 |
| **P4 小缺陷**（D-1） | `apiScan` 去重键改 instance label；顺带核对 rtt/地址选择（D-2） | 小 | 体验 | **v0.4.1 已实施**（D-2 仍只记录，不自动改地址选择） |

**配套明确不做**：跨网段转发/桥接 mDNS 组播（违反 RFC 6762，且 mesh 无组播语义）；
自研组播隧道（复杂度与收益不成比例）。mDNS reflector（如 avahi-daemon reflector）
属于用户侧网络基建选项，写入文档即可，不做进产品。

**确认点（已关闭）**：

1. 手动配对返回的 IPP URI host **回显请求的 host**（`apiPairProxy` 的
   `host := req.Host`）。`.local` 名会尝试解析成地址再写入队列 URL。
2. `client_none_hint` / `client_manual_*` / `client_badge_known` 已过调研 14
   禁用词测试（`TestI18nNoJargonInUserCopy`）。
3. P2 已知节点表 = `<stateDir>/added-printers.json`。不另建文件，删除队列时
   记录一起消失。共享端 `paired-devices.json` 不参与接收端重发现。

## 7. 验收标准（修复后）

1. LAN：接收端扫描列出 el（升级后带 dyj 标记、单条目——D-1 修复后）。
2. mesh-only（关掉两端 LAN/断开同一 WiFi，仅 10.126.126.x 相连）：接收端"按地址
   添加"输入 `10.126.126.1:6631` + PIN → 配对成功 → 装队列 → 测试页打印成功。
3. 换网重连：打开添加页，对 `added-printers.json` 里仍然应答 IPP 的地址置顶
   （P2，v0.4.2）。地址本身变了（旧 LAN IP 已死）时不会凭空猜新 IP，用
   「按地址添加」填新地址。软件侧可以在无打印机的环境用 `probeIPP` 单测
   覆盖「IPP 成功 / 401 / 假握手沉默」；真机 mesh 仍要现场看 el 升级之后的结果。
4. `dyj doctor --client` 三条检查在 mesh-only 场景可用。

## 8. 与既有文档的关系

- 调研 12 §3（el 旧构建处置）、§4（.2 runbook + mesh 假握手证据）——P0/P2 的依据。
- `docs/troubleshooting.md` 症状 1——本文档落地后同步补充跨网段口径（已同步）。
- research 13 §5（可观测性契约）：P1/P2 需新增事件建议复用 `pair.req`（`dst` 已记录）
  并加 `scan.manual`（手动地址提交）与 `scan.known`（已知节点探测）。

## 9. 附：本次实测原始输出（摘）

```text
$ dyj list-interfaces
以太网              mtu=1460  addrs=192.168.110.40

$ dyj browse        # 摘要
10.2.3.29,172.17.0.1,172.18.0.1,172.19.0.1,172.20.0.1,192.168.101.6,192.168.110.214:6631
  "dyj-debian-e2e._ipp._tcp.local"
  txt: txtvers=1 qtotal=1 rp=ipp/print ty=dyj-debian-e2e pdl=image/pwg-raster,...
       UUID=28a0d495-… TLS=F Color=F printer-type=0x0000 kind=document PaperMax=legal-A4
       （无 dyj=/pair=/dyjver= 键 —— 旧 v0.2 构建）

$ curl -s http://127.0.0.1:6631/admin/api/scan   # UI 同款接口，回环免令牌
{"services":[{…"host":"dyj-debian-e2e-br-e57f74e0bde4.local",…,"reachable":true,"rtt_ms":0},
             {…"host":"dyj-debian-e2e-optun.local",…,"reachable":true,"rtt_ms":0}]}

$ dyj status 10.126.126.1:6631   # mesh 单播
printer: dyj-debian-e2e   state: idle/pending   accepting: yes
formats: image/pwg-raster,image/jpeg,image/png,text/plain
```
