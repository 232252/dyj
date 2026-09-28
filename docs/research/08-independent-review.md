# 附录 F：对调研结论的独立复核（Lead 自查 + 对抗性事实核查）

> 目的：调研报告由子代理产出，其**最承重的结论**必须用一手来源独立复核后才可采信。
> 方法：GitHub REST API 直接查询（非搜索摘要）+ 读取源码/规范原文 + **一轮对抗性事实核查**
> （专门试图**推翻**结论，而非确认）。
> 本附录记录**复核结论**；未通过复核的项会明确标出。

## F.0 对抗性核查结果总览（**3 项被推翻/高估，已全部更正**）

| # | 原结论 | 裁定 | 影响 | 处理 |
| --- | --- | --- | --- | --- |
| 1 | Go `net.ListenMulticastUDP` **不设置** `SO_REUSEADDR`（附录 D） | ❌ **被推翻** | **高**：归因错误，导致对标准库的可用性判断错误 | ✅ 已更正（附录 D §D.6、ADR-0002 风险项） |
| 2 | "接受 PDF/PWG/**URF** 即符合 IPP Everywhere"（ADR-0002 D3） | ⚠️ **高估/不准确** | **高**：漏了彩色机必需的 **JPEG JFIF**；URF 非 PWG 必需 | ✅ 已更正（ADR-0002 D3、ADR-0003） |
| 3 | `cups-filters` 是 **GPL-2**（报告 01 §6.2） | ❌ **被推翻** | 中高：凭空多出一条 copyleft 风险 | ✅ 已更正（报告 01 §6.2、速查表、ADR-0003） |
| 4 | `hashicorp/mdns` 能正确应答 PTR+SRV+TXT | ✅ **确认**，但有真实缺口 | 中 | 已在附录 A/ADR-0002 补充缺口细节 |
| 5 | `ipps` 默认端口 631（RFC 7472 §4.3） | ✅ **逐字确认** | 无 | 维持 |
| 6 | RFC 3998 ≠ INFRA；INFRA = PWG 5100.18 | ✅ **两部分均确认** | 无 | 维持 |
| 7 | `ancwrd1/ipp.rs` 无 LICENSE → 否决 Rust | ✅ **确认**（措辞略强，见下） | 无 | 维持 |
| 8 | CUPS = Apache-2.0 + 两项例外 | ✅ **逐字确认** | 无 | 维持 |

### 关于第 1 项（最重要的更正）

核查者指出并给出源码：`src/net/sockopt_windows.go` 中

```go
func setDefaultMulticastSockopts(s syscall.Handle) error {
	// Allow multicast UDP and raw IP datagram sockets to listen
	// concurrently across multiple listeners.
	return os.NewSyscallError("setsockopt", syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1))
}
```

且 `src/net/sock_posix.go` 的 `listenDatagram` 在 `addr.IP.IsMulticast()` 时会调用它。
**我（Lead）已亲自抓取上述两个文件核对，确认核查者所述属实。**
→ 我原先的"Go 不设 `SO_REUSEADDR`"论断**是错误的**，已撤回。
真正原因是**默认组播接口选错**（本机为 `optun`）加上 `IP_MULTICAST_LOOP=0`。

### 关于第 7 项的措辞校正

GitHub API 返回 `license: null` 只能证明**未识别到标准 LICENSE 文件**，
**不能严格证明仓库中不存在任何许可声明文本**。因此"无 LICENSE 文件"的说法**略强于证据**；
但**法律结论（默认保留全部版权）依然成立**，**否决 Rust 的决策不变**。
（已亲自列目录确认：根目录确无 LICENSE/COPYING 类文件。）

## F.1 复核通过的结论

### ✅ 仓库元数据（全部与简报一致，无出入）

