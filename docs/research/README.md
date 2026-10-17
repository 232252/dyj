# 调研与设计索引

本目录保留动工前的调研（01–09）、平台与界面研究（10–12），以及已交付版本的
设计记录（13 = v0.3，14 = v0.4，15 = 跨网段发现）。**产品决策以 `docs/adr/`
为准。** 下文凡写「已实施」的条目，代码已在对应版本落地；不要再把它们当成
Phase 0 待办。

> **状态（2026-10-17，v0.4.2）**
>
> - v0.1–v0.2：IPP 服务端、渲染、管理页、三平台打包已交付。
> - v0.3：零配置角色向导、4 位码配对、接收端装系统队列、多打印机、可观测性
>   （调研 13 / ADR-0010）已实施。
> - v0.4.0：4 路由无侧栏极简界面（调研 14 / ADR-0011）已实施。
> - v0.4.1：按地址添加、扫描去重（调研 15 P1/P4）已实施。
> - v0.4.2：已知节点重发现（调研 15 P2）、配对码格子空渲染、mesh 假握手不再
>   被当成「在线」已实施。
>
> **仍须现场人工验收（软件侧不再缺入口，云端 VM 无法代做）**：
>
> 1. **Win11 设置里原生「添加打印机」**（P0 #1 / ADR-0004 A4）。代码路径有
>    黄金字节 + 独立解码器 + E2E；GUI 点击本身要在一台 Windows 11 上做。
>    步骤：共享端 `dyj` 已在跑 → 客户端「设置 → 蓝牙和其他设备 → 打印机和扫描仪
>    → 添加设备」或「按 IP 添加」填 `http://<共享端>:6631/ipp/print`（已配对的
>    队列 URI 在系统队列里；未配对的裸路径会被 401，应走 dyj 客户端页）。
> 2. **AirPrint / `image/urf`（P0 #4）**。ADR-0007 明确不声明 URF，直到取得
>    Apple 规范原文或 `ippeveprinter` + `dns-sd -B/-L` 的一手 TXT。手机/iPad
>    直接发现 **不在承诺范围**。
> 3. **el（Debian 192.168.110.214 / mesh 10.126.126.1）仍是 2026-09-29 的
>    v0.2 旧构建**（调研 12 §3、调研 15 GAP-2）。root 停旧进程，用当前
>    linux-amd64 二进制按原参数（当时是 `-backend file`）重启。未升级前不要
>    在 UI 上对它走配对。
> 4. **10.126.126.2（真实打印机宿主）** 仍无凭据。交接步骤见
>    [调研 12 §4](12-windows-acceptance-2026-09-30.md)。mesh 对「没有监听的端口」
>    会做 TCP 假握手，探活必须用 IPP，不能用裸 TCP。
> 5. **`dyj service install` 与防火墙规则** 需要管理员会话，安装器已包含；
>    绿色运行需人工执行调研 12 §4 的 `netsh` 两条。

## 文档

