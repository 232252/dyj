# ADR-0002：技术栈与共享架构选型

- **状态**：已接受（待 P0 实证后可能修订）
- **日期**：2026-02（会话日期）
- **决策者**：项目发起人 + 调研结论
- **依据**：[调研简报 02](../research/02-prior-art-and-stacks.md)、[协议简报 01](../research/01-protocols.md)、[附录 A 阻塞项实证](../research/03-verified-blockers.md)、[附录 B 微软时间表](../research/04-microsoft-driver-timeline.md)、[附录 C 本机实测](../research/05-environment-probe.md)

## 背景

要做"局域网共享打印机，Windows/Linux/macOS/iOS 客户端免装驱动"，需要选定：
① 共享协议；② 主语言；③ 驱动/设备接入方式；④ 模块拆分方式。
约束为项目价值观：**体积小、好维护、长眼光、无巨石**。

## 决策

### D1. 协议：以 **IPP（RFC 8010/8011）+ DNS-SD（RFC 6762/6763）** 为唯一主路径

**理由**：这是唯一同时满足"三平台原生支持"和"长期有效"的协议组合。
且**微软官方时间表在系统性淘汰第三方驱动**（2026-07-01 起驱动排序永远优先内置 IPP 类驱动；
2027-07-01 起第三方驱动更新终结 —— 见附录 B 原文核实）。
**IPP-first 的价值随时间单调上升，SMB/驱动分发路线单调下降。**

**明确拒绝**：
- ❌ **SMB / Point-and-Print 作为主路径** —— 微软正在主动降级它，且需远程装驱动、提权，是经典故障源。
- ❌ **9100 裸端口作为主路径** —— 无能力协商、无状态回传、需客户端自带驱动，违背"零驱动"目标。可作为**插件后端**存在。
- ❌ **依赖 CUPS 作为核心** —— **CUPS 在 Windows 上根本不存在**，而目标机器很可能就是 Windows（本机实测正是 Windows）。至多作为 Linux/macOS 的驱动插件。

### D2. 主语言：**Go**

**理由**：
1. **三平台一等公民**：`GOOS=windows/linux/darwin` 产出免运行时静态二进制。本机 Go 工具链已就绪。
   对比：Python 需 PyInstaller（体积膨胀 5–10 倍）；C/C++ 需维护 MSVC/MinGW + libusb + avahi 交叉构建矩阵。
2. **唯一同时具备"许可证友好的 IPP 编解码"与"活跃 MIT 的 mDNS 库"的生态**：
   - IPP：`phin1x/go-ipp`（Apache-2.0）—— **已实证具备服务端编解码能力**（附录 A）
   - mDNS：`hashicorp/mdns`（MIT，活跃）—— **已实证支持服务注册**（附录 A）
3. **有官方同路线先例**：`OpenPrinting/ipp-usb`（Go，BSD-2-Clause）已验证该技术路线，
   且其 README 完整记录了 IPP-over-USB 的实战陷阱，可直接借鉴。

**否决 Rust 的理由**：唯一成熟 IPP 库 `ancwrd1/ipp.rs` **仓库中无 LICENSE 文件**（法律上不可用）；
MIT 替代品 `heeen/ipp-printer-app` 仅 2★。**IPP 生态尚未成熟到可押注。**
→ 但保留 **Rust 可写插件** 的能力（见 D4 插件契约）。

**否决 C/C++ 作为主语言的理由**：内存安全风险高；Windows 交叉构建复杂；会把项目绑上 CUPS 的重构节奏。

### D3. 设备接入：**优先"转发给已有打印队列"，不碰 USB 解绑**

**这是本机实测直接推翻默认假设后的决策。**

本机实测发现目标设备是 **WSD/网络接入**的 Epson（非 USB），且 Windows 上
`usbprint.sys` 会**独占** USB 打印机（第三方进程无法直接写端点），走 IPP-over-USB 需解绑并改用 WinUSB，
**属侵入性操作且会破坏系统原生打印**。

因此**首个可用版本做 `driver/winspl` 后端**：把 IPP 作业转发给 Windows spooler 中**已存在的打印队列**
（本机即 `EPSONC42E48 (K200)`）。收益：
- 完全绕开 USB 独占问题；
- **复用系统已装的厂商驱动做光栅化**，我们**不需要实现驱动/PPD/filter 栈** —— 这正是 `cups-filters` 246 MB 的来源；
- 可在 Windows 上立即交付可用版本。