| 仓库 | License | Stars | 仓库体积 | 最后推送 | 归档 |
| --- | --- | --- | --- | --- | --- |
| `OpenPrinting/cups` | **Apache-2.0** | 1763 | 125,890 KB | 2026-09-26 | 否 |
| `OpenPrinting/ipp-usb` | **BSD-2-Clause** | 210 | 562 KB | 2026-08-16 | 否 |
| `SimulPiscator/AirSane` | GPL-3.0 | 372 | 503 KB | 2026-09-27 | 否 |
| `istopwg/ippsample` | Apache-2.0 | 249 | 14,261 KB | 2026-07-04 | 否 |
| `hashicorp/mdns` | **MIT** | **1373** | 118 KB | **2026-09-14** | 否 |
| `grandcat/zeroconf` | **NOASSERTION** | 905 | 108 KB | **2023-12-07** | 否 |
| `OpenPrinting/go-mfp` | BSD-2-Clause | 23 | 25,528 KB | 2026-09-27 | 否 |

**简报对 `grandcat/zeroconf` 的警告经复核成立**：license 为 `NOASSERTION`（非标准），
最后推送 **2023-12-07**，距今约两年无提交。**不应作为长期依赖** —— 该建议采纳。

### ✅ CUPS 是 Apache-2.0（简报 §6 许可证结论成立）

GitHub API 返回 `spdx_id = Apache-2.0`。
**推翻"CUPS 是 GPL"这一常见误解**，也支撑了 ADR-0003 的判断：**风险不在 CUPS 本身，而在 Ghostscript**。

### ✅ 拒绝 Rust 的理由成立（**这是最关键的一条，已独立确认**）

简报称"唯一成熟 IPP 库 `ancwrd1/ipp.rs` 没有 LICENSE 文件，法律上不可用"，据此否决 Rust。

独立复核结果：

```
repo      : ancwrd1/ipp.rs
stars     : 71
pushed    : 2026-09-02
license   : >>> NULL (no license detected)
archived  : False

--- root file listing ---
  .github, .gitignore, CHANGELOG.md, Cargo.lock, Cargo.toml,
  README.md, examples, ipp, rustfmt.toml, util

LICENSE-like files present: (空)
```

**确认：根目录确实没有任何 LICENSE/COPYING 文件，GitHub 也未识别出许可证。**
→ 该项目**默认保留全部版权**，未经授权不得使用/分发。**否决 Rust 的理由成立。**

**补充观察（简报未提，值得注意）**：该库**仍然活跃**（2026-09 推送）。
所以这不是"作者弃坑"，而是**作者从未加许可证**。
若未来该项目补上许可证（如 MIT/Apache-2.0），**Rust 选项值得重新评估** —— 建议列为长期观察项。

### ⚠️ 附录 D 的 mDNS 结论（**部分被推翻，已更正**）

**仍然成立的部分**：多进程共享 UDP 5353 **可行**（附录 D §D.2 结论 1/2 的实测输出有效）。

**已被推翻的部分**：我原先归因于"Go 不设 `SO_REUSEADDR`"——**错误**（见 F.0 第 1 项）。
真正原因是**默认组播接口选错**（本机为 `optun`，非主 LAN `以太网`）+ `IP_MULTICAST_LOOP=0`。

**过程诚实记录**：本会话我最初的几次探针失败，原因**各不相同**且部分是我自己的测试代码缺陷
（`use of closed network connection` 来自 Go 单进程内两个 `ListenMulticastUDP` 相互竞争；
`no such network interface` 来自非 ASCII 接口名的传参问题）。
**我把多个异源失败归并成单一解释，这是误诊的根源。**
教训：凡"某库有缺陷"的结论，**必须先读源码再下断言**。

## F.2 需要修正/限定的表述

