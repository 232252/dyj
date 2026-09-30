# 调研索引（Phase 0）

本目录是项目动工前的调研产物。**结论已并入 `docs/adr/`，此处保留完整论据与来源。**

> **状态更新（2026-09-29，v0.1 交付后）**：P0 清单 #1、#2、#9、#10、#11 已在实现轮
> 完成代码级实证（见 `docs/adr/0005`、`0006`、`0007` 与 CHANGELOG）。
> **#1 的 GUI 实测（Win11 添加打印机）仍待真机验收**——代码路径已被
> 黄金字节测试 + 独立解码器交叉验证 + 二进制 E2E 覆盖，但 ADR-0004 的 A4
> （真实 Win11 添加）仍是开放项。#4（AirPrint/URF）已按 ADR-0007 显式降级为"不声明"。

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
| [`08-independent-review.md`](08-independent-review.md) | **附录 F**：对调研结论的独立复核（含"否决 Rust"的关键论据确认） | ✅ Lead 复核 |
| [`10-ui-and-platform-deployment.md`](10-ui-and-platform-deployment.md) | **Phase 1 前瞻**：GUI 选型（Web 管理页+可选托盘）、控件组件化、三平台安装/自启/防火墙/签名/分发（Win: Inno+SCM；Linux: nfpm+systemd+avahi；macOS: launchd+brew）；含 `service_unix.go` 修正清单与 cups 后端 gap | ✅ 调研产出（待 ADR 定稿） |
| [`12-windows-acceptance-2026-09-30.md`](12-windows-acceptance-2026-09-30.md) | **附录 G**：v0.2 后 Windows 物理机验收（wingdi 真驱动无纸链路、跨平台客户端矩阵、管理页鉴权；发现的 TLS 中文主机名崩溃与多网卡 mDNS 缺口及修复）；含 10.126.126.2 交接 runbook | ✅ Lead 实测 |
| [`13-zero-config-redesign.md`](13-zero-config-redesign.md) | **v0.3 详细设计**：单程序角色向导（共享本机打印机 / 添加网络打印机）、4 位码配对与 URI 路径 token 鉴权、接收端自动装系统队列（WPS 直接可见）、进程内热生效、多打印机模型；可观测性契约（结构化事件日志、文件日志、24 项检查点表、doctor 客户端扩展、故障树）；P0–P3 阶段与验收标准 | ✅ 设计产出（决策见 [ADR-0010](../adr/0010-role-wizard-pairing.md)，待实施） |

## 三句话结论

1. **协议**：IPP + DNS-SD 是唯一同时满足"三平台原生支持"与"长期有效"的组合；
   微软官方时间表正在淘汰第三方驱动，**IPP-first 的价值逐年上升**（附录 B）。
2. **栈**：**Go**（三平台免运行时静态二进制 + 唯一同时具备可用 IPP 库与活跃 MIT mDNS 库的生态）。
3. **架构**：**不做驱动栈、不碰 USB 解绑** —— 把 IPP 端点架在"已有打印队列"之上（`driver/winspl`），
   复用系统驱动完成光栅化。既省体积，又**彻底绕开 Ghostscript AGPL 风险**。

## ⚠️ P0 待实证清单（动工前必须做，**不要当成已知事实**）

按优先级排序。**每一项都必须在写生产代码前用实测回答。**

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
| ~~**11**~~ | ~~`operations-supported` 1setOf enum 的字节宽度~~ | ✅ **已实证并修正**：RFC 8010 §3.8 强制 **4 字节**（附录 F 红队报告 §5），2 字节版本会让独立解码器失步；黄金测试锁定 | — |
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
