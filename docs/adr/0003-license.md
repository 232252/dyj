# ADR-0003：许可证策略

- **状态**：**已接受（Accepted）** —— 项目发起人于本次会话拍板：**采用 Apache-2.0**
- **日期**：2026-02（会话日期）
- **依据**：[协议简报 §6 许可证](../research/01-protocols.md)、[ADR-0002](0002-stack-and-architecture.md)

## 背景

许可证选择会**反向约束架构**（尤其 AGPL 的 Ghostscript 与 GPL 的 cups-filters），
因此必须在写代码前定方向。调研确认风险**不集中在 CUPS 本身**，而集中在**光栅化环节**。

## 已核实的关键事实

| 组件 | 许可证 | 风险 | 说明 |
| --- | --- | --- | --- |
| **CUPS** | **Apache-2.0** | ✅ 低 | 官方许可证页已核实，含嵌入例外 + GPLv2 链接例外 → **商业友好** |
| `cups-filters` | **Apache-2.0-with-GPL2-LGPL2-Exception** | ✅ 低 | **已更正**：`COPYING` 原文为 Apache-2.0 + GPL2/LGPL2 例外，**非 GPL-2**，无 copyleft 风险 |
| **Ghostscript** | **AGPL** | 🔴 **高** | 最高风险项（Artifex 页面本次未取到原文，需复核） |
| `hashicorp/mdns` | MIT | ✅ 低 | 已核实 |
| `phin1x/go-ipp` | Apache-2.0 | ✅ 低 | 已核实 |
| `OpenPrinting/ipp-usb` | BSD-2-Clause | ✅ 低 | 已核实，**可直接借鉴代码** |
| `SimulPiscator/AirSane` | **GPL-3.0** | ⚠️ 中 | **仅可阅读，不可作代码基础** |
| `istopwg/ippsample` | Apache-2.0 | ✅ 低 | 可作对拍基准 |

## 核心决策：**用架构彻底绕开 Ghostscript（AGPL）**

Ghostscript 的唯一职责是**把 PDF/PostScript 渲染成"不懂页面描述语言的设备"能吃的位图**。

因此：**只要服务端只接受 PWG Raster / PDF / JPEG 作为输入，就完全不需要 Ghostscript。**

理由链：
1. 现代设备（AirPrint / IPP Everywhere）**自己能吃 PWG Raster、PDF 或 JPEG** → 无渲染需求
   （PWG 原文：*"Required: ... PWG Raster and JPEG JFIF file formats (JPEG only required for color printers).
   Recommended: PDF"*，见 [ADR-0002 D3](0002-stack-and-architecture.md)）；
2. 走 `driver/winspl` 时，**光栅化由 Windows spooler + 厂商驱动完成**（系统组件，与我们的许可证无关）；
3. 因此渲染这一步在我们架构中**根本不存在**。

**→ 这是 ADR-0002 中"不做驱动栈"决策的第二个收益：不仅省体积，还彻底消除 AGPL 风险。**

> ⚠️ **注意 `URF` 的定位**：`image/urf` 是 **Apple AirPrint 扩展，不是 PWG 必需格式**。
> 面向 AirPrint 可以额外声明，但**不能拿它顶替 PWG Raster**。

## 决策

✅ **采用 Apache-2.0**（与 CUPS、`go-ipp` 一致，商业友好、专利授权明确）。

理由：核心代码不链接任何 GPL/AGPL 组件；插件契约是**可执行文件 + stdin/stdout 边界**，
属"独立进程通过通用接口通信"，**不构成衍生作品**，因此允许用户自行编写 GPL 插件而不传染主程序。

⚠️ 本 ADR 不替代法务意见；若未来商业化，建议由法务复核。

## 已决问题（发起人拍板）

1. **项目目标**：明确**保留未来商业化路径** → 因此必须避开一切 copyleft 传染，
   这正是选 Apache-2.0 而非 GPL 类许可证的决定性理由。
2. ~~是否接受 GPL-2 的 `cups-filters`~~ → ✅ **问题消失**：`cups-filters` 实为
   **Apache-2.0-with-GPL2-LGPL2-Exception**（已核实 `COPYING` 原文），**商业友好，无 copyleft 风险**。
   我们仍不采用它的**驱动/光栅化栈**，但理由是**体积（246 MB）与维护成本**，**不是许可证**。
3. **Ghostscript 兜底**：若未来必须支持"哑打印机"，是否愿意引入 AGPL 组件？
   建议**永不引入**，改为要求用户自行安装（用户自装 ≠ 我们分发）。

## 后果

- 选 Apache-2.0 **正面**：可直接借鉴 `ipp-usb`(BSD-2) 与 `go-ipp`(Apache-2.0) 代码；商业路径开放。
  且实测确认 **CUPS 与 cups-filters 均为 Apache-2.0**，整条 CUPS 生态**没有 copyleft 传染风险**。
- **负面**：`AirSane` 是 **GPL-3.0**，**仅可阅读、不可复用代码**（架构思想可借鉴）。
- **风险**：若未来想支持老式哑打印机，可能被迫面对 Ghostscript/AGPL 抉择 —— 因此**插件契约的进程边界必须从一开始就严格遵守**，
  这是保有"未来可以引入独立进程 GPL/AGPL 插件"这一选项的前提。

## 参考

- CUPS 许可证页：<https://github.com/OpenPrinting/cups/blob/master/LICENSE>
- Ghostscript 许可证：<https://www.artifex.com/licensing/agpl/>
- AGPL 与网络服务：<https://www.gnu.org/licenses/agpl-3.0.html>