| 项 | 问题 | 处理 |
| --- | --- | --- |
| 附录 A 原称"`go-ipp` 响应编码器私有，可能需 fork" | **过度悲观**：`Response.Encode()` 是导出的，无需 fork | 已在[附录 E](07-go-ipp-server-capability.md) §E.1 修正 |
| 附录 A 未发现 `AttributeTagMapping` 只有 70 项 | **遗漏了一个重大缺口** | 已在[附录 E](07-go-ipp-server-capability.md) §E.2 补充（缺失 `-supported` 系列） |
| 附录 D §D.2 归因"Go 不设 `SO_REUSEADDR`" | ❌ **错误归因**（Go 实际会设） | 已在附录 D §D.6 撤回并给出正确根因 |
| ADR-0002 D3 "接受 PDF/PWG/**URF**" | ⚠️ URF 非 PWG 必需；**漏了彩色机必需的 JPEG JFIF** | 已按 PWG 原文更正 |
| 报告 01 §6.2 + 速查表 "`cups-filters` = GPL-2" | ❌ **错误**，与报告 02 §A.1 自相矛盾 | 已改为 `Apache-2.0-with-GPL2-LGPL2-Exception` |
| 简报对 `hashicorp/mdns` 标注 `[未验证]` 注册能力 | 实际**支持**注册（`NewMDNSService` + `Zone`） | 已在附录 A 改为"已验证支持" |
| 简报未提及 `hashicorp/mdns` 的 TXT 值需自行拼 `key=value` | 真实陷阱 | 已写入附录 A 与附录 D |
| 简报未提及 `hashicorp/mdns` 的 `NewServer` **忽略监听错误**、`NewMDNSService` **回退 `os.Hostname()`**（Windows 上为 NetBIOS 名，SRV 目标会错） | 真实缺口 | 已写入 ADR-0002 风险项 |
| 简报自述的 `web_search` 401 故障 | 已复现，确为环境问题，非子代理失误 | 已在[本目录 README](README.md) 记录影响与应对 |

## F.3 仍**未被独立复核**的项（诚实声明）

以下内容**尚未**取得一手来源确认，**不得当作已证实事实**（已在各文档中标注 ⚠️）：

1. **AirPrint 的确切要求**（`urf` TXT 取值、`_universal._sub._ipp._tcp` 是否存在、必需属性清单）
   —— Apple 规范原文本次未能取得。**必须用 `ippeveprinter` + `dns-sd` 实证。**
2. **Windows 内置 IPP 客户端实际发送的文档格式** —— 决定 `document-format-supported` 的内容。
3. **Windows Protected Print Mode (WPPM)** 的实际行为（官方页面 404）。
4. **LPR Port Monitor 是否为 Windows 可选功能**（枚举需管理员权限，本次未提权）。
5. **IPP Everywhere 必需操作集"恰好 8 个"** 的精确性 —— 方向可信，具体数字待用 PWG 5100.14 复核。
6. ~~**`cups-filters` 的 SPDX 标识**未核实~~ → ✅ **已核实为 `Apache-2.0-with-GPL2-LGPL2-Exception`**。
7. **Ghostscript AGPL 的原文页**未取得（结论方向可信，但缺一手 URL）。
8. **彩色机型上 `document-format-supported` 是否必须含 `image/jpeg`** ——
   PWG 页面原文说 "JPEG only required for color printers"，但**"彩色打印机"的判定标准**
   （由哪个属性决定？`color-supported`？）未核实。**本机目标机型是彩色机，故此项重要。**

**这些都是"动工前 P0 清单"的一部分**，见[本目录 README](README.md)。

## F.4 复核结论

**调研的三条主干结论（IPP+DNS-SD 协议、Go 技术栈、不做驱动栈）在完成独立复核与一轮对抗性核查后依然成立。**
其中"否决 Rust"、"CUPS 为 Apache-2.0"两条关键论据**已逐字确认**。

**对抗性核查推翻了 3 项，全部已更正**：
1. 附录 D 的 `SO_REUSEADDR` 归因 → 改为**默认组播接口选错**（对标准库的可用性判断由"不可用"改为"可用但须显式指定接口"）；
2. ADR-0002 的文档格式表述 → 改为 **PWG Raster + JPEG JFIF（彩色机）必需、PDF 推荐**，URF 非必需；
3. `cups-filters` 的许可证 → **Apache-2.0（含例外）**，**消除了原以为存在的 copyleft 风险**。

**工作量预期需要上调的只有一处**（Lead 实测发现，附录 E）：
`pkg/attributes/` **必须**自研"带显式 tag 的 RFC 8010 属性写入器"，
因为 `go-ipp` 缺少几乎所有 IPP Everywhere 必需的能力属性。

**净判断：Go 选型与"不做驱动栈"方向成立；许可证图景比原先更有利；
"IPP 服务端工作量"比简报呈现的更重。** 这不改变方向，只影响排期 ——
且该模块边界清晰、可用 `ipptool` 对拍验收。