> ⚠️ **文档格式必须按 PWG 的实际要求来声明（已核实原文，见下）。**
> [PWG IPP Everywhere 页面](https://www.pwg.org/ipp/everywhere.html) 原文：
> *"**Required**: IPP/2.0, DNS-SD, **PWG Raster and JPEG JFIF** file formats (JPEG only required for color printers)
> **Recommended**: PDF, IPP-USB"*
>
> 因此 `document-format-supported` 的构成应为：
> - **必需**：`image/pwg-raster`；**彩色机型另需 `image/jpeg`**；
> - **推荐**：`application/pdf`；
> - ⚠️ **`image/urf` 不是 PWG 必需项**（它是 Apple AirPrint 扩展）。
>   面向 AirPrint **可以**额外声明 `urf`，但**不能拿它顶替 PWG Raster**。
>
> **注意**：这意味着**服务端自身不需要光栅化**（PDF/PWG/JPEG 都是"已光栅化或可直接送设备"的格式），
> 从而仍然绕开 Ghostscript；但**必须正确声明这些格式**，否则彩色机型上不满足 IPP Everywhere 一致性。
> ⚠️ 本机目标机型（Epson L3250 量级）**是彩色机**，故 **JPEG JFIF 属于必需**。

**后续按需通过插件扩展**：`driver/ippusb`（USB 7/1/4 设备）、`driver/ipp`（真 IPP 设备直转）、`driver/rawlp`（Linux `/dev/usb/lp0`）。

### D4. 模块拆分：**核心同进程 + 驱动层可独立进程 + 插件契约**

```
cmd/            printd(守护进程)  printctl(CLI)  print-plugin-*(插件进程)
pkg/
  transport/    HTTP/HTTPS、连接管理、chunked、请求净化
  ipp/          RFC 8010 编解码 + RFC 8011 操作语义
  attributes/   IPP Everywhere 必需属性表构造与校验
  discovery/    mDNS/DNS-SD 通告 + 浏览 + 冲突解决(需自研)
  spooler/      作业队列、状态机、持久化
  driver/       后端适配器**接口**（本项目唯一的扩展缝）
  config/       分层配置（全局 / per-device / access 白名单）
```

**理由**：
- **核心同进程**：`ipp-usb` 实证"单进程处理所有设备内存更省"，拆微服务得不偿失；
- **驱动层隔离**：驱动是变化最剧烈的部分，隔离后**新增打印机支持不触碰核心代码**；
- **插件契约照抄 CUPS backend 模型**（经 `CUPS-PDF` 等大量第三方验证）：
  可执行文件 + `argv`（`job-id user title copies options [file]`）+ `stdin` 数据 +
  环境变量元数据 + `stderr` 回传状态（`STATE:` / `ATTR:` / `PAGE:`）。
  **零语言绑定、零 ABI 负担、天然进程隔离**，且允许用任何语言写插件。
  （Go 自带 `plugin` 包跨平台性差，明确不用。）

## 备选方案与拒绝理由

| 方案 | 拒绝理由 |
| --- | --- |
| 薄控制面 + shell out 到 CUPS | **CUPS 在 Windows 上不存在**，直接违背跨平台目标（本机正是 Windows） |
| Python + 成熟生态 | 分发体积膨胀 5–10 倍 + Windows Service 需 pywin32；**且无服务端 IPP 库**，省不下工作量 |
| C/C++ + libcups | 跨平台构建矩阵复杂；绑上 CUPS 3.x 激进重构节奏（`cups-sharing` 已停滞两年） |
| Rust 主语言 | 唯一成熟 IPP 库无 LICENSE；替代品 2★ |
| 单一大进程塞所有厂商逻辑 | 直接违背"无巨石"；厂商怪癖会污染核心调度器 |
| 微服务化（每模块一进程） | 核心路径 IPC 开销得不偿失；`ipp-usb` 实证单进程更省内存 |

## 后果

- **正面**：三平台免运行时分发；零驱动客户端体验；驱动层可无限扩展而不动核心；许可证风险可控（见 ADR-0003）。
- **负面**：**IPP 服务端生态在任何语言中都近乎空白**，RFC 8010 编解码与 IPP Everywhere 必需属性表需大量自研。
  实测（[附录 E](../research/07-go-ipp-server-capability.md)）：`go-ipp` 的 `AttributeTagMapping` **只认识 70 个属性名**，
  **缺少几乎所有 IPP Everywhere/AirPrint 必需的 `-supported`/`-default` 属性**
  （`document-format-supported`、`operations-supported`、`urf-supported`、`media-supported` 等均缺失），
  且 `Encode` 遇未知属性**直接报错**、无绕过参数。
  → **必须自研"带显式 tag 的 RFC 8010 属性写入器"**，放在 `pkg/attributes/`，用 `ipptool` 对拍验收。
  可复用的部分是：`NewRequestDecoder`（请求解析）、属性值类型解码、报文框架、以及
  **自动保证 `attributes-charset` 第一 / `attributes-natural-language` 第二**的强制顺序；
  `Response.Encode()` 已导出，**无需 fork**。
- **风险**：
  1. `hashicorp/mdns` **不含**服务名冲突解决、`_universal._sub` 子类型、goodbye 包，需自研（附录 A）；
     且它的 `NewServer` **忽略了监听错误**（`ipv4List, _ := net.ListenMulticastUDP(...)`），
     部分失败会**静默降级**；还需**显式传入 FQDN hostName + IP 列表**，
     否则其 `NewMDNSService` 会回退到 `os.Hostname()`（Windows 上是 NetBIOS 名，无法经 mDNS 解析）
     导致 SRV 记录目标错误或构造失败。`discovery/` 应自行管理 socket 以固定接口与错误处理；
  2. ~~Chrome 占用 UDP 5353~~ → ✅ **已实证解除**（附录 D）：多进程共存可行；
     但**必须显式指定组播接口**（本机实测默认组播走 `optun` 而非主 LAN `以太网`）。
     ⚠️ 附录 D 曾把"收不到包"误诊为"Go 不设 `SO_REUSEADDR`"——**该归因已撤回**，
     真正原因是**默认接口选错**（Go 实际会设置 `SO_REUSEADDR`，见[附录 D §D.6](../research/06-mdns-coexistence-verified.md)）；
  3. CUPS 3.x 正在激进重构 —— 但我们自己实现 IPP，反而受益于其"driverless only"转向。

## 待验证的 P0 项（动工前 / 首个版本必须实证）

见 [`docs/research/README.md`](../research/README.md) 的"P0 待实证"清单。其中最关键：
**自研 IPP 服务端能否被 Windows 11 直接添加成功**（本项目的核心验收标准）。
