# ADR-0003：许可证策略

- **状态**：**待决（Proposed）** —— 需要项目发起人拍板
- **日期**：2026-02（会话日期）
- **依据**：[协议简报 §6 许可证](../research/01-protocols.md)、[ADR-0002](0002-stack-and-architecture.md)

## 背景

许可证选择会**反向约束架构**（尤其 AGPL 的 Ghostscript 与 GPL 的 cups-filters），
因此必须在写代码前定方向。调研确认风险**不集中在 CUPS 本身**，而集中在**光栅化环节**。

## 已核实的关键事实

| 组件 | 许可证 | 风险 | 说明 |
| --- | --- | --- | --- |
| **CUPS** | **Apache-2.0** | ✅ 低 | 官方许可证页已核实，含嵌入例外 + GPLv2 链接例外 → **商业友好** |
| `cups-filters` | **GPL-2** | ⚠️ 中 | copyleft；项目归属已核实，SPDX only/or-later 未核实 |
| **Ghostscript** | **AGPL** | 🔴 **高** | 最高风险项（Artifex 页面本次未取到原文，需复核） |
| `hashicorp/mdns` | MIT | ✅ 低 | 已核实 |
| `phin1x/go-ipp` | Apache-2.0 | ✅ 低 | 已核实 |
| `OpenPrinting/ipp-usb` | BSD-2-Clause | ✅ 低 | 已核实，**可直接借鉴代码** |
| `SimulPiscator/AirSane` | **GPL-3.0** | ⚠️ 中 | **仅可阅读，不可作代码基础** |
| `istopwg/ippsample` | Apache-2.0 | ✅ 低 | 可作对拍基准 |

## 核心决策：**用架构彻底绕开 Ghostscript（AGPL）**

Ghostscript 的唯一职责是**把 PDF/PostScript 渲染成"不懂页面描述语言的设备"能吃的位图**。

因此：**只要服务端只接受 PDF / PWG Raster / URF / PostScript 作为输入，就完全不需要 Ghostscript。**

理由链：
1. 现代设备（AirPrint / IPP Everywhere）**自己能吃 PDF 或 PWG Raster** → 无渲染需求；
2. 走 `driver/winspl` 时，**光栅化由 Windows spooler + 厂商驱动完成**（系统组件，与我们的许可证无关）；
3. 因此渲染这一步在我们架构中**根本不存在**。

**→ 这是 ADR-0002 中"不做驱动栈"决策的第二个收益：不仅省体积，还彻底消除 AGPL 风险。**

## 建议

**建议采用 Apache-2.0**（与 CUPS、go-ipp 一致，商业友好、专利授权明确）。

理由：核心代码不链接任何 GPL/AGPL 组件；插件契约是**可执行文件 + stdin/stdout 边界**，
属"独立进程通过通用接口通信"，**不构成衍生作品**，因此允许用户自行编写 GPL 插件而不传染主程序。

⚠️ **但这需要法律确认**，本 ADR 不能替代法务意见。

## 待决问题（需发起人拍板）

1. **项目目标**：纯自用 / 开源 / 未来商业化？→ 决定许可证选择空间。
2. **是否接受 GPL-2 的 `cups-filters`** 作为可选的 Linux 侧插件（若接受，可作为独立进程隔离以降低传染风险）。
3. **Ghostscript 兜底**：若未来必须支持"哑打印机"，是否愿意引入 AGPL 组件？
   建议**永不引入**，改为要求用户自行安装（用户自装 ≠ 我们分发）。

## 后果

- 选 Apache-2.0 **正面**：可直接借鉴 `ipp-usb`(BSD-2) 与 `go-ipp`(Apache-2.0) 代码；商业路径开放。
- **负面**：不能直接复用 `cups-filters` 与 `AirSane` 代码，需要自研相应能力。
- **风险**：若未来想支持老式哑打印机，可能被迫面对 GPL/AGPL 抉择 —— 因此**插件契约的进程边界必须从一开始就严格遵守**，
  这是保有"未来可以引入 GPL 插件"这一选项的前提。

## 参考

- CUPS 许可证页：<https://github.com/OpenPrinting/cups/blob/master/LICENSE>
- Ghostscript 许可证：<https://www.artifex.com/licensing/agpl/>
- AGPL 与网络服务：<https://www.gnu.org/licenses/agpl-3.0.html>