| 文档 | 内容 | 状态 |
| --- | --- | --- |
| [`01-protocols.md`](01-protocols.md) | IPP/CUPS/AirPrint/DNS-SD/WSD/LPD/9100 协议全景；7 种服务端架构对比表 | ✅ 调研员产出（655 行，79 URL） |
| [`02-prior-art-and-stacks.md`](02-prior-art-and-stacks.md) | 既有实现（CUPS/ipp-usb/AirSane/ippsample/ipp-sharing）与技术栈对比 | ✅ 调研员产出（727 行，102 URL） |
| [`03-verified-blockers.md`](03-verified-blockers.md) | **附录 A**：对简报两个"待验证阻塞项"的一手源码实证 | ✅ Lead 实证 |
| [`04-microsoft-driver-timeline.md`](04-microsoft-driver-timeline.md) | **附录 B**：微软打印驱动淘汰时间表（一手原文逐条核实） | ✅ Lead 实证 |
| [`05-environment-probe.md`](05-environment-probe.md) | **附录 C**：本机真实环境实测（网络/打印机/mDNS 占用） | ✅ Lead 实测 |
| [`06-mdns-coexistence-verified.md`](06-mdns-coexistence-verified.md) | **附录 D**：P0 #3 实证 —— mDNS 与 Chrome 共存**已解除阻塞** | ✅ Lead 实测 |
| [`07-go-ipp-server-capability.md`](07-go-ipp-server-capability.md) | **附录 E**：`go-ipp` 服务端能力实测 —— **发现属性编码硬缺口** | ✅ Lead 实测 |
| [`08-independent-review.md`](08-independent-review.md) | **附录 F**：对调研结论的独立复核（含"否决 Rust"的关键论据确认）。与 09 不是同一份 | ✅ Lead 复核 |
| [`09-p0-empirical-verification.md`](09-p0-empirical-verification.md) | **附录 I**：P0 红队实证（wire 字段顺序、enum 宽度）。文首曾误标「附录 F」，与 08 撞名，已改。§9 的代码缺陷已在 v0.1 前修掉（ADR-0005/0006）；§8 里 Win11 GUI / ipptool / Apple 原文仍未取到 | ✅ 红队报告；代码项已关闭 |
| [`10-ui-and-platform-deployment.md`](10-ui-and-platform-deployment.md) | GUI 选型（Web 管理页 + Windows 托盘）、控件组件化、三平台安装/自启/防火墙/签名/分发（Win: Inno+SCM；Linux: nfpm+systemd+avahi；macOS: launchd+brew） | ✅ 已落地（ADR-0008/0009，v0.2 起；文内行号是动工前快照） |
| [`11-ui-icon-design-research.md`](11-ui-icon-design-research.md) | 品牌标记、托盘图标、Windows exe 资源、Linux hicolor 图标 | ✅ v0.3 已落地（见 CHANGELOG「Brand identity」）；macOS `.icns` 仍挂起 |
| [`12-windows-acceptance-2026-09-30.md`](12-windows-acceptance-2026-09-30.md) | **附录 G**：v0.2 后 Windows 物理机验收（wingdi 真驱动无纸链路、跨平台客户端矩阵、管理页鉴权；发现的 TLS 中文主机名崩溃与多网卡 mDNS 缺口及修复）；含 10.126.126.2 交接 runbook。份数列恒为 0 已在 v0.4.2 按 IPP 默认 1 修复 | ✅ Lead 实测 |
| [`13-zero-config-redesign.md`](13-zero-config-redesign.md) | **v0.3 详细设计**：单程序角色向导、4 位码配对与 URI 路径 token、接收端自动装系统队列、进程内热生效、多打印机；可观测性契约 | ✅ 已实施（v0.3）。产品界面被 14 取代；技术章节继续有效。非目标是「不转发 mDNS」，不是「跨网段完全不能用」 |
| [`14-minimal-two-mode-redesign.md`](14-minimal-two-mode-redesign.md) | **v0.4 详细设计（界面极简重构）**：7 项侧栏/8 路由 → 4 路由无侧栏、角色即主页、零设置、文案禁用词、诊断导出 | ✅ 已实施（v0.4.0，[ADR-0011](../adr/0011-minimal-two-mode-ui.md)）。§9 开放问题已关闭或标成现场项 |
| [`15-discovery-cross-subnet-mesh.md`](15-discovery-cross-subnet-mesh.md) | **附录 H**：跨网段扫不到的根因（mDNS 链路本地 / 曾无手动地址 / el 旧构建）。P1 按地址添加、P4 去重、P2 已知节点重发现已实施；P0 el 升级仍是现场运维 | ✅ Lead 实测；代码项已实施，el 升级未做 |

## 三句话结论

1. **协议**：IPP + DNS-SD 是唯一同时满足"三平台原生支持"与"长期有效"的组合；
   微软官方时间表正在淘汰第三方驱动，**IPP-first 的价值逐年上升**（附录 B）。
2. **栈**：**Go**（三平台免运行时静态二进制 + 唯一同时具备可用 IPP 库与活跃 MIT mDNS 库的生态）。
3. **架构**：**不做驱动栈、不碰 USB 解绑** —— 把 IPP 端点架在"已有打印队列"之上（`driver/winspl`），
   复用系统驱动完成光栅化。既省体积，又**彻底绕开 Ghostscript AGPL 风险**。

## ⚠️ 原 Phase 0 实证清单

这是动工前的问题单，不是当前待办。划掉的项已经用代码或实测回答。
没划掉的项要么被架构决策取代，要么只能在真机/外部规范上收口（见文首现场清单）。

