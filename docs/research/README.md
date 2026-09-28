# 调研索引（Phase 0）

本目录是项目动工前的调研产物。**结论已并入 `docs/adr/`，此处保留完整论据与来源。**

## 文档

| 文档 | 内容 | 状态 |
| --- | --- | --- |
| [`01-protocols.md`](01-protocols.md) | IPP/CUPS/AirPrint/DNS-SD/WSD/LPD/9100 协议全景；7 种服务端架构对比表 | ✅ 调研员产出（655 行，79 URL） |
| [`02-prior-art-and-stacks.md`](02-prior-art-and-stacks.md) | 既有实现（CUPS/ipp-usb/AirSane/ippsample/ipp-sharing）与技术栈对比 | ✅ 调研员产出（727 行，102 URL） |
| [`03-verified-blockers.md`](03-verified-blockers.md) | **附录 A**：对简报两个"待验证阻塞项"的一手源码实证 | ✅ Lead 实证 |
| [`04-microsoft-driver-timeline.md`](04-microsoft-driver-timeline.md) | **附录 B**：微软打印驱动淘汰时间表（一手原文逐条核实） | ✅ Lead 实证 |
| [`05-environment-probe.md`](05-environment-probe.md) | **附录 C**：本机真实环境实测（网络/打印机/mDNS 占用） | ✅ Lead 实测 |
| [`06-mdns-coexistence-verified.md`](06-mdns-coexistence-verified.md) | **附录 D**：P0 #3 实证 —— mDNS 与 Chrome 共存**已解除阻塞** | ✅ Lead 实测 |

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
| **1** | **自研 IPP 服务端能否被 Windows 11 直接添加成功？** | **本项目核心验收标准**；若不成立则整个方案不成立 | 起最小 IPP 服务端 → Win11「添加打印机(IP 地址)」→ 真机打印 |
| **2** | **Windows 内置 IPP 客户端实际发送什么文档格式**（PDF？PWG Raster？XPS？） | 直接决定 `document-format-supported` 必须包含什么，否则 Windows 打不出来 | `ipptool` 或抓包观察真实 Windows 请求 |
| ~~3~~ | ~~mDNS 能否与已占用 5353 的 Chrome 共存？~~ | ✅ **已实证解除**（[附录 D](06-mdns-coexistence-verified.md)） | 见附录 D：**共存可行**，但**禁用 `net.ListenMulticastUDP`**，须自建 socket + 显式指定接口 |
| **4** | AirPrint 的确切要求（`urf` TXT 取值、`_universal._sub._ipp._tcp` 是否存在、必需属性） | 决定 macOS/iOS 能否发现；**Apple 规范原文本次取不到** | 跑 `ippeveprinter`，用 `dns-sd -B/-L` 观察其**实际**广播的 TXT 键 |
| **5** | IPP Everywhere 必需属性表能否通过 `ipptool` 官方测试套件？ | 属性缺失是 AirPrint 报错 21345 类问题的根源 | `ipptool` + `get-printer-attributes-suite.test` 对拍 |
| **6** | 打印机休眠时端口全关（附录 C.4），如何判断设备可用性？ | 不能用端口扫描判活；影响作业失败处理 | 走 spooler 队列状态查询而非端口扫描 |
| **7** | LPR Port Monitor / Internet Printing Client 是否 Windows 可选功能及其名称 | 调研未核实；影响客户端引导文档 | 提权后 `Get-WindowsOptionalFeature` |
| **8** | Windows Protected Print Mode (WPPM) 的实际行为 | 官方正文取不到（404），不要断言 | 需权威来源或实测 |
| **9** | `go-ipp` 响应编码器为包内私有，构造响应的可行路径 | 影响 `ipp/` 模块工作量 | fork 加导出包装 vs 自行按 RFC 8010 序列化，二选一 |

## 来源可靠性说明

- **调研员报告**中的仓库指标（stars/license/size/pushed_at）均通过 **GitHub REST API 实测**，非搜索摘要。
- 本次会话中 **`web_search` 工具全程不可用**（DeepSeek API 返回 HTTP 401），
  所有调研均通过直接 `web_fetch` 已知权威 URL + GitHub API 完成。
- 凡未取得权威原文的论断，均显式标注 `[未验证]` / ⚠️，**未编造任何 RFC 编号、端口或 API 细节**。
- 已知并已修正的简报事实错误：
  - `savoirfairelinux/ipp-usb` **不存在**（404），正确为 `OpenPrinting/ipp-usb`；
  - `OpenPrinting/ippsample` **404**，正确为 `istopwg/ippsample`；
  - **RFC 3998 不是 INFRA**（它是 Administrative Operations）；INFRA 实为 PWG 5100.18，且本项目不需要；
  - **`ipps` 默认端口是 631 而非 443**（RFC 7472 §4.3）。
