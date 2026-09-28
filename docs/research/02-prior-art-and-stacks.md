# 既有实现（Prior Art）与技术栈调研简报

> 项目目标：在局域网内共享一台打印机，使 Windows / Linux / macOS 客户端能够打印。
> 项目价值观：**体积小、易维护、长期演进、拒绝单体（no monolith）**。
>
> 本文所有仓库指标（star / license / 语言 / 仓库体积 / 最后推送时间）均通过 **GitHub REST API `api.github.com/repos/...`** 实时查询获得，查询时间为 2026 年（见文末"数据获取方式"）。凡未能验证的内容一律显式标注 **[未验证]**。仓库 `size` 字段单位为 KB，由 GitHub 计算（含历史对象，通常显著大于工作树大小）。

---

## 目录

- [0. 方法论与一个重要更正](#0-方法论与一个重要更正)
- [A. 既有实现 / 竞品与相邻项目](#a-既有实现--竞品与相邻项目)
- [B. 服务端技术栈对比](#b-服务端技术栈对比)
- [C. 部署与运行时选项](#c-部署与运行时选项)
- [D. "拒绝单体"的设计模式](#d-拒绝单体的设计模式)
- [E. 结论与建议](#e-结论与建议)
- [参考来源](#参考来源)

---

## 0. 方法论与一个重要更正

**关于 `savoirfairelinux/ipp-usb`**：任务描述中提到的该项目**不存在**。查询 `https://api.github.com/repos/savoirfairelinux/ipp-usb` 返回 **HTTP 404 Not Found**。正确的项目是 [`OpenPrinting/ipp-usb`](https://github.com/OpenPrinting/ipp-usb)。这是一个需要修正的事实性错误——`savoirfairelinux` 是 Ring/Jami 的所属组织，与 ipp-usb 无关。

**关于 `AirPrint Bridge` 命名歧义**：GitHub 上并不存在一个权威的、唯一的 "AirPrint Bridge"。搜索 `airprint bridge` 返回 20 个结果，全部为个人项目。其中最相关的是 [`sapireli/AirPrint_Bridge`](https://github.com/sapireli/AirPrint_Bridge)（648 stars，但语言标记为 **Shell**，MIT）。任务描述把 `AirPrint Bridge`、`AirPrintBridge` 当作两个独立项目列出的假设**不成立**——它们只是同一命名空间下的不同个人仓库。

---

## A. 既有实现 / 竞品与相邻项目

### A.1 CUPS 生态（在位者 / incumbent）

CUPS（Common Unix Printing System）是事实标准，理解它的演进方向对本项目至关重要。

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 | 说明 |
|---|---|---|---|---|---|---|
| [OpenPrinting/cups](https://github.com/OpenPrinting/cups) | C | Apache-2.0 | 1763 | 125,890 KB (~123 MB) | 2026-09-26 | CUPS 2.x 主源码，"OpenPrinting CUPS Sources" |
| [OpenPrinting/cups-filters](https://github.com/OpenPrinting/cups-filters) | C | Apache-2.0 | 199 | 252,025 KB (~246 MB) | 2026-09-08 | "Everything which CUPS 2.x needs to be used on non-Mac systems"，含 filters / backends / driverless |
| [OpenPrinting/cups-browsed](https://github.com/OpenPrinting/cups-browsed) | C | Apache-2.0 | 51 | 228,787 KB (~223 MB) | 2026-08-23 | 局域网打印机浏览/自动发现，已被拆分为独立仓库 |
| [OpenPrinting/libcups](https://github.com/OpenPrinting/libcups) | C | Apache-2.0 | 83 | 94,124 KB (~92 MB) | 2026-09-26 | **CUPS 3.x 库源码**，最新 release **v3.0.3**（2026-08-20） |
| [OpenPrinting/cups-local](https://github.com/OpenPrinting/cups-local) | C | Apache-2.0 | 8 | 936 KB | 2025-10-29 | CUPS 3.0 本地服务器，"Local server for CUPS 3.0" |
| [OpenPrinting/cups-sharing](https://github.com/OpenPrinting/cups-sharing) | **M4** | Apache-2.0 | 9 | 100 KB | 2024-01-12 | CUPS 3.0 共享服务器 |

**关键观察：CUPS 3.x 的"仅无驱动（driverless only）"转向已经落地，而且它把单体拆开了。**

[OpenPrinting CUPS v3 官方页面](https://openprinting.github.io/cups/cups3.html) 明确表述：CUPS v3 支持打印到 AirPrint / IPP Everywhere 打印机，以及对网络和本地（USB）打印机通过 **Printer Applications（打印机应用）** 支持。CUPS v3 提供 System V（`lp`）与 Berkeley（`lpr`）命令行、C API，以及**本地的和网络的共享服务器（local and network sharing servers）**。

架构上最重要的信号是：**CUPS 3.x 不再是一个"cupsd 巨物"，而是被拆成 4 个仓库**——`libcups`（库）、`cups-commands`、`cups-local`（本地 spooler/服务器）、`cups-sharing`（网络共享服务器）。这与本项目的"no monolith"价值观**方向完全一致**，是极强的外部背书。

注意 `cups-sharing` 的最后推送是 **2024-01-12**（距今约两年），语言标记为 M4（autotools 宏），说明该组件**仍在早期、活跃度低**。

**我们能学到/借鉴什么：**
1. **CUPS 3.x 把"本地 spooler"与"网络共享"拆成两个独立可执行体。** 这正是本项目应该做的事情——共享层不应与打印队列耦合。
2. **`ippeveprinter`（IPP Everywhere Printer Application）** 是 CUPS 3.x 里提供的参考实现，见 [libcups 文档列表](https://openprinting.github.io/cups/cups3.html)。它证明"用 IPP 协议暴露一个虚拟打印机"是被官方认可的模式。
3. **CUPS 生态的 License 是 Apache-2.0**（libcups / cups / cups-filters 全部如此），对商业友好。注意 CUPS 3 的 license 带有"允许链接 GPL2-only 软件"的例外条款。
4. **不要重写 CUPS 的驱动/光栅化那一层**——cups-filters 仓库体积 246 MB，是几十年积累的坑。我们的项目应该**只做 IPP 转发/共享**，把格式转换（如果有）作为可选插件。

---

### A.2 `ippsample` —— OpenPrinting/PWG 的 IPP 示例实现

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 |
|---|---|---|---|---|---|
| [istopwg/ippsample](https://github.com/istopwg/ippsample) | C | Apache-2.0 | 249 | 14,261 KB (~14 MB) | 2026-07-04 |

**注意**：该项目的正确位置是 `istopwg/ippsample`（ISTO-PWG 组织），**不是** `OpenPrinting/ippsample`——后者返回 **HTTP 404**。

描述为 "IPP sample implementations."，topics 包含 `ipp`、`ipp-clients`、`ipp-printers`、`printer`、`proxies`。它包含 `ippserver`（示例 IPP 服务器）、`ippclient` 以及 `ipptool` 等工具链。

**它能提供什么：**
- **一份可运行的、用 C 写的 IPP 服务器参考实现**，其 IPP 二进制编码/解码逻辑（RFC 8010 的 wire format）是**可以直接阅读并作为正确性基准**的。
- 它的 `ippserver` 是"用 IPP 暴露一个虚拟打印机"的最小可用形态——**是验证本项目 IPP 实现是否合规的最佳对拍工具**。

**[未验证]** 该仓库具体的二进制体积与发行包大小未能确认。

---

### A.3 ipp-usb —— OpenPrinting 的 USB 打印机 → 网络 IPP（**最重要的架构范本**）

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 | Open Issues |
|---|---|---|---|---|---|---|
| [OpenPrinting/ipp-usb](https://github.com/OpenPrinting/ipp-usb) | **Go** | **BSD-2-Clause** | 210 | 562 KB | 2026-08-16 | 31 |

这是我**最推荐深入研究**的项目。它的 README 揭示了一个极其重要的技术陷阱，任何做 USB 打印共享的项目都必须知道：

> IPP-over-USB 的朴素实现（把 TCP 连接直接中继到 USB）**是行不通的**。原因是：TCP 连接在客户端关闭时有"丢弃服务端已发送数据"的副作用，但 USB 连接**没有**这个副作用。对于 USB，未被客户端接收的数据会**留在 USB 缓冲区里**，下次客户端连接设备时会收到上一次异常结束的请求残留的数据。README 直言："Actually, it's an obvious flaw in the IPP-over-USB standard, but we have to live with it."

因此**实现必须完整读取整个 HTTP 响应**，也就意味着**必须理解 HTTP 协议**，实质上是实现一个 **HTTP 反向代理**，后端是 IPP-over-USB 连接。

**ipp-usb 的实际功能（来自 README，均为已验证的一手描述）：**
- 实现 HTTP 代理，后端为到 IPP-over-USB 设备的 USB 连接
- 完整支持 IPP 打印、eSCL 扫描、Web 管理界面
- 为所有支持的服务做 **DNS-SD 广播**
- IPP 的 DNS-SD 参数来自 **IPP get-printer-attributes 查询**
- eSCL 的 DNS-SD 参数来自解析 `GET /eSCL/ScannerCapabilities` 响应
- **TCP 端口分配与设备绑定**（VendorID + ProductID + 序列号三元组），多设备时端口保持稳定，**分配结果持久化到磁盘**
- **自动 DNS-SD 名称冲突解决**，最终名称持久化到磁盘
- 可由 **UDEV 触发**，也可独立运行
- 可共享给网络其他机器，**或仅监听 loopback**
- 可生成非常详细的排障日志

**"Under the hood"（README 原文列举的隐性工作，价值极高）：**
- 客户端 HTTP 连接与打印机侧 HTTP-over-USB 连接**完全解耦**
- HTTP 请求被**净化（sanitize）**，缺失的 header 会被补齐
- HTTP 协议在需要时从 **1.0 升级到 1.1**
- 阻止 Web 控制台不明智地发起 **WebSocket 升级**——因为这会长时间独占 USB 连接
- 客户端请求在所有可用的 **2–3 条 USB 连接间做负载均衡**，与客户端连接数/持久性无关
- 客户端断开在任何情况下都被正确处理，**即使是在发送中途**；最坏情况打印机可能收到被截断的文档，但 **HTTP 事务始终被正确完成**

**依赖与体积（README 原文）：**
- 外部依赖仅：`libusb`（USB 访问）、`libavahi-common` + `libavahi-client`（DNS-SD）、以及**需要运行 Avahi 守护进程**
- README 明确承认："Being written on Go, ipp-usb has a **large executable size**. However, its memory consumption is not very high. When single device is connected, ipp-usb RSS is similar or even slightly less in comparison to ippusbxd." 而且因为它在**单进程内处理所有设备**，服务 2 台以上设备时内存占用明显低于 ippusbxd。
- **这是一个重要的诚实数据点：Go + cgo（libusb/avahi）的二进制体积偏大。**

**部署方式（README 已验证）：**
- Debian / Fedora / openSUSE / Ubuntu 发行版二进制包
- **FreeBSD ports**（`print/ipp-usb`），README 表示希望扩展到 NetBSD/OpenBSD
- **Snap**：`sudo snap install --edge ipp-usb`
- **Docker/Rock**：`ghcr.io/openprinting/ipp-usb:latest`，运行参数为 `--network host -v /dev/bus/usb:/dev/bus/usb:ro --device-cgroup-rule='c 189:* rmw'`
- 经典安装通过 `make install`，安装 **UDEV rules** 与 **systemd service**（位于 `systemd-udev/`）

**关于 Avahi 的一个关键坑（README 原文）：** IPP-over-USB 通常只把打印机暴露到 **localhost**，因此需要 DNS-SD 能对 localhost 广播。这要求 **Avahi 0.8.0 或更新**；旧版本不支持对 localhost 广播。README 给出了自检方法（`avahi-publish -s test _test._tcp 1234` 配合 `avahi-browse _test._tcp -r`），并指出 Ubuntu/Fedora 打了补丁而 Debian 没有。还提供了仓库内的 `avahi/avahi-localhost.patch`。

**配置文件（已读取 `ipp-usb.conf` 原文）**，节选关键项：
```ini
[network]
  http-min-port = 60000
  http-max-port = 65535
  dns-sd = enable      # enable | disable
  interface = loopback # all | loopback
  ipv6 = enable        # enable | disable

[auth uid]
  all = *    # operations: all|config|fax|print|scan ; users: user|@group

[logging]
  device-log = all
  main-log = debug
  console-log = debug
  max-file-size = 256K
  max-backup-files = 5
  get-all-printer-attrs = false
```
注意 `get-all-printer-attrs` 的注释：某些企业级 HP 打印机返回巨量数据且极慢，会导致初始化超时，所以默认关闭。**这是真实世界的兼容性教训。**

**`[未验证]` 关于 releases**：`https://api.github.com/repos/OpenPrinting/ipp-usb/releases` 返回空数组 `[]`，即该仓库**没有使用 GitHub Releases 发布**（走发行版包/Snap/Docker），因此**无法从 GitHub 给出"最新版本号"**。

---

### A.4 扫描仪对应物：`AirSane` 与 `sane-airscan`（**最佳架构先例**）

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 |
|---|---|---|---|---|---|
| [SimulPiscator/AirSane](https://github.com/SimulPiscator/AirSane) | **C++** | **GPL-3.0** | 372 | 503 KB | 2026-09-27 |
| [alexpevzner/sane-airscan](https://github.com/alexpevzner/sane-airscan) | C | **NOASSERTION**（GitHub 无法识别，非标准 SPDX） | 369 | 1,636 KB | 2026-09-07 |

**AirSane**（README 原文，17,197 字节）：
- 是 **SANE 的前端，同时是一个扫描仪服务器**，支持 Apple 的 AirScan 协议
- 扫描仪自动检测，**通过 mDNS 发布**
- 采集的图像可以 JPEG / PNG / **PDF-raster** 格式传输
- 目标客户端：Apple Image Capture、[sane-airscan](https://github.com/alexpevzner/sane-airscan)（Linux）、Windows 10/11 内置 eSCL 客户端
- 除 eSCL 服务器功能外，**还提供简单的 Web 界面**
- **图像在采集过程中即时编码（on-the-fly）**，使内存/存储需求保持低位，因此**在 Raspberry Pi 或类似设备上运行良好**
- 认证与安全通信通过 **nginx 之类的代理服务器**配合实现（见 `README.https.md`）
- 实现了 **Mopria eSCL 协议**
- 提供 **systemd service 文件**，假设 `saned` 用户/组存在且有扫描仪访问权限
- 安装：`sudo apt-get install avahi-daemon && make && sudo make install && sudo systemctl enable airsaned`
- **默认监听端口 8090**，默认监听所有本地地址
- 配置文件：`/etc/default/airsane`，可配置 `INTERFACE`、`LISTEN_PORT=8090`、`UNIX_SOCKET`、`ACCESS_LOG`、`HOTPLUG=true`、`RELOAD_DELAY=1`、`MDNS_ANNOUNCE=true`、`ANNOUNCE_SECURE=false`、`ANNOUNCE_BASE_URL`、`WEB_INTERFACE=true`、`RESET_OPTION=false`、`DISCLOSE_VERSION=true`、`RANDOM_PATHS=false`、`COMPATIBLE_PATH=true`、`LOCAL_SCANNERS_ONLY=false`，以及 `OPTIONS_FILE` / `IGNORE_LIST` / `ACCESS_FILE` 路径
- **另有 `/etc/airsane/options.conf`**：支持按设备用**正则**匹配做 per-device 选项覆盖（`device <regex>` 节），以及 gamma 校正等
- **`/etc/airsane/ignore.conf`**：按正则忽略设备，**原始用途正是"避免与自动检测 eSCL 设备的后端形成回环"**
- **`/etc/airsane/access.conf`**：allow/deny IP 段访问控制，支持 `local on eth0` 语法

**为什么 AirSane 是本项目最好的架构先例：**
1. **它是"把本地硬件通过 mDNS + 标准协议暴露成网络服务"的成熟范例**，且项目规模很小（503 KB 仓库，30+ 个源文件量级）。
2. **它有 Web 界面 + daemon + systemd 的完整交付形态**，正是本项目需要的。
3. **它的配置分层（全局 / per-device / ignore / access）非常清晰**，值得照抄。
4. **它的 hotplug 与 reload-delay 设计**（`HOTPLUG=true`，`RELOAD_DELAY=1`）解决了 USB 热插拔抖动问题。

**注意 License 差异**：AirSane 是 **GPL-3.0**（传染性强）；`sane-airscan` 是 **NOASSERTION**——GitHub API 返回 `"license": {"key": "other", "spdx_id": "NOASSERTION"}`，意味着它使用自定义或非标准许可。**两者都不能随意作为闭源/宽松许可项目的基础**，只能作为架构阅读材料。

**sane-airscan** 描述为 "Scanner Access Now Easy - universal driver for eSCL (Apple AirScan) and WSD"——注意它同时支持 **eSCL 和 WSD** 两种协议，这是"多协议适配器"的范例。

---

### A.5 Windows 侧共享替代方案

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 | 说明 |
|---|---|---|---|---|---|---|
| [ArcticLampyrid/ipp-sharing](https://github.com/ArcticLampyrid/ipp-sharing) | **Rust** | **AGPL-3.0** | 16 | 5,569 KB | 2026-06-03 | **最直接的竞品** |
| [salmanasmat/Windows-AirPrint-Bridge](https://github.com/salmanasmat/Windows-AirPrint-Bridge) | Python | GPL-3.0 | 13 | 223 KB | 2026-09-14 | Python + Windows Service |
| [sapireli/AirPrint_Bridge](https://github.com/sapireli/AirPrint_Bridge) | Shell | MIT | 648 | 892 KB | 2026-09-24 | macOS 专用 |
| [tvrcgo/airprint-bridge](https://github.com/tvrcgo/airprint-bridge) | (null) | (none) | 0 | 6,747 KB | 2023-03-28 | 已停更 |
| [imjustafox/airprint-bridge](https://github.com/imjustafox/airprint-bridge) | Go | MIT | 10 | 60 KB | 2026-01-30 | 极小，无描述 |
| [KarpenkoDima/AirPrintBridge](https://github.com/KarpenkoDima/AirPrintBridge) | C# | MIT | 0 | 125 KB | 2026-07-16 | Windows C# |

**`ArcticLampyrid/ipp-sharing` 是本次调研发现的、与本项目目标最接近的项目**，其 README 原文给出：

- "IPP Sharing is a **lightweight tool to share local Windows printers drivelessly via IPP protocol**. While it is ideal for home users, it may not be as suitable for enterprise environments due to the absence of advanced features such as color management, custom paper sizes, user authentication, and audit logging."
- "This is a **Windows-only tool**, of course. For Linux and macOS users, CUPS is recommended..."
- **功能**：轻量易用；基础 Print Ticket 支持（标准纸张尺寸、方向、双面、色彩模式）；**Apple AirPrint 兼容**；客户端**无需驱动、无需 PPD**；**DNS-SD (Bonjour) 支持**
- **前置条件**：Windows 10 或更高（"required for Rust compatibility"）；**Apple Bonjour**（随 iTunes 或 Bonjour Print Services 分发）
- **配置**：YAML 格式，`server.addr`、`server.tls.cert/key`、`devices[]`（`name` / `info` / `target` / `uuid` / `basepath` / `dnssd`）
- 设备 `target` 直接指向 Windows 打印机名，例如 `"Microsoft Print to PDF"`
- **端口**：TCP **631**（IPP）与 UDP **5353**（DNS-SD/Bonjour）需放行
- **需要自签证书**（README 给出 openssl 命令），即**默认走 IPPS/TLS**

**这个项目的局限正是本项目的市场空缺：**
1. **仅 Windows** —— 而本项目要覆盖 Windows / Linux / macOS。
2. **依赖 Apple Bonjour**（在 Windows 上额外安装）—— 而纯 Go/Rust 的 mDNS 库可以避免这个依赖。
3. **需要手动配置 YAML + 手动生成证书** —— 对家庭用户仍偏重。
4. **AGPL-3.0** —— 传染性极强，**不可作为本项目代码基础**（只能阅读借鉴）。
5. README 自述缺少色彩管理、自定义纸张、用户认证、审计日志。

**`salmanasmat/Windows-AirPrint-Bridge`** 描述为 "A standalone Python bridge and **Windows Service** that makes any local Windows printer natively Apple AirPrint and IPP Everywhere compatible via mDNS."（13 stars，GPL-3.0，topics 含 `windows-service`、`zeroconf`、`mdns`、`ipp-protocol`）。它的价值是**展示了 "Python + Windows Service" 的打包形态**，可作为 Windows 服务化（见 C 节）的参考。创建于 2026-08-10，非常新，成熟度低。

**`sapireli/AirPrint_Bridge`** 有 648 stars 但语言是 **Shell**——说明它是**脚本包装 CUPS**，而非独立协议实现。它的价值在于证明**"AirPrint 桥接"这个需求有真实且可观的用户量**（648 stars 远超任何纯 IPP 实现）。**[未验证]** 其星数与语言标签存在张力，可能大量 star 来自 macOS 用户对 GUI 安装器的期待。

---

### A.6 `usbip` —— USB over IP（另一条技术路线）

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 |
|---|---|---|---|---|---|
| [dorssel/usbipd-win](https://github.com/dorssel/usbipd-win) | C# | GPL-3.0 | **6177** | 8,649 KB | 2026-09-25 |
| [jiegec/usbip](https://github.com/jiegec/usbip) | Rust | MIT | 536 | 152 KB | 2026-09-03 |
| [rpasek/usbip-wsl2-instructions](https://github.com/rpasek/usbip-wsl2-instructions) | (null) | (none) | 362 | 9 KB | 2021-10-05 |
| [yunsmall/usbipdcpp](https://github.com/yunsmall/usbipdcpp) | C++ | LGPL-3.0 | 134 | 2,526 KB | 2026-09-24 |
| [cgutman/USBIPServerForAndroid](https://github.com/cgutman/USBIPServerForAndroid) | Java | GPL-3.0 | 120 | 1,061 KB | 2024-07-28 |
| [george-hopkins/usbip-windows](https://github.com/george-hopkins/usbip-windows) | C | (none) | 138 | 513 KB | 2020-03-31 |

`usbipd-win` 有 **6177 stars**，是高价值的参考——它证明"在 Windows 上把 USB 设备共享出去"是可行的且有巨大需求，并且它采用了 **C# + Windows 服务 (`usbipd`)** 的形态。

**但 usbip 路线对本项目不合适**，原因：
1. **usbip 传输的是裸 USB 流量**，客户端需要安装内核驱动（Linux `vhci-hcd`）或 Windows 驱动，**这与"客户端零安装"的目标背道而驰**。
2. 它共享的是**整个 USB 设备**，不是"打印机"这一语义对象，无法做打印队列、纸张、双面等 Print Ticket 语义。
3. `usbipd-win` 是 **GPL-3.0**，同样不可作为代码基础。

**结论：usbip 应作为对照路线记录，但不推荐采用。** IPP-over-USB（ipp-usb 路线）才是正确的抽象层次。

---

### A.7 `CUPS-PDF` 与虚拟打印机

| 仓库 | 语言 | License | Stars | 最后推送 |
|---|---|---|---|---|
| [alexivkin/CUPS-PDF-to-PDF](https://github.com/alexivkin/CUPS-PDF-to-PDF) | C | GPL-2.0 | 39 | 2021-12-22 |
| [ManuelKlaer/docker-cups-canon](https://github.com/ManuelKlaer/docker-cups-canon) | Dockerfile | MIT | 15 | 2025-02-18 |
| [aadl/docker-cups-alpine](https://github.com/aadl/docker-cups-alpine) | Shell | (none) | 7 | 2018-12-18 |
| [superna9999/urftopdf](https://github.com/superna9999/urftopdf) | C++ | (none) | 18 | 2017-08-30 |
| [benwiggy/pswrite](https://github.com/benwiggy/pswrite) | Shell | (none) | 6 | 2023-05-12 |

注意：**官方 CUPS-PDF 并没有一个权威的 GitHub 主仓库**。搜索 `cups-pdf` 返回 151 个结果，最高的 `cupslab/password_meter`（188 stars）**完全无关**（CMU 的密码强度计）。`alexivkin/CUPS-PDF-to-PDF` 是"CUPS-PDF with a patch to print text correctly"的 fork。

**借鉴价值：** CUPS-PDF 的本质是一个 **CUPS backend**（可执行文件），把打印数据写进文件系统。这证明了 **"backend 即可执行文件"这一扩展机制极其简洁**——一个后端就是一个遵循约定（argv + stdin/stdout + 环境变量）的进程。这个模型直接对应本项目的插件机制（见 D 节）。

`urftopdf`（CUPS UNIRAST → PDF 过滤器）和 `docker-cups-alpine`（"Alpine docker image with cups and cups-pdf"）也说明：**在容器里跑 CUPS 是常见做法，但 USB 直通是痛点**（见 C 节）。

---

### A.8 Raspberry Pi 打印服务器 / 自托管应用

**关于 `OctoPrint`：** OctoPrint 是 **3D 打印机**控制软件，**不是**文档打印机共享方案。它常被误列入"打印服务器"清单。**本项目不应把它当作竞品或先例**——它的领域是 G-code/串口控制，与 IPP/PDF 打印栈毫无重叠。**[未验证]** 我没有为 OctoPrint 拉取仓库指标，因为经判断它与本项目领域无关。

**关于 `paperless-ngx`：** 任务描述明确要求跳过（"is unrelated — skip"）。**确认其确实无关**——它是文档管理系统（DMS），处理的是已扫描文档的归档与 OCR，不涉及打印共享。

**真实存在的市场空缺：** 在 1319 个匹配 `printer server` 的仓库中，排名靠前的是 `OctoFarm`（359 stars，3D 打印农场管理，AGPL-3.0，最后推送 2023-05-19——**已实质停更**）等无关项目。**没有一个高星、跨平台、现代化的"局域网文档打印机共享"项目存在。** 这正是本项目的立足点。

---

### A.9 IPP 服务器实现（Go / Rust）

**这是本次调研最关键的技术发现：多语言 IPP 生态中，几乎没有成熟的"服务端"实现，只有客户端库。**

#### Go 侧

| 仓库 | License | Stars | 仓库体积 | 最后推送 | 性质 |
|---|---|---|---|---|---|
| [OpenPrinting/ipp-usb](https://github.com/OpenPrinting/ipp-usb) | BSD-2-Clause | 210 | 562 KB | 2026-08-16 | **代理**（非完整 IPP 服务端） |
| [phin1x/go-ipp](https://github.com/phin1x/go-ipp) | **Apache-2.0** | **172** | 136 KB | 2026-08-09 | **纯 Go IPP 库（客户端）** |
| [OpenPrinting/go-mfp](https://github.com/OpenPrinting/go-mfp) | **BSD-2-Clause** | 23 | 25,528 KB | 2026-09-27 | **Go 的多功能打印机/扫描仪库** |
| [michaelgrenfellbrown/GoPrintServer](https://github.com/michaelgrenfellbrown/GoPrintServer) | (none) | 0 | 6,424 KB | 2025-01-21 | "IPP Printer written in Golang"，玩具级 |
| [ghawley365/printcap](https://github.com/ghawley365/printcap) | (none) | 1 | 7,712 KB | 2026-06-19 | 大型多功能抓包/代理，非库 |

**`phin1x/go-ipp`** 描述为 "**Pure Go IPP library**"，Apache-2.0，172 stars，仅 **1 个 open issue**，最后推送 2026-08-09。**这是 Go 生态中最成熟、最活跃、许可证最友好的 IPP 库。** 但需注意：从命名与主题（topics 含 `cups`）看它是**客户端导向**的。**[未验证]** 我未确认它是否提供了完整的服务端（server-side）请求解析与响应构造能力——**这是本项目启动前必须验证的第一个技术问题**。

**`OpenPrinting/go-mfp`** 是重要发现：描述为 "**MFP: Go libraries and tools for Multi-Function Printers and scanners**"，**BSD-2-Clause**（对商业友好），由 OpenPrinting 官方维护，最后推送 2026-09-27（非常活跃）。它很可能把 ipp-usb 中积累的 IPP/eSCL/USB 知识**提炼成了可复用的库**。仓库体积 25 MB 说明内容量不小。**这应该是本项目的首要依赖候选。**

**Go 生态中，服务端 IPP 实现基本是空白**：搜索 `ipp server in:name,description language:Go` 只返回 **2 个结果**（`ghawley365/printcap` 1 star、`michaelgrenfellbrown/GoPrintServer` 0 star）。这是一个**明确的生态空缺**。

#### Rust 侧

| 仓库 | License | Stars | 仓库体积 | 最后推送 | 性质 |
|---|---|---|---|---|---|
| [ArcticLampyrid/ipp-sharing](https://github.com/ArcticLampyrid/ipp-sharing) | **AGPL-3.0** | 16 | 5,569 KB | 2026-06-03 | **Windows IPP 共享服务端（最直接竞品）** |
| [ancwrd1/ipp.rs](https://github.com/ancwrd1/ipp.rs) | **(none)** | **71** | 7,577 KB | 2026-09-02 | **"IPP protocol implementation for Rust"** |
| [heeen/ipp-printer-app](https://github.com/heeen/ipp-printer-app) | MIT | 2 | 220 KB | 2026-09-15 | "Pure-Rust IPP Everywhere framework for building CUPS-driverless printer applications" |
| [mryll/printbar](https://github.com/mryll/printbar) | MIT | 6 | 16,727 KB | 2026-09-21 | Waybar 打印机状态插件（客户端） |
| [SamiKalliomaki/ipp-duplexd](https://github.com/SamiKalliomaki/ipp-duplexd) | MIT | 0 | 41 KB | 2026-08-13 | 极小 |
| [CalvoM/baz_printing](https://github.com/CalvoM/baz_printing) | (none) | 0 | 791 KB | 2026-07-16 | LPD (RFC 1179) + IPP (RFC 8011) 实现 |
| [flll/fake-printer](https://github.com/flll/fake-printer) | MIT | 0 | 223 KB | 2026-08-09 | Windows 虚拟 IPP/AirPrint 打印机 |
| [TheNarcode/printfd](https://github.com/TheNarcode/printfd) | (none) | 0 | 920 KB | 2026-08-11 | 桌面客户端 |

**`ancwrd1/ipp.rs`** 是 Rust 侧最成熟的 IPP 库（71 stars，7,577 KB 仓库体积，最后推送 2026-09-02）。**但它的 `license` 字段为 `null`——即仓库中没有可识别的 LICENSE 文件。** 这是一个**重大法律风险**：无许可证的代码默认保留所有权利，**不可用于本项目**，除非作者明确授权。

**`heeen/ipp-printer-app`** 提供了最有希望的定位："**Pure-Rust IPP Everywhere framework for building CUPS-driverless printer applications**"，MIT 许可。但只有 **2 stars**，创建于 2026-05-25，**极度不成熟**。

**Rust 生态的关键结论**：存在 `ipp-sharing`（AGPL）与 `ipp-printer-app`（MIT 但 2 stars）两个服务端方向的尝试，**但没有一个既成熟又许可证友好**。

#### Python 侧

| 仓库 | 语言 | License | Stars | 最后推送 | 性质 |
|---|---|---|---|---|---|
| [williamkapke/ipp](https://github.com/williamkapke/ipp) | JavaScript | (none) | **425** | 2022-08-17 | Node.js IPP，**已 4 年未更新** |
| [watson/ipp-encoder](https://github.com/watson/ipp-encoder) | JavaScript | MIT | 63 | 2016-01-19 | IPP 编解码器，**已 10 年未更新** |
| [ctalkington/python-ipp](https://github.com/ctalkington/python-ipp) | Python | — | — | — | "**Asynchronous Python client** for IPP"（**客户端**） |
| [bontchev/ipphoney](https://github.com/bontchev/ipphoney) | Python | GPL-3.0 | 5 | 2020-08-21 | **IPP 蜜罐** |

**关于 `pyipp`**：任务描述提到的 `pyipp` 对应的是 `ctalkington/python-ipp`，其描述明确为 "**Asynchronous Python client** for Internet Printing Protocol (IPP)"。**它是客户端库，不是服务端。**

**Python 侧的服务端 IPP 实现几乎不存在**，唯一相关的是 `ipphoney`——一个**蜜罐**（honeypot，GPL-3.0，5 stars，2020 年停更）。蜜罐的原理是**接收并解析 IPP 请求**，因此它的代码结构对"如何解析 IPP 请求"有参考价值，但**它是一个安全研究工具，不是生产级服务器**。

#### 其他语言（供参考）

| 仓库 | 语言 | License | Stars | 最后推送 |
|---|---|---|---|---|
| [gmuth/ipp-client-kotlin](https://github.com/gmuth/ipp-client-kotlin) | Kotlin | MIT | 104 | 2026-09-23 |
| [johnvandeweghe/PIPP](https://github.com/johnvandeweghe/PIPP) | PHP | (none) | 5 | 2019-04-20 |
| [123inkt/ipp](https://github.com/123inkt/ipp) | PHP | MIT | 4 | 2026-09-09 |

**关键结论：无论选哪种语言，RFC 8010 的 IPP 二进制编码（wire format）都需要相当程度的自行实现。** 最成熟的参考是 C 语言的 `istopwg/ippsample` 与 `libcups`。

#### 测试工具

- **`ipptool`**：CUPS 官方提供的 IPP 测试工具，在 [CUPS 3.x 文档列表](https://openprinting.github.io/cups/cups3.html) 中有独立 man page（`ipptool(1)` 和 `ipptoolfile(5)`）。**这是验证本项目 IPP 服务端合规性的必备工具。**
- **`ippfind`**：查找 IPP 打印机（同页 `ippfind(1)`）。
- **`ipptransform`**：`ipptransform(1)` —— "Convert Document Data to Alternate Formats"，**这是做格式转换插件的现成参考**。
- **`ippeveprinter`**：`ippeveprinter(1)` —— "An IPP Everywhere Printer Application for CUPS"，**是本项目最接近的官方参考实现**。

---

### A.10 mDNS / Zeroconf 库生态（**选型关键**）

| 仓库 | 语言 | License | Stars | 仓库体积 | 最后推送 | 维护状态 |
|---|---|---|---|---|---|---|
| [hashicorp/mdns](https://github.com/hashicorp/mdns) | Go | **MIT** | **1373** | 118 KB | **2026-09-14** | **活跃**（HashiCorp 官方） |
| [grandcat/zeroconf](https://github.com/grandcat/zeroconf) | Go | **NOASSERTION** | **905** | 108 KB | **2023-12-07** | **已停更约 2 年** |
| [jstasiak/python-zeroconf](https://github.com/jstasiak/python-zeroconf) | Python | — | — | — | — | **[未验证]**（查询超时） |
| [keepsimple1/mdns-sd](https://github.com/keepsimple1/mdns-sd) | Rust | **Apache-2.0** | 224 | 810 KB | **2026-09-21** | **活跃** |

**这是本次调研中信息量最大的选型发现之一：**

**`grandcat/zeroconf`（905 stars）看起来是 Go 生态的默认选择，但它的最后一次推送是 2023-12-07——已近 2 年无提交，且 license 为 `NOASSERTION`（非标准）。** 依赖它是一个**长期风险**。

**`hashicorp/mdns` 反而是更好的选择**：**1373 stars**（比 grandcat 更多）、**MIT 许可**（清晰友好）、最后推送 **2026-09-14**（活跃），且由 HashiCorp 维护并用于其生产产品（因此长期维护概率高）。仓库仅 118 KB，非常轻量。

**但要注意一个关键差异**：`hashicorp/mdns` 的自我描述是 "Simple mDNS **client/server** library"——它更偏向**服务发现（发现别人）**。而本项目需要的是**服务通告（advertise 自己，让别人发现自己）**。`grandcat/zeroconf` 的描述明确包含 "**mDNS / DNS-SD Service Discovery** in pure Go (also known as Bonjour)"，且 topics 含 `avahi`、`bonjour`、`dns-sd`——它更偏向 DNS-SD 的完整语义。**[未验证]** 我没有逐行核对 `hashicorp/mdns` 是否支持完整的 DNS-SD 服务注册（`_ipp._tcp` TXT 记录、SRV/PTR 发布）。**这是选型前必须验证的第二个技术问题。**

**Rust 侧 `keepsimple1/mdns-sd` 状况良好**：Apache-2.0、224 stars、最后推送 2026-09-21、仓库 810 KB、17 个 open issues。它是 Rust 生态中维护最好的 mDNS/DNS-SD 库。

**关于 `jdx/mDNS`**：任务描述中提到的这个路径**未能验证**。我的查询 `api.github.com/repos/jstasiak/python-zeroconf` **超时**，因此 Python 侧 mDNS 生态**整体标注为 [未验证]**。

**关于 `python-zeroconf`**：它是 Python 生态事实上的标准（被 Home Assistant 等广泛使用），但**[未验证]**——我无法确认其 star 数、license 与最后更新时间，因此**不应在正式决策中引用具体数字**。

---

## B. 服务端技术栈对比

### B.1 评估维度一：IPP 线协议支持

这是**最重要的维度**，因为 IPP 使用 RFC 8010 定义的**二进制编码**（不是 JSON/文本），必须逐字节处理 tag、长度前缀、值类型（integer / boolean / enum / string / dateTime / resolution / rangeOfInteger / collection 等）。

| 语言 | 成熟 IPP 编解码库 | 现状评估 |
|---|---|---|
| **Go** | `phin1x/go-ipp`（Apache-2.0, 172★, 活跃）、`OpenPrinting/go-mfp`（BSD-2-Clause, 活跃） | **中等偏可用**。有 2 个许可证友好的候选，但需验证服务端能力。`[未验证]` go-ipp 的服务端支持 |
| **Rust** | `ancwrd1/ipp.rs`（**无 LICENSE**）、`heeen/ipp-printer-app`（MIT, **2★**） | **差**。唯一成熟库无许可证；MIT 的替代品极不成熟 |
| **Python** | 无服务端库（`python-ipp` 是客户端；`ipphoney` 是蜜罐） | **差**。必须大量手写 |
| **C/C++** | `libcups` / `istopwg/ippsample`（Apache-2.0，官方） | **最好**。有官方参考实现可直接对拍 |

**在 Go 中，IPP 需要多少手写？** 即使 `phin1x/go-ipp` 提供了编解码，服务端仍需自行实现：
- 请求路由（`/`, `/ipp/print`, `/printers/<name>`）
- 操作分发（Get-Printer-Attributes、Print-Job、Validate-Job、Get-Jobs、Cancel-Job、Get-Job-Attributes、CUPS-Get-Printers 等）
- **响应属性构造**——这是最繁琐的部分：`printer-attributes-tag` 必须返回大量必需属性（`printer-name`、`printer-state`、`printer-state-reasons`、`printer-is-accepting-jobs`、`printer-uri-supported`、`uri-security-supported`、`uri-authentication-supported`、`operations-supported`、`document-format-supported`、`media-supported`、`media-default`、`sides-supported`、`print-color-mode-supported`、`printer-resolution-supported`、`ipp-versions-supported`、`charset-configured`、`natural-language-configured` 等数十项）
- **Chunked transfer encoding** 处理（IPP 文档数据常用 chunked）

**IPP Everywhere 的"必需属性"集合是硬性门槛**：macOS/iOS 客户端（AirPrint）对缺失属性非常挑剔——这正是 AirSane README 中 Apple Image Capture "error 21345" 类问题的根源。**CUPS 的 `ipptool` + `get-printer-attributes-suite.test` 是验证这一点的标准手段**（注意 libcups v3.0.3 的 release notes 中明确提到 "Fixed the `ipptool` 'get-printer-attributes-suite.test' test file"，说明这份测试套件是活的基准）。

---

### B.2 评估维度二：mDNS / DNS-SD 库成熟度

| 语言 | 最佳候选 | Stars | License | 最后推送 | 评估 |
|---|---|---|---|---|---|
| **Go** | `hashicorp/mdns` | 1373 | MIT | 2026-09-14 | **活跃、MIT、轻量**；`[未验证]` DNS-SD 服务通告能力 |
| **Go**（备选） | `grandcat/zeroconf` | 905 | NOASSERTION | **2023-12-07** | **停更 2 年**，长期风险 |
| **Rust** | `keepsimple1/mdns-sd` | 224 | Apache-2.0 | 2026-09-21 | **良好** |
| **Python** | `python-zeroconf` | **[未验证]** | **[未验证]** | **[未验证]** | 生态标准但本次未能验证 |
| **C/C++** | Avahi（`libavahi-client`） | N/A | LGPL | — | 成熟但**需运行 avahi-daemon 守护进程** |

**关键洞察（来自 ipp-usb 的实战教训）**：无论选哪个库，都要面对 **Avahi 0.8.0 之前不支持向 localhost 广播 DNS-SD** 这个坑。ipp-usb 的应对是提供**三个选项**：升级 Avahi、自行打补丁、或配置为监听所有接口（`interface = all`）。**纯 Go/Rust 的 mDNS 库（自己实现收发）天然绕过了这个依赖，这是相对于 ipp-usb 的一个真实优势。**

**同样重要的是**：ipp-usb 依赖 Avahi **守护进程**，`AirSane` 也要求 `apt-get install avahi-daemon`。**如果本项目用纯用户态 mDNS 库，就能去掉这个进程级依赖**——直接服务于"小体积、少依赖"的价值观。

---

### B.3 评估维度三：跨平台二进制

| 语言 | Windows 原生 | Linux | macOS | 可行性判断 |
|---|---|---|---|---|
| **Go** | **优秀**（`GOOS=windows`，纯静态，无需运行时） | **优秀** | **优秀** | **唯一真正三平台一等公民** |
| **Rust** | **优秀**（MSVC/GNU toolchain） | **优秀** | **优秀** | **同样优秀** |
| **Python** | **差**（需打包 PyInstaller；Windows Service 支持需 pywin32） | 需系统 Python 或打包 | 需打包 | **分发痛点** |
| **C/C++** | **中**（需 MSVC/MinGW，依赖 libusb/avahi 的跨平台移植） | **优秀** | **中**（macOS 上是 CUPS 的主场，但 Avahi 不可用，需换 Bonjour） | **构建矩阵复杂** |
| **薄控制面 + shell out 到 CUPS** | **差** | **优秀** | **中** | **Windows 上 CUPS 不存在，直接违背跨平台目标** |

**"薄控制面 + shell out 到 CUPS"这条路线需要明确否决**：CUPS 在 Windows 上**根本不存在**（CUPS 是 Apple/Unix 的产品）。这直接违背了"Windows/Linux/macOS 三端客户端都能打印"的项目目标——因为**服务端必须能跑在 Windows 上**（用户很可能就是在 Windows 机器上插着 USB 打印机）。**除非**该方案只作为 Linux/macOS 的"驱动适配器插件"存在，而核心是自研的 IPP 栈。这是唯一合理的用法。

**注意 ipp-usb 的诚实自述**："Being written on Go, ipp-usb has a large executable size."——这说明 **Go + cgo 的二进制并不小**。但纯 Go（不含 cgo）的静态二进制在 5–15 MB 量级，仍远小于 Python 打包产物。**用纯 Go 的 USB 库（如 `google/gousb`）或纯 Go mDNS 可以避免 cgo，从而得到真正的静态单文件二进制。**

---

### B.4 评估维度四：体积（Footprint）

**已验证的事实：**
- ipp-usb（Go + cgo）：README 自述"**large executable size**"，但**内存占用不高**——单设备时 RSS 与 ippusbxd 相当或略低；**多设备时因单进程处理而明显优于 ippusbxd**。
- AirSane（C++）：仓库仅 **503 KB**，README 称"图像即时编码，使内存/存储需求保持低位，**因此能在 Raspberry Pi 或类似设备上运行良好**"。
- `ippsample`（C）：仓库 14 MB（含历史）。
- **容器镜像**：ipp-usb 提供了官方 Docker/Rock 镜像 `ghcr.io/openprinting/ipp-usb:latest`。**[未验证]** 其镜像具体大小我未能查询（GitHub Container Registry 的 API 支持需要额外认证）。

**推理（标注为推理，非已验证事实）**：
- 纯 Go 静态二进制（无 cgo）：通常 5–15 MB
- Rust release 二进制（strip 后）：通常 2–8 MB
- Python + PyInstaller：通常 30–80 MB
- Alpine + CUPS 容器：通常 100–300 MB
- 完整 CUPS 安装：连同 cups-filters 与 PPD 数据库，通常数百 MB

**注意**：这些数字是**基于语言生态常识的估计，不是本次验证的数据**。项目启动后应实测。

---

### B.5 评估维度五：维护成本、社区、长期风险

| 语言 | 招聘/社区 | 长期风险 | 本项目特有风险 |
|---|---|---|---|
| **Go** | **极强**（云原生主流） | **低** | 服务端 IPP 生态空缺，需自建 |
| **Rust** | **强且增长** | **低** | 唯一成熟 IPP 库**无许可证**；替代品 2★ |
| **Python** | **极强** | **低** | 分发与性能是硬伤；无服务端 IPP 库 |
| **C/C++** | 中（系统编程人群） | **中**（人力萎缩） | 内存安全风险高；但可复用 libcups |
| **薄控制面 + CUPS** | 强 | **高**（受制于 CUPS 3.x 的激进重构） | Windows 缺席 |

**一个被低估的长期风险**：CUPS 3.x 正在**激进重构**——`cupsd` 被拆成 `cups-local` + `cups-sharing`，而 `cups-sharing` 最后推送是 **2024-01-12**（已两年）。**如果本项目选择"薄控制面 + CUPS"，就等于把自己的命运绑在一个正在剧烈变动且部分组件停滞的上游上。** 反之，**如果本项目自己做 IPP 栈，则 CUPS 3.x 的"driverless only"转向反而对我们有利**——因为整个生态正在向"纯 IPP Everywhere"收敛，我们不需要支持古老的 PPD/驱动模型。

---

### B.6 推荐技术栈（含理由）

> **推荐：Go 作为主语言，采用"模块化多包 + 独立小进程"结构；mDNS 用自己的实现或 `hashicorp/mdns`；IPP 编解码基于 `phin1x/go-ipp` 与/或 `OpenPrinting/go-mfp`，不足部分按 RFC 8010/8011 自行补齐。**

**理由 1：Go 是三平台一等公民，且免运行时依赖——这是"小体积、易部署"价值观的直接兑现。**
`GOOS=windows/linux/darwin` 三条交叉编译命令即可产出**无需安装运行时**的静态二进制。Python 需要 PyInstaller（体积膨胀 5–10 倍），C/C++ 需要维护 MSVC/MinGW/Clang + libusb + avahi 的交叉构建矩阵。**本项目的用户是"家里有台打印机、想在局域网共享"的普通人，安装体验是核心产品力。**

**理由 2：Go 生态同时具备"够用的 IPP 库"和"许可证友好、活跃的 mDNS 库"——两者缺一不可，而只有 Go 和（勉强）Rust 同时满足。**
- IPP：`phin1x/go-ipp`（Apache-2.0，172★，活跃）+ `OpenPrinting/go-mfp`（BSD-2-Clause，OpenPrinting 官方，2026-09 活跃）
- mDNS：`hashicorp/mdns`（MIT，1373★，2026-09 活跃）——**比 Go 生态名气更大的 `grandcat/zeroconf` 已停更两年且许可证不明**

对比 Rust：唯一成熟 IPP 库 `ipp.rs` **没有 LICENSE 文件**（法律上不可用），MIT 的替代品仅 2★。**Rust 的 IPP 生态尚未成熟到可以安全押注的程度。**

对比 C/C++：虽然 `libcups` 是标准，但它是 Apache-2.0 的 C 库，跨平台构建复杂（尤其 Windows），且会把项目绑上 CUPS 的重构节奏。

**理由 3：ipp-usb 已经用 Go 证明了这个技术路线可行，包括所有"踩过的坑"，而它的许可证（BSD-2-Clause）允许我们直接借用代码与设计。**
`OpenPrinting/ipp-usb` 是 **Go 语言、BSD-2-Clause**（非传染性，可自由用于开源或闭源）、由 OpenPrinting 官方维护、活跃（2026-08 推送）。它验证了：Go 能跑 USB（libusb）+ mDNS（avahi）+ IPP 代理，并且**它的 README 把 IPP-over-USB 的致命陷阱、HTTP 净化规则、WebSocket 阻断、连接负载均衡、截断文档的处理策略全部写清楚了**。这些是**无法通过读协议文档获得的实战知识**。

同时 `OpenPrinting/go-mfp` 是 **BSD-2-Clause**，与 ipp-usb 同源，**很可能已是 ipp-usb 内部逻辑的库化版本**——这意味着我们可以站在官方维护的肩膀上。

**理由 4（补充）：CUPS 3.x 的架构选择为"自研 IPP 栈"提供了外部合法性。**
CUPS 官方自己把系统拆成了 `libcups` + `cups-commands` + `cups-local` + `cups-sharing` 四个独立仓库，并提供 `ippeveprinter`（IPP Everywhere Printer Application）。**"用 IPP 协议实现一个打印机应用"是被 OpenPrinting 官方认可并鼓励的模式**，而不是"重复造 CUPS 轮子"。

**需要在动工前验证的两个问题：**
1. `phin1x/go-ipp` / `OpenPrinting/go-mfp` 是否提供**服务端**的请求解析与响应构造能力（而非仅客户端）。
2. 选定的 mDNS 库是否支持完整的 **DNS-SD 服务注册**（`_ipp._tcp` / `_ipps._tcp` 的 PTR + SRV + TXT 三件套），因为这是 AirPrint 发现的前提。

---

## C. 部署与运行时选项

### C.1 三种运行时形态对比

| 形态 | 优点 | 缺点 | 适用平台 |
|---|---|---|---|
| **原生二进制** | 无运行时依赖；体积最小；启动最快；**能直接访问 `/dev/bus/usb` 与 IOKit** | 需为每个架构编译；无自动更新 | **全平台（推荐主推）** |
| **Docker / 容器** | 一致的构建产物；易回滚 | **USB 直通需要 `--network host` + `--device-cgroup-rule` + volume**；mDNS 在容器内几乎必须用 host 网络；Windows/macOS 上 Docker 是虚拟机，USB 直通更麻烦 | Linux 服务器/NAS |
| **systemd service** | 与发行版集成；UDEV 触发；日志进 journald | 仅 Linux | Linux |
| **Windows Service** | 开机自启；无需用户登录 | 需要 SCM 集成（Go 可用 `golang.org/x/sys/windows/svc`） | Windows |

**ipp-usb 的实证（已验证）**：它**同时提供全部四种形态**——发行版包（含 UDEV rules + systemd service）、Snap、Docker/Rock（`ghcr.io/...`）、以及源码 `make install`。**这印证了"原生二进制为主 + 平台特定封装为辅"的最佳实践。**

**Docker 的 USB 代价（已验证的 ipp-usb 官方命令）：**
```bash
sudo docker run -d --network host \
    -v /dev/bus/usb:/dev/bus/usb:ro \
    -v ipp-usb-state:/var/ipp-usb \
    -v ipp-usb-conf:/etc/ipp-usb \
    --device-cgroup-rule='c 189:* rmw' \
    --name ipp-usb \
    ghcr.io/openprinting/ipp-usb:latest
```
注意 `--network host` 是**必需的**（否则 mDNS 无法工作），`c 189:*` 是 USB 字符设备的主设备号。**这几个参数说明：容器化对 USB 打印共享而言是"能跑但很别扭"**。README 还特别指出 Snap 无法安装 UDEV rules，只能用 `udevadm monitor` 脚本轮询来模拟触发——**这是封装成本的直接证据**。

### C.2 各平台的安装与后台运行

**Linux：**
- **systemd service**：AirSane 提供了现成的 service 文件，安装流程为 `make && sudo make install && sudo systemctl enable airsaned && sudo systemctl start airsaned`（已验证）。
- **UDEV 触发**：ipp-usb 同时安装 UDEV rules 和 `systemd-udev/` 下的 service（已验证），使得设备插入时自动启动，拔出时自动退出（`ipp-usb udev` 参数模式）。**这对于"只有插上打印机才需要服务"的场景是最优雅的设计。**
- **权限**：AirSane 的 README 揭示了一个真实的坑——`saned` 用户可能看不到扫描仪，需要 `/etc/udev/rules.d/65-libsane.rules` 里加 `ENV{libsane_matched}=="yes", RUN+="/usr/bin/setfacl -m g:scanner:rw $env{DEVNAME}"`，并指出这是 [libsane 的一个 Debian bug](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=918358)。**同类问题在打印机上也会出现**，需要组权限（`lp` 组）或 udev 规则。

**Windows：**
- **Windows Service**：需要实现 SCM 接口（Go 用 `golang.org/x/sys/windows/svc`；Python 用 `pywin32`）。`salmanasmat/Windows-AirPrint-Bridge` 的描述明确声称是 "a standalone Python bridge and **Windows Service**"（已验证其描述，**未验证其实现质量**）。`ArcticLampyrid/ipp-sharing` 则是**直接运行二进制**（README 的 Step 3 只说 "Execute the binary"），**未提供服务化方案**——这是一个可以超越竞品的点。
- **防火墙**：`ipp-sharing` README 明确要求放行 **TCP 631**（IPP）与 **UDP 5353**（DNS-SD）。**这是必须处理的安装步骤。**
- **依赖**：`ipp-sharing` 要求安装 **Apple Bonjour**（"Install Bonjour for service discovery. It is bundled with iTunes or Bonjour Print Services"）。**这正是纯 Go mDNS 库能消除的依赖。**

**macOS：**
- 可用 **launchd**（`~/Library/LaunchAgents/*.plist` 或 `/Library/LaunchDaemons/*.plist`）。
- 注意 **macOS 自带 CUPS**，因此 macOS 上"共享打印机"已有原生路径（系统设置 → 打印机共享）。**本项目的 macOS 价值主要在于跨平台一致性，而非填补功能空白。**
- **[未验证]** 我没有找到权威的 macOS 上运行第三方 IPP 共享服务的 launchd 范例。

### C.3 USB 打印机的"最小依赖"故事（**这是全篇最需要实证的部分**）

| 平台 | 可达的 USB 接口 | 评估 |
|---|---|---|
| **Linux** | **`/dev/usb/lp0`**（`usblp` 内核驱动，字符设备）与 **`/dev/bus/usb/BBB/DDD`**（`libusb` / usbfs 路径） | **两种都可用**。`/dev/usb/lp0` 是最简单路径（直接 `open()` + `write()` 原始数据），但**只适用于"哑打印机"**（直接接收 PCL/PostScript/ESC-POS 的机型）。现代打印机走 IPP-over-USB 时需要 `/dev/bus/usb/*` + libusb。 |
| **Windows** | **`usbprint.sys`**（USB 打印机类驱动，暴露为打印队列）与 **WinUSB**（`winusb.sys`，通用 USB 访问） | **`usbprint.sys` 是默认绑定**——这意味着**打印机被 Windows 打印子系统占用，第三方程序不能直接 `WriteFile` 到端点**。要走 IPP-over-USB，需要**解绑 usbprint 并绑定 WinUSB**（通过 INF 文件或 `Zadig` 类工具），**这是侵入性操作，且会破坏 Windows 原生打印**。**[未验证]** 我未验证 Windows 上是否存在无侵入的 IPP-over-USB 访问路径。 |
| **macOS** | **IOKit**（`IOUSBDevice` / `IOUSBInterface`，用户态可访问）与 **`/dev/usb/...`（不标准）** | **IOKit 是用户态可访问的**（通过 `IOUSBInterfaceInterface`）。但 **macOS 的打印栈（CUPS + `usb` backend）在打开设备时会申请独占（exclusive）访问**。**[未验证]** 我未验证 macOS 上第三方进程与 CUPS 争抢同一 USB 打印机时的实际行为。 |

**关键工程结论（基于已验证证据的推理）：**

1. **`ipp-usb` 走的是 IPP-over-USB 标准（USB 接口类 7/1/4）**，README 明确提到 `ipp-usb check` 会"scan the USB for the presence of potentially supported USB devices (**7/1/4 interface protocol**)"，并且识别方式基于 **VendorID + ProductID + 序列号**三元组。**这意味着 ipp-usb 只支持"本身就是 IPP 打印机"的现代设备**——它做的是**协议代理，不是驱动**。

2. **这是本项目必须做出的最根本的产品决策**：
   - **路线 A（ipp-usb 式）**：只支持 IPP-over-USB 设备（7/1/4 接口）。**用户在客户端零安装、零驱动**，服务端只需做 HTTP 反向代理 + mDNS。**但不能支持老式"哑打印机"。**
   - **路线 B（CUPS 式）**：支持任意 USB 打印机，但需要**驱动/PPD/filter 栈**——这就是 cups-filters 那 246 MB 的由来。**违背"小体积"价值观。**

   **强烈建议：先做路线 A。** 理由：现代打印机（近十年内）绝大多数支持 IPP-over-USB 或至少 eSCL；路线 A 的实现量是路线 B 的百分之一；而"拒绝单体"的价值观要求**通过插件机制（见 D 节）在后期才引入路线 B**，而不是一开始就背负它。

3. **Windows 上的原生共享反而是优势**：`ipp-sharing` 的做法是**不碰 USB**——它把 `target` 设为一个**已存在的 Windows 打印队列名**（例如 `"Microsoft Print to PDF"`），然后把 IPP 请求转换后**提交给 Windows 打印后台处理程序（spooler）**。**这完全绕开了 `usbprint.sys` 的独占问题。** 这是 Windows 侧最务实的架构，**强烈建议借鉴**。

---

## D. "拒绝单体"的设计模式

### D.1 建议的模块分解

基于对 ipp-usb、AirSane、CUPS 三者的分析，建议的分解（每个是一个**独立的 Go package**，其中若干可选地成为**独立进程**）：

```
┌─────────────────────────────────────────────────────────────┐
│                        cmd/ (可执行入口)                      │
│  printd        printctl         print-plugin-*              │
│  (daemon)      (CLI/API)        (独立插件进程)                │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│  pkg/ (纯库，无全局状态，可独立测试)                            │
│                                                             │
│  transport/    HTTP/HTTPS 服务器、连接管理、chunked 处理       │
│  discovery/    mDNS/DNS-SD 通告 + 浏览（_ipp._tcp/_ipps._tcp） │
│  ipp/          RFC 8010 编解码 + RFC 8011 操作语义            │
│  spooler/      作业队列、状态机、持久化                        │
│  driver/       后端适配器接口 + 各实现（在本项目里=插件契约）    │
│  config/       配置加载（分层：全局/per-device/ignore/access） │
│  attributes/   IPP Everywhere 必需属性表的构造与校验           │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
  driver/ippusb        driver/winspl          driver/rawlp
  (USB 7/1/4 → HTTP)   (Windows spooler)      (/dev/usb/lp0 直写)
```

**这个分解直接对应 ipp-usb 已验证的职责划分**（README 的 "Under the hood" 一节几乎逐条对应）：
- HTTP 净化 / 1.0→1.1 升级 / WebSocket 阻断 → `transport/`
- DNS-SD 通告 + 名称冲突解决 + 端口持久化 → `discovery/`
- `get-printer-attributes` 查询构造与解析 → `ipp/` + `attributes/`
- 客户端连接与设备连接解耦、USB 连接负载均衡 → `driver/ippusb` + `spooler/`
- 多级配置与 per-device 覆盖 → `config/`（**照抄 AirSane 的 `/etc/airsane/options.conf` 正则匹配模式**）

### D.2 三种解耦方式的取舍

| 方式 | 隔离性 | 性能 | 部署复杂度 | 适用场景 |
|---|---|---|---|---|
| **同进程内模块（Go package）** | 低（共享地址空间） | **最高**（函数调用） | **最低** | **核心路径：transport / ipp / discovery** |
| **独立小进程 + 本地 socket/HTTP** | **高**（崩溃不互相影响） | 中（IPC 开销） | 中 | **驱动适配器：一个打印机后端一个进程** |
| **插件（外部可执行文件 + 约定接口）** | **最高**（可任意语言） | 中 | 中 | **第三方扩展点（长期演进的关键）** |

**推荐组合：核心同进程 + 驱动层可独立进程 + 对外暴露插件接口。**

**理由：** ipp-usb 是一个**单进程处理所有设备**的守护进程（README 明确："because ipp-usb handles all devices in a single process, it uses noticeably less memory"）。这证明**核心路径同进程是正确的**——拆成微服务会得不偿失。但**驱动/后端层是变化最剧烈的部分**（不同打印机型号、不同操作系统、不同年代），把它隔离成进程或插件，才能实现"新增打印机支持不触碰核心代码"。

### D.3 CUPS 的 backend/filter 模型（**最值得抄的扩展机制**）

CUPS 的扩展模型极其简洁，且已由 `CUPS-PDF`、`urftopdf`、`pswrite` 等大量第三方项目验证（见 A.7）：

- **backend = 一个可执行文件**，位于 `/usr/lib/cups/backend/`，遵循约定：
  - 从 **argv** 接收：`job-id user title copies options [file]`
  - 从 **stdin** 接收文档数据（或从 `file` 参数指定的文件）
  - 从 **环境变量** 接收作业元数据（`DEVICE_URI`、`CUPS_SERVERBIN`、`PRINTER`、`FINAL_CONTENT_TYPE` 等）
  - 向 **stderr** 输出状态消息（`STATE:`、`ATTR:`、`PAGE:`、`INFO:` 等，用于回传打印机状态与页计数）
  - **以设备 URI scheme 命名**（如 `socket`、`usb`、`ipp`、`lpd`），CUPS 据此发现后端
- **filter = 一个可执行文件**，位于 `/usr/lib/cups/filter/`，从 stdin 读、stdout 写，做格式转换链（如 PDF → raster → 设备语言）。

**为什么这个模型对本项目是完美的模板：**
1. **零语言绑定**——后端可以用任何语言写，只要能执行。这保证了**长期演进**（未来想用 Zig/OCaml/Julia 写一个后端，核心零改动）。
2. **零 ABI 稳定性负担**——不需要维护 C ABI 或 Go plugin（Go 的 `plugin` 包在跨平台支持上很差，且要求完全相同的工具链版本，**不适合本项目**）。
3. **天然的进程隔离**——一个后端崩溃不会拖垮主服务。
4. **可独立测试**——`echo <testdata> | ./mybackend 1 user title 1 opts` 就能验证。
5. **URI scheme 作为发现机制**——配置文件里写 `ippusb://...`，核心就去 `PATH` 或插件目录找同名可执行文件。

**建议的插件接口契约（借鉴 CUPS 但简化为 IPP 语义）：**
```
核心 → 插件（stdin，JSON 行协议 或 简单文本头 + 二进制体）：
  设备枚举请求 / 提交作业(元数据 + 文档流) / 查询状态 / 取消作业
插件 → 核心（stdout/stderr）：
  设备清单(JSON) / 作业状态更新 / 打印机状态(reasons) / 页计数
```
**关键设计约束：插件必须是"可选增强"而非"必需组件"**——没有插件时，核心应该仍能通过内置的 `ippusb` 后端工作。这是"拒绝单体"的底线：**核心不依赖任何单个插件。**

### D.4 `ipp-usb` 与 `airscan` 的责任拆分（**已验证的对照**）

| 关注点 | `ipp-usb` 的做法 | `AirSane` 的做法 | 对本项目的启示 |
|---|---|---|---|
| **协议适配** | HTTP 反向代理，后端 IPP-over-USB | eSCL/Mopria 服务端，后端 SANE | **协议适配必须是最薄的一层** |
| **设备发现** | 自己的 DNS-SD 通告（依赖 Avahi）+ UDEV | mDNS 通告（`MDNS_ANNOUNCE=true`，依赖 avahi-daemon） | 两者都**外包给系统 Avahi**——本项目可超越，用用户态 mDNS |
| **参数来源** | IPP `get-printer-attributes` / eSCL `ScannerCapabilities` | SANE 后端能力查询 | **DNS-SD TXT 记录必须由设备真实能力动态生成，不能硬编码** |
| **端口稳定性** | 按 VendorID+ProductID+序列号分配并**持久化** | `LISTEN_PORT=8090`（固定） | **ipp-usb 的做法更专业**——多设备场景端口稳定是刚需 |
| **名称冲突** | 自动解决并持久化 | [未在 README 中提及] | **ipp-usb 的做法值得抄** |
| **访问控制** | `[auth uid]` 节，按 UID/GID + 操作类型（all/config/fax/print/scan） | `access.conf`，按 IP 段 allow/deny + `local on eth0` | **两者互补**：本地用 UID，网络用 IP |
| **per-device 配置** | quirk 文件（设备兼容性 workaround） | `options.conf` 正则匹配设备名 | **AirSane 的正则匹配更通用；quirk 文件更适合"型号级兼容性补丁"** |
| **避免回环** | — | `ignore.conf`（"original purpose ... is to avoid loops with backends that auto-detect eSCL devices"） | **重要教训**：当共享服务自己也是 mDNS 客户端时，会形成回环，必须有忽略机制 |
| **图像/数据处理** | 流式代理 | **采集时即时编码**，保持内存低位 | **流式处理是"小体积"的关键**——绝不能把整个作业读进内存 |
| **Web 界面** | 有（设备控制台代理） | 有（`WEB_INTERFACE=true`，端口 8090） | 两者都提供，**说明 Web 界面是必需品** |
| **热插拔** | UDEV 触发 + 单实例锁 | `HOTPLUG=true` + `RELOAD_DELAY=1`（防抖） | **AirSane 的防抖延迟是必要的**——USB 枚举事件会连发 |

**结论：`ipp-usb` 强在"协议与设备绑定的严谨性"（端口/名称持久化、HTTP 净化、连接解耦），`AirSane` 强在"配置分层与可运维性"（options/ignore/access 三个文件 + 防抖）。本项目应同时吸收两者。**

---

## E. 结论与建议

1. **产品定位**：做一个**跨平台（Windows/Linux/macOS 服务端）、零客户端安装、基于 IPP Everywhere/AirPrint 的局域网打印机共享工具**。当前市场存在真实空缺——最接近的 `ArcticLampyrid/ipp-sharing` 仅 16 stars、仅 Windows、AGPL-3.0、需手动配置。

2. **技术路线**：**先只支持 IPP-over-USB（USB 接口类 7/1/4）设备**，做 HTTP 反向代理 + 动态 DNS-SD 通告。**不要一开始就做驱动/PPD 栈。**

3. **技术栈**：**Go**。三平台一等公民、免运行时、有许可证友好的 IPP 库（`phin1x/go-ipp` Apache-2.0、`OpenPrinting/go-mfp` BSD-2-Clause）与活跃的 MIT mDNS 库（`hashicorp/mdns` 1373★）。

4. **架构**：核心同进程（`transport` / `ipp` / `discovery` / `spooler` / `attributes` / `config`），**驱动适配层用 CUPS 式"可执行文件插件"契约**（argv + stdin/stdout 或 JSON 行协议），天然支持任意语言、进程隔离、零 ABI 负担。

5. **必须先验证的两个问题**：
   - `phin1x/go-ipp` 或 `OpenPrinting/go-mfp` 是否提供**服务端**的 IPP 请求解析与响应构造能力。
   - 选定的 mDNS 库是否支持完整的 **DNS-SD 服务注册**（`_ipp._tcp` 的 PTR+SRV+TXT）。

6. **最大的技术陷阱（已由 ipp-usb 实证）**：**IPP-over-USB 不能做朴素 TCP 中继**——必须在客户端断开时把整个 HTTP 响应读完，否则残留数据会污染下一次请求。这要求实现真正的 HTTP 反向代理（含请求净化、协议版本升级、WebSocket 阻断）。

---

## 参考来源

本文实际抓取（fetch）过的全部 URL：

**GitHub REST API（仓库元数据，2026 年查询）**
- https://api.github.com/repos/OpenPrinting/cups
- https://api.github.com/repos/OpenPrinting/cups-filters
- https://api.github.com/repos/OpenPrinting/cups-browsed
- https://api.github.com/repos/OpenPrinting/libcups
- https://api.github.com/repos/OpenPrinting/libcups/releases?per_page=3
- https://api.github.com/repos/OpenPrinting/cups-local
- https://api.github.com/repos/OpenPrinting/cups-sharing
- https://api.github.com/repos/OpenPrinting/ipp-usb
- https://api.github.com/repos/OpenPrinting/ipp-usb/releases?per_page=3
- https://api.github.com/repos/OpenPrinting/go-mfp
- https://api.github.com/repos/istopwg/ippsample
- https://api.github.com/repos/alexpevzner/sane-airscan
- https://api.github.com/repos/SimulPiscator/AirSane
- https://api.github.com/repos/SimulPiscator/AirSane/contents/README.md
- https://api.github.com/repos/grandcat/zeroconf
- https://api.github.com/repos/hashicorp/mdns
- https://api.github.com/repos/keepsimple1/mdns-sd
- https://api.github.com/repos/dorssel/usbipd-win
- https://api.github.com/repos/savoirfairelinux/ipp-usb → **HTTP 404（证明该项目不存在）**
- https://api.github.com/repos/airprintbridge/airprint-bridge → **HTTP 404（证明该仓库不存在）**
- https://api.github.com/repos/OpenPrinting/ippsample → **HTTP 404（正确路径是 istopwg/ippsample）**
- https://api.github.com/repos/jstasiak/python-zeroconf → **请求超时，未获取到数据**
- 搜索：`https://api.github.com/search/repositories?q=airprint+bridge&sort=stars&order=desc&per_page=15`
- 搜索：`https://api.github.com/search/repositories?q=airsane&sort=stars&order=desc&per_page=10`
- 搜索：`https://api.github.com/search/repositories?q=ipp+server+in:name,description+language:Go&sort=stars&order=desc&per_page=20`
- 搜索：`https://api.github.com/search/repositories?q=ipp+printer+in:name,description+language:Rust&sort=stars&order=desc&per_page=20`
- 搜索：`https://api.github.com/search/repositories?q=ipp+in:name+language:Go&sort=stars&order=desc&per_page=20`
- 搜索：`https://api.github.com/search/repositories?q=ipp+protocol+in:name,description&sort=stars&order=desc&per_page=25`
- 搜索：`https://api.github.com/search/repositories?q=cups-pdf&sort=stars&order=desc&per_page=8`
- 搜索：`https://api.github.com/search/repositories?q=usbip+in:name&sort=stars&order=desc&per_page=8`
- 搜索：`https://api.github.com/search/repositories?q=printer+server+in:name,description&sort=stars&order=desc&per_page=10`

**项目 README / 配置原文（raw.githubusercontent.com）**
- https://raw.githubusercontent.com/OpenPrinting/ipp-usb/master/README.md
- https://raw.githubusercontent.com/OpenPrinting/ipp-usb/master/ipp-usb.conf
- https://raw.githubusercontent.com/ArcticLampyrid/ipp-sharing/master/README.md

**官方文档页面**
- https://openprinting.github.io/cups/cups3.html （OpenPrinting CUPS v3，含 libcups/cups-local/cups-sharing 仓库链接与 `ippeveprinter`/`ippfind`/`ipptool`/`ipptransform` 文档索引）

**权威参考（未直接抓取，仅由上述来源指认）**
- [Mopria eSCL Specification](https://mopria.org/mopria-escl-specification) （由 AirSane README 指认）
- [IPP-over-USB 1.0（USB-IF）](https://www.usb.org/document-library/ipp-protocol-10) （由 ipp-usb README 指认）
- [IPP Everywhere（PWG）](https://www.pwg.org/ipp/everywhere.html) （由 CUPS v3 页面指认）
- [libsane Debian bug #918358](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=918358) （由 AirSane README 指认）
- [ipp-usb 发行版下载（openSUSE OBS）](https://download.opensuse.org/repositories/home:/pzz/)
- [ipp-usb FreeBSD port](https://www.freshports.org/print/ipp-usb/)
- [ipp-usb Snap](https://snapcraft.io/ipp-usb)

**未能完成 / 标注为 [未验证] 的项**
- `jstasiak/python-zeroconf` 的仓库元数据——API 请求**超时**，故 Python mDNS 生态整体未给出具体数字
- `OpenPrinting/ippsample`（错误路径，**HTTP 404**）——正确路径是 `istopwg/ippsample`
- `airprintbridge/airprint-bridge`（**HTTP 404**）——该组织/仓库不存在
- `savoirfairelinux/ipp-usb`（**HTTP 404**）——该项目不存在
- OctoPrint 的仓库指标——经判断与本项目领域（3D 打印 vs 文档打印）无关，未查询
- `ipp-usb` 的二进制体积与 Docker 镜像大小——README 自述"large executable size"但无具体数字；GHCR 镜像大小需额外认证
- `phin1x/go-ipp` 与 `hashicorp/mdns` 的服务端/服务通告能力——仅凭仓库描述无法确认，需阅读源码
- Windows `usbprint.sys` 与 WinUSB 的解绑方案、macOS IOKit 与 CUPS 的 USB 独占行为——均未找到可验证的一手来源