| # | 待验证问题 | 为什么是 P0 | 建议验证方式 |
| --- | --- | --- | --- |
| ~~**1**~~ | ~~自研 IPP 服务端能否被 Windows 11 直接添加成功？~~ | 代码级已实证（RFC 8010 黄金字节 + 独立解码器 + E2E）；**GUI 真机验收仍开放** | 起服务 → Win11 添加 → 打印（待做） |
| ~~**2**~~ | ~~Windows 内置 IPP 客户端实际发送什么文档格式~~ | **已被架构决策取代**：只声明我们能渲染的格式（pwg-raster/jpeg/png/text），见 ADR-0007 D1——客户端适配声明，而非声明适配客户端 | — |
| ~~3~~ | ~~mDNS 能否与已占用 5353 的 Chrome 共存？~~ | ✅ **已实证解除**（[附录 D](06-mdns-coexistence-verified.md)） | 共存**可行**。⚠️ 原"Go 不设 `SO_REUSEADDR`"归因**已被对抗性核查推翻**，真正原因是**默认组播接口选错**；详见 [附录 D §D.6](06-mdns-coexistence-verified.md) |
| **4** | AirPrint 的确切要求（`urf` TXT 取值、`_universal._sub._ipp._tcp` 是否存在、必需属性） | 决定 macOS/iOS 能否发现；**Apple 规范原文本次取不到** | 跑 `ippeveprinter`，用 `dns-sd -B/-L` 观察其**实际**广播的 TXT 键 |
| **5** | IPP Everywhere 必需属性表能否通过 `ipptool` 官方测试套件？ | 属性缺失是 AirPrint 报错 21345 类问题的根源 | `ipptool` + `get-printer-attributes-suite.test` 对拍 |
| **6** | 打印机休眠时端口全关（附录 C.4），如何判断设备可用性？ | 不能用端口扫描判活；影响作业失败处理 | 走 spooler 队列状态查询而非端口扫描 |
| **7** | LPR Port Monitor / Internet Printing Client 是否 Windows 可选功能及其名称 | 调研未核实；影响客户端引导文档 | 提权后 `Get-WindowsOptionalFeature` |
| **8** | Windows Protected Print Mode (WPPM) 的实际行为 | 官方正文取不到（404），不要断言 | 需权威来源或实测 |
| ~~9~~ | ~~`go-ipp` 响应编码器为包内私有~~ | ✅ 已实证并超越：**编码与解码均已弃用 go-ipp**（解码器有覆盖 bug），见 [ADR-0005](../adr/0005-single-wire-encoder.md)、[ADR-0006](../adr/0006-own-request-decoder.md) | — |
| ~~**10**~~ | ~~`go-ipp` 缺少 IPP Everywhere 必需属性~~ | ✅ **已解决**：`pkg/attributes/` 显式 tag 写入器 + 完整属性表已交付，黄金字节测试锁定 | — |
| ~~**11**~~ | ~~`operations-supported` 1setOf enum 的字节宽度~~ | ✅ **已实证并修正**：RFC 8010 §3.8 强制 **4 字节**（[附录 I](09-p0-empirical-verification.md) 红队报告 §5；附录 F 是 08，不是这份），2 字节版本会让独立解码器失步；黄金测试锁定 | — |
| ~~**12**~~ | ~~彩色机是否必须声明 `image/jpeg`~~ | ✅ **已按原文落地**：JPEG 在默认声明表中（彩色机 PWG 必需；灰度机转换后同样可打） | — |

## 来源可靠性说明

- **调研员报告**中的仓库指标（stars/license/size/pushed_at）均通过 **GitHub REST API 实测**，非搜索摘要。
- 本次会话中 **`web_search` 工具全程不可用**（DeepSeek API 返回 HTTP 401），
  所有调研均通过直接 `web_fetch` 已知权威 URL + GitHub API 完成。
- 凡未取得权威原文的论断，均显式标注 `[未验证]` / ⚠️，**未编造任何 RFC 编号、端口或 API 细节**。
- **已完成一轮对抗性事实核查**（专门试图推翻结论），结果与全部更正见
  [附录 F](08-independent-review.md) §F.0。**3 项结论被推翻/高估，均已在原文档就地更正并留痕。**
- 已知并已修正的事实错误：
  - `savoirfairelinux/ipp-usb` **不存在**（404），正确为 `OpenPrinting/ipp-usb`；
  - `OpenPrinting/ippsample` **404**，正确为 `istopwg/ippsample`；
  - **RFC 3998 不是 INFRA**（它是 Administrative Operations）；INFRA 实为 PWG 5100.18，且本项目不需要；
  - **`ipps` 默认端口是 631 而非 443**（RFC 7472 §4.3）；
  - **`cups-filters` 不是 GPL-2**，而是 `Apache-2.0-with-GPL2-LGPL2-Exception`（报告 01 与 02 曾互相矛盾）；
  - **Go 的 `net.ListenMulticastUDP` 确实会设置 `SO_REUSEADDR`**（附录 D 原归因错误，已撤回）；
  - **`image/urf` 不是 IPP Everywhere 必需格式**；彩色机必需 **JPEG JFIF**。
