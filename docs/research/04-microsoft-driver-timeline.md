# 附录 B：微软打印驱动淘汰时间表（一手原文核实）

> **核实方式**：Team Lead 直接 `web_fetch` Microsoft Learn 原文页面，逐句比对。
> **来源**：<https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows>
> **页面标注**："Deprecation Timeline Calendar - Update May 2025 update – Printer driver timeline date adjustment."
> **核实时间**：HTTP 200，正文 7,722 字符，于本次会话直接抓取。

## 为什么这份文档是全项目的战略基石

本项目要做的"局域网共享打印机，客户端免装驱动"在技术上是否**逆流**，完全取决于 Windows 客户端未来还认不认 IPP。
这份官文给出了明确答案：**微软正在主动把 Windows 打印栈推向 IPP，并在时间表上逐年切断第三方驱动。**

**即：IPP-first 路线的价值随时间单调上升，而 SMB/Point-and-Print 驱动分发路线的价值单调下降。**

## 原文时间表（逐条摘录，非转述）

| 时间 | 原文表述 | 含义 |
| --- | --- | --- |
| Win10 21H2 起 | "Windows offers inbox support for **Mopria compliant printer devices** over network and USB interfaces via the **Microsoft IPP Class Driver**" | Windows 已内置 IPP 类驱动，**网络与 USB 都支持** |
| **2026-01-15** | "For Windows 11+ and Windows Server 2025+, **no new printer drivers will be published to Windows Update**" | 第三方驱动不再进入 WU 分发 |
| **2026-01-15** | "new printer drivers ... will only be approved on a **case-by-case basis to be signed**" | 新驱动签名改为逐例审批 |
| **2026-07-01** | "Printer driver ranking order modified to **always prefer Windows IPP inbox class driver**" | **驱动排序永远优先内置 IPP 类驱动** —— 决定性一条 |
| **2027-07-01** | "Except for security-related fixes, **third-party printer driver updates will no longer be allowed**" | 第三方驱动更新终结 |

## 对本项目的四条硬结论

1. **技术方向被官方背书，不是逆流。** 我们不需要说服任何人"IPP 是未来"——微软的排期表已经替我们说了。
2. **"零驱动安装"体验是有官方路径保证的**：客户端走 Windows 内置 `Microsoft IPP Class Driver`。
   本机实测已在 Windows 11 上确认该驱动存在（见 [`05-environment-probe.md`](05-environment-probe.md)）。
3. **不要以 SMB / Point-and-Print 作为主路径。** 它在 2026-07-01 后会被系统的驱动排序策略**主动降级**，
   等于把产品建在正在下沉的地基上。这与简报 §7 的建议一致。
4. **Mopria 认证是"设备侧"的门槛，不是"服务端"的。** 原文说的是 Mopria **compliant printer devices**。
   我们要做的是让**共享出来的端点满足 IPP Everywhere / AirPrint 要求**，从而被 Windows 视为可用的 IPP 打印机。
   ⚠️ 但**"我们自研的 IPP 服务端能否被 Windows 判定为 Mopria/IPP 类驱动可用"这一点尚未实证** —— 见下方待办。

## ⚠️ 仍未核实 / 需实证的事项（不要当成已知事实）

以下条目调研员与 Lead **均未**取得权威原文，**不得在代码注释或文档中断言**：

1. **Windows 内置 IPP 客户端实际发送什么文档格式**（PDF？PWG Raster？XPS？）——
   这直接决定服务端 `document-format-supported` 必须包含哪些，否则 Windows 打不出来。
   → **实证方案**：起一个 IPP 服务端，用 `ipptool` 或抓包观察真实 Windows 请求。
2. **Windows Protected Print Mode (WPPM)** 的官方正文未能取得（页面重定向后 404）。
   "Win11 24H2 引入""要求 Mopria 认证"等具体论断**未获官方 URL 确证**。
3. **LPR Port Monitor 是否为 Windows 可选功能及其确切名称**未核实。
4. **自研 IPP 服务端能否被 Windows 11 的 "Add printer using IP address" 直接添加成功** ——
   这是本项目的 **P0 验收标准**，必须在第一个可用版本就用真机验证。

## 参考来源

- Microsoft Learn —— End of servicing plan for third-party printer drivers on Windows（2025-05 更新）：
  <https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows>
