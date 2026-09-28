# 附录 F：对调研结论的独立复核（Lead 自查）

> 目的：调研报告由子代理产出，其**最承重的结论**必须由 Lead 用一手来源独立复核后才可采信。
> 方法：GitHub REST API 直接查询（非搜索摘要）+ 读取源码原文。
> 本附录只记录**复核结论**；未通过复核的项会被明确标出。

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

### ✅ 附录 D 的 mDNS 结论（Lead 自己的实测，已如实记录偏差）

本会话中我最初的几次探针失败了，原因**各不相同**且部分是我自己的测试代码缺陷
（`use of closed network connection` 来自 Go 单进程内两个 `ListenMulticastUDP` 竞争；
`no such network interface` 来自接口名非 ASCII 的传参问题）。
最终用**自建 socket + `SO_REUSEADDR` + `IP_ADD_MEMBERSHIP`** 得到确定结论，
并已在[附录 D](06-mdns-coexistence-verified.md) 中按"实测输出"如实记录，未美化过程。

## F.2 需要修正/限定的表述

| 项 | 问题 | 处理 |
| --- | --- | --- |
| 附录 A 原称"`go-ipp` 响应编码器私有，可能需 fork" | **过度悲观**：`Response.Encode()` 是导出的，无需 fork | 已在[附录 E](07-go-ipp-server-capability.md) §E.1 修正 |
| 附录 A 未发现 `AttributeTagMapping` 只有 70 项 | **遗漏了一个重大缺口** | 已在[附录 E](07-go-ipp-server-capability.md) §E.2 补充（缺失 `-supported` 系列） |
| 简报对 `hashicorp/mdns` 标注 `[未验证]` 注册能力 | 实际**支持**注册（`NewMDNSService` + `Zone`） | 已在附录 A 改为"已验证支持" |
| 简报未提及 `hashicorp/mdns` 的 TXT 值需自行拼 `key=value` | 真实陷阱，且其 `NewServer` 内部用 `net.ListenMulticastUDP`（附录 D 证明不可靠） | 已写入附录 A 与附录 D |
| 简报自述的 `web_search` 401 故障 | 已复现，确为环境问题，非子代理失误 | 已在[本目录 README](README.md) 记录影响与应对 |

## F.3 仍**未被独立复核**的项（诚实声明）

以下内容**尚未**取得一手来源确认，**不得当作已证实事实**（已在各文档中标注 ⚠️）：

1. **AirPrint 的确切要求**（`urf` TXT 取值、`_universal._sub._ipp._tcp` 是否存在、必需属性清单）
   —— Apple 规范原文本次未能取得。**必须用 `ippeveprinter` + `dns-sd` 实证。**
2. **Windows 内置 IPP 客户端实际发送的文档格式** —— 决定 `document-format-supported` 的内容。
3. **Windows Protected Print Mode (WPPM)** 的实际行为（官方页面 404）。
4. **LPR Port Monitor 是否为 Windows 可选功能**（枚举需管理员权限，本次未提权）。
5. **IPP Everywhere 必需操作集"恰好 8 个"** 的精确性 —— 方向可信，具体数字待用 PWG 5100.14 复核。
6. **`cups-filters` 的 SPDX 标识**（only vs or-later）未核实。
7. **Ghostscript AGPL 的原文页**未取得（结论方向可信，但缺一手 URL）。

**这些都是"动工前 P0 清单"的一部分**，见[本目录 README](README.md)。

## F.4 复核结论

**调研的三条主干结论（IPP+DNS-SD 协议、Go 技术栈、不做驱动栈）在本次独立复核后依然成立**，
其中"否决 Rust"与"CUPS 为 Apache-2.0"两条**关键论据已逐字确认**。

**但有两处需要上调工作量预期**（均由 Lead 实测发现，见附录 D/E）：
1. `discovery/` **不能**用现成库的便利 API，必须自建 socket 并显式管理组播接口；
2. `pkg/attributes/` **必须**自研"带显式 tag 的 RFC 8010 属性写入器"，因为 `go-ipp` 缺少
   几乎所有 IPP Everywhere 必需的能力属性。

**净判断：Go 选型仍然成立，但"IPP 服务端工作量"比简报呈现的更重。**
这不改变方向，只影响排期 —— 且这两块都是**边界清晰、可用 `ipptool` 对拍验收**的模块。
