# 局域网打印机共享：协议与架构技术调研简报

> 面向目标：构建一个**小而可维护**的打印机共享工具，让 Windows / Linux / macOS 客户端都能打印到接在某台局域网主机上的打印机。
>
> 本文所有事实均标注来源。**凡未能亲自核实的内容，一律显式标注 `⚠️ 未核实`**。本文不臆造 RFC 编号、端口号或 API 细节。

---

## 0. 结论先行（TL;DR）

1. **主线协议只有一条：IPP。** 它是唯一被三大桌面系统（Windows 10 21H2+、macOS、Linux/CUPS）共同原生支持的打印协议。其他协议（LPD、9100、SMB 共享）只应用作兼容性兜底。
2. **最小可行架构 = 一个 IPP Everywhere 服务端 + mDNS/DNS-SD 广播**，接受 PDF 与 PWG Raster 输入，透传到打印机。这样 macOS/iOS 走 AirPrint 路径、Linux 走 CUPS 路径、Windows 走内置 IPP Class Driver 路径，**无需在客户端装任何驱动**。
3. **不要以 SMB 共享 + Point-and-Print 为主路径。** 它是历史上最大的痛点来源（远程驱动安装、提权、SMB 协商），而且微软正在系统性地淘汰第三方打印驱动（见 §5.5）。
4. **许可风险可控**：现代 CUPS 是 Apache-2.0（含 GPLv2 链接例外），可以安全地链接/分发。**Ghostscript 是 AGPL，是唯一的真实法律地雷** —— 只要你的服务端只接受 PDF / PWG Raster / URF / PostScript 输入，就完全不需要 Ghostscript。

---

## 1. IPP（Internet Printing Protocol）

### 1.1 核心规范：RFC 8010 / RFC 8011

| 文档 | 标题 | 关系 |
|---|---|---|
| **RFC 8010** | *Internet Printing Protocol/1.1: Encoding and Transport* | 定义 `application/ipp` MIME 类型的二进制编码，以及如何用 HTTP/HTTPS 承载。**废弃** RFC 2910、3382 |
| **RFC 8011** | *Internet Printing Protocol/1.1: Model and Semantics* | 定义抽象对象模型（Printer / Job / Document）、操作语义、属性。**废弃** RFC 2911、3381、3382 |

两者均为 IETF Standards Track，2017 年 1 月发布。来源：[RFC 8010](https://www.rfc-editor.org/rfc/rfc8010.txt)、[RFC 8011](https://www.rfc-editor.org/rfc/rfc8011.txt)。

**编码要点（RFC 8010 §3.1.1）**——每条 IPP 报文的结构：

```
version-number      2 bytes   (required)   e.g. 0x0101 = 1.1, 0x0200 = 2.0
operation-id        2 bytes   (request)    或 status-code (response)
request-id          4 bytes   (required)
attribute-group     n bytes   (0 or more)
end-of-attributes-tag 1 byte  (required, 值 0x03)
data                q bytes   (optional, 即文档数据)
```

- 整数一律 **二进制补码、大端序**；1 字节用于 version/tag，2 字节用于 operation-id/status-code/length，4 字节用于 value 与 request-id。
- 属性编码：`value-tag(1) + name-length(2) + name + value-length(2) + value`。多值属性的后续值 `name-length` 置 0。
- 集合（collection）属性：`0x34`(begCollection) 开始，`0x37`(endCollection) 结束。

**强制顺序要求（RFC 8011 §4.1.4）**：`attributes-charset` 必须是每个请求/响应属性组中的**第一个**属性，`attributes-natural-language` 必须是**第二个**。所有实现**必须**支持 `utf-8`。

### 1.2 RFC 3998 的真面目（⚠️ 重要更正）

任务书中猜测「IPP Shared Infrastructure Extensions / INFRA — RFC 3998?」。**这个猜测是错误的**，需要纠正：

- **RFC 3998** 实际是 *Internet Printing Protocol (IPP): Job and Printer Administrative Operations*（2005-03），定义 16 个**可选**管理操作（Enable/Disable-Printer、Pause-Printer-After-Current-Job、Hold-New-Jobs、Restart-Printer、Shutdown-Printer 等）以及 subordinate-printers / parent-printers 的 fan-out / fan-in 模型。来源：[RFC 3998](https://www.rfc-editor.org/rfc/rfc3998.txt)。
- **INFRA 实际是 PWG 5100.18**，*IPP Shared Infrastructure Extensions*，最新版 **v1.1 (PWG 5100.18-2025)，2025-05-02 发布**。它定义 Cloud Imaging Model 的 IPP 绑定，让 IPP Printer 反向连接共享服务去**拉取** Job/Document（用于云打印、集中计费、刷卡释放打印）。来源：[PWG Published Standards](https://www.pwg.org/standards.html)、[PWG IPP Everywhere 页面](https://www.pwg.org/ipp/everywhere.html)。

> **对小型局域网工具的取舍**：INFRA 面向「打印机主动出站连云端」，与「局域网内一台主机共享本地打印机」的场景**方向和复杂度都不对口**，本工具**不需要实现 INFRA**。RFC 3998 的操作也全部 OPTIONAL，最小实现不需要。

### 1.3 其他相关 RFC（已核实引用关系）

| RFC | 标题 | 作用 |
|---|---|---|
| **RFC 3510** | IPP/1.1: IPP URL Scheme | 定义 `ipp` URI scheme |
| **RFC 7472** | IPP over HTTPS Transport Binding and the 'ipps' URI Scheme | 定义 `ipps` URI + HTTPS 绑定。**更新** RFC 2910/2911。要求 **TLS 1.2 或更高** |
| **RFC 3995** | IPP: Event Notifications and Subscriptions | 事件订阅模型（可选） |
| **RFC 3996** | IPP: The 'ippget' Delivery Method | 拉取式事件通知，定义 Get-Notifications (0x001C) |
| RFC 2569 | Mapping between LPD and IPP Protocols | LPD↔IPP 映射（informative） |
| RFC 3196 | IPP/1.1 Implementor's Guide | 实现者指南（已被 PWG 5100.19 v2.0 扩展） |

### 1.4 端口（已核实）

| 用途 | 端口 | 依据 |
|---|---|---|
| IPP over HTTP（明文） | **631/tcp** | RFC 8010 / IANA |
| IPP over HTTPS（TLS） | **631/tcp** | **RFC 7472 §4.3** |

> ⚠️ **纠正一个常见误解**：`ipps` URI **默认也用 631，不是 443**。RFC 7472 §4.3 原文："all 'ipps' URIs that do NOT explicitly specify a port MUST be resolved to IANA-assigned well-known port 631"，并明确说明"port 631 is used for all IPP connections (with or without TLS)"。
>
> 实践中确实存在 `ipps://host:443/ipp/print` 这样的写法（例如 CUPS 文档的 URI 示例中列出了 `ipps://ip-address:443/ipp/print`，见 [CUPS: Using Network Printers](https://openprinting.github.io/cups/doc/network.html)），但那是**显式指定端口**的特例，用于穿透只放行 443 的网络，**不是规范默认值**。

### 1.5 最小合规服务端所需的操作

依据 **PWG 5100.14 IPP Everywhere v1.1**（[规范 PDF](https://ftp.pwg.org/pub/pwg/candidates/cs-ippeve11-20200515-5100.14.pdf)），IPP Everywhere Printer 的**必需**操作集合为：

| 操作 | operation-id | 必需性 |
|---|---|---|
| **Get-Printer-Attributes** | 0x000B | **必需** |
| **Print-Job** | 0x0002 | **必需** |
| **Validate-Job** | 0x0004 | **必需** |
| **Create-Job** | 0x0005 | **必需** |
| **Send-Document** | 0x0006 | **必需** |
| **Cancel-Job** | 0x0008 | **必需** |
| **Get-Job-Attributes** | 0x0009 | **必需** |
| **Get-Jobs** | 0x000A | **必需** |

上表的操作名与 operation-id 数值来自 **CUPS 实现文档**（[CUPS Implementation of IPP](https://www.cups.org/doc/spec-ipp.html)）与 RFC 8011 §4.2/§4.3 的操作定义，两者一致。

**IPP Everywhere 必需的文档格式**（来源同上 PWG 页面）：**PWG Raster (`image/pwg-raster`)** 与 **JPEG JFIF (`image/jpeg`，仅彩色打印机必需）**；**推荐**支持 **PDF (`application/pdf`)**。

> 💡 **对本工具的关键含义**：如果你的服务端**只接受 PDF**，就不能宣称完全符合 IPP Everywhere，因为 PWG Raster 是必需的。但对局域网共享场景，客户端几乎总是发 PDF（CUPS/macOS/Windows IPP 路径都倾向 PDF），可以先用 PDF + 声明 `document-format-supported = application/pdf` 起步，把 PWG Raster 作为后续增强。

### 1.6 最小合规服务端所需的关键属性

依据 RFC 8011 §5.4（Printer Description and Status Attributes）与 PWG 5100.13/5100.14，`Get-Printer-Attributes` 至少要能返回：

**基础标识与状态**
- `printer-uri-supported` (1setOf uri) — RFC 8011 §5.4.1
- `uri-security-supported` (1setOf type2 keyword) — RFC 8011 §5.4.3
- `uri-authentication-supported` (1setOf type2 keyword) — RFC 8011 §5.4.2
- `printer-name` (name(127)) — §5.4.4
- `printer-info` / `printer-location` (text(127)) — §5.4.6 / §5.4.5
- `printer-make-and-model` (text(127)) — §5.4.9
- `printer-state` (type1 enum：3=idle, 4=processing, 5=stopped) — §5.4.11
- `printer-state-reasons` (1setOf type2 keyword) — §5.4.12
- `printer-is-accepting-jobs` (boolean) — §5.4.23
- `queued-job-count` (integer) — §5.4.24

**能力协商（客户端据此决定发什么）**
- `ipp-versions-supported` (1setOf type2 keyword) — §5.4.14
- `operations-supported` (1setOf type2 enum) — §5.4.15
- `charset-configured` / `charset-supported` — §5.4.17 / §5.4.18（`utf-8` 必需）
- `natural-language-configured` / `generated-natural-language-supported` — §5.4.19 / §5.4.20
- `document-format-default` / `document-format-supported` — §5.4.21 / §5.4.22
- `compression-supported` — §5.4.32（至少 `none`）
- `color-supported` (boolean) — §5.4.26
- `multiple-document-jobs-supported` (boolean) — §5.4.16

**Job Template 默认值与支持值**（客户端打印时要用）
- `copies-default` / `copies-supported` — §5.2.5
- `sides-default` / `sides-supported` — §5.2.8（`one-sided`, `two-sided-long-edge`, `two-sided-short-edge`）
- `media-default` / `media-supported` / `media-ready` — §5.2.11
- `print-quality-default` / `print-quality-supported` — §5.2.13（3=draft, 4=normal, 5=high）
- `printer-resolution-default` / `printer-resolution-supported` — §5.2.12
- `orientation-requested-default` / `-supported` — §5.2.10
- `finishings-default` / `finishings-supported` — §5.2.6
- `page-ranges-supported` (boolean) — §5.2.7
- `job-creation-attributes-supported` (1setOf keyword, PWG 5100.11)

**每个请求/响应的通用操作属性（RFC 8011 §4.1.4 / §4.1.5）**
- 请求：`attributes-charset`, `attributes-natural-language`, `printer-uri`；Job 操作再加 `job-id`
- 响应：`attributes-charset`, `attributes-natural-language`, `status-message`
- 常用：`requesting-user-name` (name(MAX))、`job-name` (name(MAX))、`document-format` (mimeMediaType)
- 多文档作业：`ipp-attribute-fidelity` (boolean) 控制「不支持就失败」还是「忽略并降级」

**状态码**：成功 `successful-ok` (0x0000)；属性被忽略 `successful-ok-ignored-or-substituted-attributes` (0x0001)；客户端错误族 `0x0400`–`0x0412`；服务端错误族 `0x0500`+。完整列表见 RFC 8011 Appendix B。

### 1.7 两种提交流程（RFC 8011 §4.2.1 / §4.2.4 / §4.3.1）

- **Print-Job**：一步提交「Job + 单个文档」。实现简单，但**作业传输过程中无法可靠取消**（PWG IPP Guide 明确指出这一点）。
- **Create-Job + Send-Document**：先建 Job 拿到 `job-id`，再发文档。客户端可在传输中途**另开连接发 Cancel-Job**。这是更健壮的做法，IPP Everywhere 把它列为必需。

来源：[PWG "How to Use the Internet Printing Protocol"](https://www.pwg.org/ipp/ippguide.html)。

---

## 2. CUPS

### 2.1 架构

CUPS 的核心组件（来源：[OpenPrinting CUPS 文档](https://openprinting.github.io/cups/)、[cupsd(8)](https://openprinting.github.io/cups/doc/man-cupsd.html)）：

- **`cupsd`（scheduler）**：单一守护进程，负责 HTTP/IPP 监听、队列管理、权限策略、作业调度。`cupsd(8)` 明确写："implements a printing system based upon the Internet Printing Protocol, version 2.1, and supports most of the requirements for IPP Everywhere"。
- **backends**：把作业数据送到设备（`ipp`、`ipps`、`socket`(9100)、`lpd`(515)、`usb`、`smb`、`dnssd`…）。
- **filters**：格式转换链（PDF → PWG Raster / PostScript / 厂商 PDL…）。转换规则由 `mime.convs` / `mime.types` 描述。
- **PPD（PostScript Printer Description）**：传统的「驱动」载体，描述设备能力与选项。
- **IPP Everywhere / driverless**：不需要 PPD，直接从打印机 `Get-Printer-Attributes` 拉取能力。

> **架构层面的关键点**：CUPS 的 `cupsd` 本身就是一个完整的 IPP 服务端，**天然满足第 1 节的全部要求** —— 如果你选择「复用 CUPS 当服务端」而非从零写 IPP，你就免费获得了协议合规性、格式转换、队列与权限管理。

### 2.2 CUPS 2.x 与 3.x：PPD / 驱动到底被移除了吗？

**核实结论：在 CUPS 2.x 中，PPD 与驱动已被明确标记为「deprecated，将在未来版本移除」，但尚未移除；CUPS v3 是一次架构重构，把它拆成了新组件。** 需要区分「已声明废弃」与「已实际移除」。

**证据 A —— CUPS 2.x `lpadmin(8)` 的原文警告**（[man-lpadmin](https://openprinting.github.io/cups/doc/man-lpadmin.html)）：

> "Models other than 'everywhere' are deprecated and will not be supported in a future version of CUPS."
> "CUPS printer drivers and backends are deprecated and will no longer be supported in a future feature release of CUPS."
> "`-P` _ppd-file_ ... **Note:** PPD files and printer drivers are deprecated and will not be supported in a future version of CUPS."

同一页面也给出了迁移路径："Printers that do not support IPP can be supported using applications such as **ippeveprinter**(1)"。

**证据 B —— CUPS v3 已作为独立的、重构后的项目发布**（[OpenPrinting CUPS v3 页面](https://openprinting.github.io/cups/cups3.html)）。该页面描述 CUPS v3「supports printing to: AirPrint™ and IPP Everywhere™ printers, and Network and local (USB) printers with **Printer Applications**」，并且项目结构已被拆分为多个独立仓库：

- `libcups` — 客户端库与工具（ippfind、ipptool、ipptransform、cups-oauth、cups-x509…）
- `cups-local` — 本地打印（lp/lpr/lpadmin/lpstat/lpoptions…）
- `cups-sharing` — **本地与网络共享服务**

CUPS v3 明确不再走 PPD 驱动路线，非 IPP 打印机通过 **Printer Application**（一种把非 IPP 设备包装成 IPP 服务的独立进程）接入。

> ⚠️ **必须诚实标注的不确定性**：我**未能**核实到一份 CUPS 官方文档原文写着「PPD 支持已在 CUPS 3.0 中被彻底删除」这样一句话。可确证的是：(a) 2.x 文档反复声明 PPD/驱动已 deprecated 且「in a future version」将不再支持；(b) v3 页面描述的新架构里完全没有 PPD 驱动的位置，取而代之的是 Printer Applications。**稳妥表述：CUPS 3.x 是 driverless-first 的重构，PPD 驱动路径被有意放弃；若你的工具依赖 PPD 行为，应假定它在新版本中不可靠。**

`cups-local` / `cups-sharing` 的 `Usage` 章节在官方页面上仍标注 "Coming soon..."，说明 v3 的文档与打包生态在撰写时（2026 年）仍在完善中。

### 2.3 `cupsd.conf` 关键指令（已核实原文）

来源：[cupsd.conf(5)](https://openprinting.github.io/cups/doc/man-cupsd.conf.html)。

| 指令 | 取值 | 说明（官方原文要点） |
|---|---|---|
| **`Listen`** | `ipv4-addr:port` / `[ipv6-addr]:port` / `*:port` / `/path/to/socket` | 监听指定地址与端口或域套接字。可多行。注意：`Listen *:port` 与 `Port port` 都监听所有地址，**不能与显式 IP 的 Listen 在同一端口混用** |
| **`Port`** | number | 监听指定端口（等价 listen 全地址） |
| **`SSLListen`** | same | 监听加密连接 |
| **`SSLPort`** | number | 监听加密连接端口 |
| **`Browsing`** | `Yes` / `No` | 是否广播共享打印机。**默认 No** |
| **`BrowseLocalProtocols`** | `all` / `dnssd` / `none` | 本地共享用哪些协议。**默认在支持 Bonjour 的系统上是 `dnssd`，否则 `none`** |
| **`BrowseDNSSDSubTypes`** | `_subtype[,...]` | 为每个共享打印机广播的 Bonjour 子类型。**默认 `_cups`**（CUPS 之间共享所必需）。例如 `_cups,_print` 表示同时告知支持 CUPS 共享与 IPP Everywhere |
| **`BrowseWebIF`** | `Yes`/`No` | 是否广播 Web 界面。默认 No |
| **`DefaultShared`** | `Yes`/`No` | 本地打印机是否默认共享。**默认 Yes** |
| **`ServerAlias`** | hostname [...] / `*` | 用于 HTTP Host 头校验（外部接口访问时）。**官方警告**：用 `*` 会让系统暴露于已知的**浏览器 DNS rebinding 攻击**，即使有防火墙。建议逐条列出别名 |
| **`ServerName`** | hostname | 服务器 FQDN |
| **`DNSSDHostName`** | fqdn | Bonjour 共享所用的 FQDN，默认通常是本机 `.local` 主机名 |
| **`HostNameLookups`** | `On`/`Off`/`Double` | 是否反查客户端主机名。默认 `Off`（性能考虑） |
| **`MaxClients`** | number | 最大并发客户端数，**默认 100** |
| **`LimitRequestBody`** | size | 打印文件 / IPP 请求 / HTML 表单的最大字节数。**默认 0 = 不限制** |
| **`StrictConformance`** | `Yes`/`No` | 是否要求客户端严格遵守 IPP 规范。**默认 No** |
| **`DefaultEncryption`** | `Never`/`IfRequested`/`Required` | 已认证请求是否加密。**默认 `Required`** |
| **`DefaultAuthType`** | `Basic`/`Negotiate` | 默认认证方式。默认 `Basic` |

**访问控制**：在 `<Location>` / `<Policy>` 段内使用 `Allow all|none|host|addr/mask|@LOCAL|@IF(name)`、`Deny ...`、`Order allow,deny`、"deny,allow`、`AuthType None|Basic|Default|Negotiate`、`Require user|group|valid-user`、`Satisfy all|any`、`Encryption Never|IfRequested|Required`。

**常用路径**：`/`（所有 get 操作）、`/admin`、`/admin/conf`、`/admin/log`、`/printers`、`/printers/name`、`/printers/name.ppd`、`/jobs`、`/classes`。

**已弃用指令**（同页 "Deprecated Directives" 章节）：`Classification`、`ClassifyOverride`、`PageLogFormat`、`RIPCache`。

> ⚠️ **注意配置拆分**：文件/目录/用户相关的指令已从 `cupsd.conf` 移到 **`cups-files.conf`**，目的是防止权限提升攻击。修改 `cupsd.conf` 后**必须手动重启** scheduler。

### 2.4 开启共享的正确姿势

来源：[CUPS Printer Sharing](https://openprinting.github.io/cups/doc/sharing.html)、[cupsctl(8)](https://openprinting.github.io/cups/doc/man-cupsctl.html)。

```bash
# 1) 开启共享（同时广播 DNS-SD），并允许跨子网
cupsctl --share-printers --remote-any

# 2) 把每个要共享的队列标记为共享
lpadmin -p printer -o printer-is-shared=true

# 3) 需要认证时可设置策略
lpadmin -p printer -o printer-op-policy=authenticated
```

- `cupsctl --share-printers` 默认**只允许同子网**客户端打印；加 `--remote-any` 放开到任意地址。
- ⚠️ `cupsctl` **无法设置 `Listen` / `Port` 指令**（官方 "Known Issues"），这些必须直接改 `cupsd.conf`。

### 2.5 Linux 客户端如何添加远程 IPP 打印机

（来源：[ArchWiki CUPS](https://wiki.archlinux.org/title/CUPS)、[CUPS Printer Sharing](https://openprinting.github.io/cups/doc/sharing.html)、[man-lpadmin](https://openprinting.github.io/cups/doc/man-lpadmin.html)）

**方式 A：驱动无关（推荐）**
```bash
lpadmin -p <本地队列名> -E -v "ipp://<server>/printers/<远端队列名>" -m everywhere
```
`-m everywhere` 表示直接向目标打印机查询能力（IPP Everywhere / AirPrint 风格），不安装 PPD。

**方式 B：直接指向打印机本身**
```bash
lpadmin -p MyPrinter -E -v "ipp://11.22.33.44/ipp/print" -m everywhere
```

**方式 C：raw 队列（不做任何转换）**
```bash
lpadmin -p SHARED_PRINTER -m raw
```

**自动发现**：需要 Avahi 提供 DNS-SD/mDNS（`avahi-daemon.service`）。⚠️ **CUPS 不支持用 systemd-resolved 做 DNS-SD**（见 libcups issue #81），必须跑 avahi-daemon。已知位置时可用 `lpinfo -v` 或 `avahi-discover` 找 URI。

**⚠️ 重要陷阱（ArchWiki 明确警告）**：**不要同时在服务端和客户端配置过滤器** —— 客户端队列或服务端队列**必须有一端是 `raw`**。否则打印数据会被过滤两次，导致「本地能打印、远程打不出来」这类诡异故障。

**相关命令**：
- `lpadmin` — 增删改队列、设默认、设 ACL（`-u allow:...`/`-u deny:...`）
- `lpoptions` — 查看/设置队列选项与用户默认值（`-l` 列出、`-d` 设默认、`-o name=value`）
- `lpstat` — 查看状态（`-s` 摘要、`-p <queue>` 指定队列）。注：`lpinfo` 在 v3 中已被标记 deprecated，ArchWiki 建议改用 `lpstat`
- `cupsaccept`/`cupsreject`/`cupsenable`/`cupsdisable` — 接受/拒绝作业、启用/停用队列

### 2.6 `cups-browsed`：它到底解决什么问题

来源：[ArchWiki CUPS § cups-browsed](https://wiki.archlinux.org/title/CUPS)（该章节被标记为「out of date」，其更正说明本身很有价值）。

**关键澄清**：
- **通过 DNS-SD 发现打印机不需要 `cups-browsed`** —— 那是 `cups.service` 自己的职责。
- `cups-browsed` **只**在以下场景才需要：(1) 从 **LDAP 服务器**发现打印机；(2) 发现使用**传统 CUPS 协议（CUPS ≤ 1.5）**的服务器；(3) 需要**动态增删**随网络出现/消失的打印机。
- 通过 cups-browsed 创建的队列，作业是**直接发给打印机、不做本地处理**的，所以「队列可能不工作」，除非设备本身支持 IPP Everywhere / AirPrint。
- ⚠️ 副作用：搜索网络打印机会**显著拖慢开机时间**。

> **对本工具的含义**：**不要依赖 cups-browsed 作为跨平台共享机制**。它是为「让 Linux 客户端看到别人广播的打印机」而存在的兼容层，不是共享服务端。你的服务端应该直接广播 DNS-SD（§3）。

---

## 3. 发现机制：mDNS / DNS-SD 与 AirPrint

### 3.1 RFC 6762（mDNS）与 RFC 6763（DNS-SD）

- **RFC 6762 — Multicast DNS**（2013-02，Standards Track）。核心事实：
  - ".local." 是特殊的链路本地顶级域，**任何以 `.local.` 结尾的查询必须发往 mDNS 组播地址 `224.0.0.251`（IPv6: `FF02::FB`）**，**UDP 端口 5353**。
  - 记录 TTL：含主机名的记录（A/AAAA/SRV/反向 PTR）**推荐 120 秒**；其他记录**推荐 75 分钟**。
  - 启动时要先 **Probing**（探测名称冲突）再 **Announcing**（至少两条非请求响应，间隔 1 秒，可增至 8 条，间隔至少翻倍）。
  - 关闭时应发送 TTL=0 的 "goodbye" 包清除邻居缓存。
  - 来源：[RFC 6762](https://www.rfc-editor.org/rfc/rfc6762.txt)
- **RFC 6763 — DNS-Based Service Discovery**（2013-02，Standards Track）。核心事实：
  - 服务实例名结构：`<Instance>.<Service>.<Domain>`，如 `My Printer._ipp._tcp.local.`
  - 浏览（Service Instance Enumeration）= 对 `<Service>.<Domain>` 发 **PTR** 查询，返回一组实例名。
  - 解析（Service Instance Resolution）= 对实例名查 **SRV**（目标主机 + 端口）与 **TXT**（键值对元数据）。
  - **子类型（subtype）** 机制：`_printer._sub._http._tcp` 形式（§7.1）。IANA 注册 "ipp" 与 "pdl-datastream" 时都以 "printer" 作为该协议族的 flagship name。
  - 来源：[RFC 6763](https://www.rfc-editor.org/rfc/rfc6763.txt)

### 3.2 Bonjour 服务类型（打印相关）

| 服务类型 | 用途 | 已核实来源 |
|---|---|---|
| **`_ipp._tcp`** | IPP 打印服务（明文） | CUPS `lpinfo` 输出示例：`dnssd://Acme%20Laser%20Pro._ipp._tcp.local./?uuid=...` |
| **`_ipps._tcp`** | IPP over TLS | 同一示例：`dnssd://Example%20EX-42._ipps._tcp.local./?uuid=...` |
| **`_printer._tcp`** | LPD 打印服务 | 同一示例：`dnssd://Bar99._printer.tcp.local./?uuid=...` |
| **`_pdl-datastream._tcp`** | 原始 PDL 数据流（= 9100/JetDirect） | 同一示例：`dnssd://Foo%20Fighter-1969._pdl-datastream._tcp.local./?uuid=...` |
| **`_printer._sub._http._tcp`** | 打印机内嵌 Web 配置页（HTTP 子类型） | RFC 6763 §13.2 与 §16 明文举例 |

前三/四类来自 [CUPS: Using Network Printers](https://openprinting.github.io/cups/doc/network.html) 的真实 `lpinfo -v` 输出，可信度高。

> ⚠️ **务必注意 `_printer._tcp` 与 `_printer._sub._http._tcp` 是两回事**：前者是 **LPD** 打印服务类型（RFC 6763 §16 提到 "ipp" 和 "pdl-datastream" 都以 "printer" 为 flagship name）；后者是 **HTTP 服务的一个子类型**，用于发现打印机的 Web 管理页。二者语义完全不同，不要混用。

### 3.3 TXT 记录键

**RFC 6763 §6** 定义的 DNS-SD TXT 记录通用规则：键值对形式 `key=value`，键为小写、长度 ≤ 9（应为 ASCII 字母数字与连字符），值可为空（仅键名表示布尔存在）。建议总大小 ≤ 1300 字节以便放入单个 DNS 包。

**打印机相关 TXT 键**（`rp`, `ty`, `note`, `pdl`, `adminurl`, `UUID`, `kind`, `urf`）—— 这些键源自 Apple 的 **Bonjour Printing Specification**，并在 IPP Everywhere / AirPrint 生态中被广泛使用：

| 键 | 含义 | 核实状态 |
|---|---|---|
| `rp` | **R**esource **P**ath，打印队列的相对路径（如 `/ipp/print`） | ⚠️ 键名与语义在 RFC 6763 §13.3 的通用示例（`path=/`）与 CUPS 生态中一致，但**我未直接取到 Apple Bonjour Printing Specification 原文逐字确认每个键** |
| `ty` | 打印机型号/描述 | ⚠️ 同上 |
| `note` | 位置/备注 | ⚠️ 同上 |
| `pdl` | 支持的页面描述语言列表（逗号分隔） | ⚠️ 同上 |
| `adminurl` | 管理 Web 页 URL | ⚠️ 同上 |
| `UUID` | 打印机的唯一标识 | ⚠️ 同上 |
| `kind` | 打印机类别（如 `document`、`photo`、`envelope`） | ⚠️ 同上 |
| `urf` | 是否/如何支持 Apple Raster。**AirPrint 的关键键** | ⚠️ 键名与用途高度可信（PPD 生态中广泛存在），但**未取到 Apple 原文规范** |

**我实际能确认的、与 URF 直接相关的权威事实**：
- **`image/urf` 是 Apple Raster 的 MIME 类型**，PWG 官方 IPP 指南明确写："AirPrint Printers also support a simple raster format called Apple Raster ('image/urf')." 来源：[PWG IPP Guide §Documents](https://www.pwg.org/ipp/ippguide.html)。
- CUPS 的 IPP Everywhere 参考实现 `ippeveprinter` 提供 `-r _print` 选项来指定 DNS-SD 子类型（**默认 `_print`**），并明确 Conforming To PWG 5100.14。来源：[ippeveprinter(1)](https://openprinting.github.io/cups/doc/man-ippeveprinter.html)。

> ⚠️ **诚实声明**：任务书提到的「`_universal._sub._ipp._tcp`」、「`URF`/`urf` 键的确切取值」、「AirPrint 要求的具体属性清单」——**我未能在本次调研中取到 Apple 官方 Bonjour Printing Specification 或 AirPrint 规范的原文**（Apple 的 developer.apple.com 打印服务文档页已 404，support.apple.com 的 AirPrint 页面返回的正文被截断）。**这些具体细节需要在下一轮调研中通过 Apple 官方规范原文核实，不应基于传闻实现。**

### 3.4 AirPrint 的可靠替代核实路径

在拿到 Apple 原文之前，**最可靠的验证方法是实证**：
1. 用 CUPS 自带的 `ippeveprinter`（或 PWG 的 [ippsample](https://github.com/istopwg/ippsample)）在局域网上跑一个 IPP Everywhere 服务端。
2. 用 macOS 的 `dns-sd -B _ipp._tcp` / `dns-sd -L "<name>" _ipp._tcp` 观察它广播了哪些服务类型与 TXT 键。
3. 用 iOS/macOS 的实际打印对话框验证是否被发现。
4. 用 `ipptool`（libcups 提供，见 [ipptool(1)](https://openprinting.github.io/cups/cups3.html)）逐项检查 `Get-Printer-Attributes` 返回值。

这比基于二手资料的属性清单更可靠。

### 3.5 Windows WSD（Web Services on Devices / WS-Print）

⚠️ **未充分核实**。我能确认的是：Windows 的「添加打印机」向导支持 WSD 网络发现路径，且微软在 MFD 场景中把 **WS-Scan** 作为扫描端点方案（见下方 §5.5 引用的 Microsoft 文档，其中提到 "the Scan endpoint will work via WS-Scan or eSCL"）。

但 **WSD/WS-Print 的具体协议栈（SOAP over UDP 组播发现 + HTTP 传输）、服务类型 URI、以及实现细节，我未在本次调研中从 Microsoft 官方规范核实**。考虑到：**微软正把打印方向明确转向 IPP**（§5.5），**建议本工具不实现 WSD**，把 Windows 支持完全押在 IPP 路径上。

### 3.6 LPD / LPR（RFC 1179）

来源：[RFC 1179](https://www.rfc-editor.org/rfc/rfc1179.txt)（**Informational，非 Internet Standard**）。

- **端口 515/tcp**；客户端源端口**必须**在 **721–731** 范围内。
- 命令格式：1 字节命令码 + 打印机队列名（ASCII）+ 可选操作数 + `LF`。
- 主要命令：`01` 打印等待作业、`02` 接收打印作业、`03` 发送队列状态（短）、`04` 发送队列状态（长）、`05` 删除作业。**每条命令必须新建一个 TCP 连接**。
- 接收作业子命令：`01` 中止作业、`02` 接收控制文件（`cfA`+3位作业号+主机名）、`03` 接收数据文件（`dfA`+…）。每次子命令后需等待 1 字节确认（`0x00` 为肯定）。
- 控制文件行首字母：小写表示实际打印请求（`f` 文本、`l` 保留控制字符、`o` PostScript、`p` pr 格式、`v` Sun raster…），大写表示参数（`H` 责任主机**必需**、`P` 责任用户**必需**、`C` 类名、`J` 作业名、`L` 打印横幅、`N` 源文件名、`T` 标题、`W` 宽度…）。
- 该校验和「每条命令一个连接」的设计使得 LPD **在有状态防火墙/NAT 后面很容易出问题**。

---

## 4. Raw / JetDirect 打印

### 4.1 AppSocket / JetDirect（端口 9100）

来源：[CUPS: Using Network Printers §AppSocket](https://openprinting.github.io/cups/doc/network.html)。

- "AppSocket protocol (sometimes also called the JetDirect protocol, owing to its origins with the HP JetDirect network interfaces) **is the simplest and fastest network protocol used for printers**. AppSocket printing **normally happens over port 9100**"。
- 它**不是一个标准化协议**：就是把 PDL 字节流直接倒进 TCP 连接。
- CUPS 的 `socket` backend 支持 `contimeout`（默认 604800 秒 = 1 周）与 `waiteof`（默认 true，等待打印完成）。

### 4.2 何时可用 raw 透传，何时应避免

**CUPS 官方明确不推荐**（原文）：

> "While the AppSocket protocol is simple and fast, it **also offers no security and is often an attack vector with printers**. Consider using the Internet Printing Protocol which supports encryption and other security features."

> （LPD）"Due to limitations in the LPD protocol, **we do not recommend using it if the printer or server supports any of the other protocols**. Like AppSocket, LPD offers no security and is a common attack vector. LPD also, by default, requires that the computer save a copy of the entire print job before sending it to the printer - this can result in **gigabytes of print data being saved to disk**..."

**✅ raw 透传可接受的场景**：
- 服务端已经**自己完成**了格式渲染（例如已经产出 PWG Raster / URF 字节流），只需要把字节塞给设备。这在「服务端做转换、末端设备只认 PDL」的架构中是**正确且必要**的。
- 目标设备是纯 PDL 设备（如只认 PCL/ESC-POS 的老设备或票据打印机），且服务端已渲染完毕。

**❌ 应避免的场景**：
- **服务端直接把客户端提交的原始文件（PDF 等）透传到 9100 而不做转换** —— 设备看不懂，会打出几百页乱码，或者干脆卡住。这是极常见的误配置。
- 需要加密、认证、作业状态查询、取消能力时 —— 9100 全都没有。**IPPS 才提供这些。**

**🔒 安全提醒**：9100 与 515 都是**无认证、无加密**的明文协议，服务端若把它们暴露到不可信网络是**实际的安全风险**（CUPS 原文称之为 "often an attack vector"）。本工具应默认仅在受信局域网内绑定这些端口。

### 4.3 格式转换的职责划分（架构要点）

ArchWiki 关于 CUPS 工作流的描述（[ArchWiki CUPS §Print steps](https://wiki.archlinux.org/title/CUPS)）很好地说明了标准流程：

1. 应用把 **PDF** 交给 CUPS（其他格式先转 PDF）
2. CUPS 查 PPD，决定需要哪些 filter
3. filter 把 PDF 转成打印机懂的语言（PJL / PCL / bitmap / 原生 PDF）
4. 送到 backend（USB / socket / ipp …）

> **对本工具的含义**：如果你的服务端以 IPP 对外，**服务端就是那个 filter**。你必须明确：接受什么、转成什么、设备认什么。**如果设备本身支持 IPP Everywhere/AirPrint，最省事的做法是什么都不转，直接把 IPP 请求转发给设备**（代理模式）。

---

## 5. Windows 侧客户端路径

### 5.1 遗留路径：SMB 共享打印机 + Point-and-Print

**工作原理（架构层面）**：Windows 的 SMB 共享打印依赖 **Print Spooler 服务** 与 **`spoolss` RPC 接口**通信，走 SMB 命名管道。客户端连到服务端的共享打印机后，需要从服务端**下载并安装打印驱动**（这是 Point-and-Print 的核心语义）。

**为什么这是经典痛点**：
1. **需要远程驱动安装权限** —— 客户端要在本地安装来自服务端的驱动，这在现代 Windows 上需要管理员权限。
2. **驱动架构必须匹配** —— 服务端得为每个客户端架构（x64 / ARM64）准备对应驱动。
3. **安全强化不断收紧** —— PrintNightmare（2021）之后微软持续收紧 Point-and-Print 的驱动安装策略。

**⚠️ 未核实的具体细节**：`RestrictDriverInstallationToAdministrativeUsers` 注册表策略的精确键路径与各版本默认值、以及 2023 年后 Point-and-Print 强化的完整时间线，**我未在本次调研中从 Microsoft 官方文档逐条核实**。

**关于 SMB1 的常见误解**：**现代 Windows 打印共享不需要 SMB1** —— 它使用 SMB2/SMB3。SMB1 早已被微软标记为不安全并默认禁用。**任何要求你启用 SMB1 才能共享打印机的方案都应被视为设计缺陷。**

**关于来宾访问**：⚠️ 我未核实 Windows 11 24H2 具体的 SMB 来宾回退（guest fallback）默认禁用行为的确切版本节点与官方文档出处，**不应在实现中依赖具体版本行为**。

### 5.2 Windows 内置 IPP 客户端

**可核实的硬事实**：Microsoft 官方文档（[End of servicing plan 页面](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows)）明确写道：

> "With the release of **Windows 10 21H2**, Windows offers **inbox support for Mopria compliant printer devices** over network and USB interfaces via the **Microsoft IPP Class Driver**. This removes the need for print device manufacturers to provide their own installers, drivers, utilities, and so on."

这是**决定性的事实**：从 Windows 10 21H2 起，Windows **内置**了 IPP Class Driver，可直接驱动符合 Mopria 标准的网络打印机，**无需厂商驱动**。

同一文档还确认 IPP 覆盖 MFD 的多个功能端点：
> "For network devices, the **Print and Fax endpoints will work via IPP and IPP Fax Out**, respectively, while the Scan endpoint will work via WS-Scan or eSCL. For USB devices, the endpoints will only be accessible when the USB interface is in **IPP Over USB** mode."

> ⚠️ **未核实**：Windows IPP 客户端**具体发送哪些属性/操作**、以及**发送什么文档格式**（PDF？XPS？PWG Raster？）。微软开放规范（Microsoft Open Specifications）中可能有相关协议文档，但我未定位到并核实。**这是实现前必须实测的关键点** —— 建议用 `ipptool` 或在服务端抓包，观察真实 Windows 客户端的 `document-format` 与属性集。

### 5.3 Windows Protected Print Mode（WPPM）

**可核实的事实**：Microsoft Learn 的打印机驱动设计指南首页（[Printer driver design guide](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/)）的目录结构本身就是强证据 —— 它只列出三个主题：

1. **Modern print platform and Windows protected print mode**
2. **End of servicing plan for third-party printer drivers on Windows**
3. **Legacy v3 and v4 third-party printer drivers**

即：微软把「现代打印平台 + Windows Protected Print Mode」定位为**未来**，「遗留 v3/v4 第三方驱动」定位为**过去**。

> ⚠️ **未能核实**：`modern-print-platform` 页面的正文。该页面在本次调研中**重定向到 support.microsoft.com 且目标 404**，反复尝试（含 `?redirectedfrom=MSDN`、直接猜 support.microsoft.com 的 GUID 路径）均失败。因此**「WPPM 从 Windows 11 24H2 引入」、「WPPM 要求 Mopria 认证」、「WPPM 会禁用第三方 v3/v4 驱动」这些具体论断，我无法给出带官方 URL 的确证**。
>
> **可以合理推断的是**：WPPM 与 "Legacy v3 and v4" 并列出现在设计指南顶层的「现代平台」条目下，且与 §5.4 的驱动淘汰计划配套，方向是**明确的 IPP-only、driverless**。但**具体版本号与行为细节应在下一轮核实后再写入设计文档**。

### 5.4 微软的第三方打印驱动淘汰时间表（✅ 已核实原文）

来源：[End of servicing plan for third-party printer drivers on Windows](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows)（页面最后更新 **2025-05-09**，含 "May 2025 update – Printer driver timeline date adjustment"）。

| 时间 | 计划 |
|---|---|
| **2023 年 9 月** | 宣布 Windows 遗留第三方打印驱动进入 end-of-servicing 计划 |
| **2026 年 1 月 15 日** | 对 **Windows 11+ 与 Windows Server 2025+**，**不再向 Windows Update 发布新打印驱动**。已有驱动仍可更新，但**仅逐案审批** |
| **2026 年 7 月 1 日** | **驱动排序规则修改为始终优先选用 Windows IPP inbox class driver** |
| **2027 年 7 月 1 日** | 除安全修复外，**不再允许第三方打印驱动更新**。已有驱动仍可从 Windows Update 安装，或由厂商用安装包提供 |

**关键 FAQ 摘录**：
- 驱动签名的例外条件之一：**"A printer that cannot be Mopria certified."**（即只有无法通过 Mopria 认证的设备才需要厂商驱动）
- **"Does end of servicing apply to all versions of Windows?"** → **"This change applies to Windows 11+ and Windows Server 2025+."**
- **"Will Windows prevent installation of new printer drivers?"** → "Windows will continue to allow vendor-supplied printer drivers to be installed via separate installation packages."（**不是硬性阻止**，而是从 Windows Update 渠道退出）
- **"Does Microsoft have plans to remove existing features supported by v3 and v4 printer drivers?"** → **"There are no plans to disable print features specifically related to the legacy printer driver platform."**
- 微软会继续为遗留驱动平台发布安全修复，只要该 Windows 版本仍在支持生命周期内。

> **对本工具的战略含义**：**这是一个强烈的方向性信号。** 到 2026-07，Windows 会**优先**选用 IPP class driver；到 2027-07，第三方驱动更新基本终结。**构建 IPP-first 的共享工具，其价值随时间单调上升；构建 SMB 驱动分发方案，其价值随时间单调下降。**

### 5.5 LPR Port Monitor 在 Windows 上是否是可选的

⚠️ **未能核实**。我未能取到 Microsoft 官方文档，确认：
- LPR Port Monitor 在 Windows 10/11 客户端与 Windows Server 上是否**默认安装**；
- 其**确切的「可选功能」名称**（例如 "LPR Port Monitor"、"Print and Document Services"、"Internet Printing Client" 等）；
- 是否在近期版本中被移除或降级。

**因此：不要在设计文档中声称 "LPR 是/不是 Windows 可选功能"，需要单独核实。** 从架构角度，**即使 LPR 可用也不应作为主路径**（§4.2 的不推荐理由 + §3.6 的有状态防火墙问题）。

### 5.6 Standard TCP/IP Port 与 RAW 协议

CUPS 文档与业界惯例表明：Windows「添加打印机 → 使用 TCP/IP 地址或主机名」向导可创建 **Standard TCP/IP Port**，并可在 **RAW（默认 9100）** 与 **LPR（515）** 两种协议间选择。

⚠️ **未能核实**：Microsoft 官方对 Standard TCP/IP Port 的 RAW/LPR 默认端口取值与行为描述的确切文档。

**但方向是清楚的**：RAW/9100 是 Windows 上「把字节直接倒给设备」的传统做法，**它要求服务端（或设备）已经能理解客户端发来的字节** —— 这正是 §4.2 中「服务端必须自己完成渲染」的约束。**在 IPP 路径下不需要这个端口。**

---

## 6. 法律与许可要点

### 6.1 CUPS：Apache-2.0（✅ 已核实）

**已核实事实**：OpenPrinting CUPS 的许可证页面**标题即为 "Apache License Version 2.0"**，正文是完整的 Apache 2.0 条款。来源：[CUPS License](https://openprinting.github.io/cups/doc/license.html)。

该页面还包含两段**对产品化很关键的例外条款**（"CUPS Exceptions to the Apache 2.0 License"）：

1. **嵌入例外**：若因编译源码导致软件部分以目标码形式嵌入，可重新分发该嵌入部分**而无需遵守第 4(a)、4(b)、4(d) 条**（即无需携带许可证副本、无需标注修改、无需携带 NOTICE 归属声明）。这对静态链接/嵌入场景很宽松。
2. **GPLv2 兼容例外**：若将本软件编译形式与 **GPLv2** 软件组合（"Combined Software"），且法院判定本许可证的专利条款（§3）、赔偿条款（§9）或其他条款与 GPLv2 冲突，你可以**追溯性地和前瞻性地选择放弃或排除**这些条款，但仅限其整体、且仅针对该组合作品。

CUPS v3 页面同样声明："CUPS v3 is licensed under the Apache License Version 2.0 **with an exception to allow linking against GNU GPL2-only software**."（[CUPS v3](https://openprinting.github.io/cups/cups3.html)）

> **⚠️ 关于「CUPS 在 2007 年重新许可为 Apache 2.0」这一说法**：任务书提到这一时间点。我**核实了「CUPS 现在是 Apache-2.0」这一事实**（有官方许可证页面为证），但**未核实到 2007 年这个具体年份**的官方公告原文。**年份不应作为事实引用。** 对产品决策而言，**当前版本是 Apache-2.0 才是关键，年份无关紧要**。

> **✅ 对「发货产品」的含义**：CUPS 本体是**商业友好**的。你可以链接、嵌入、随产品分发、闭源修改，只需遵守 Apache 2.0 的归属与变更声明要求（并为嵌入场景享受上述例外）。

### 6.2 `cups-filters`：Apache-2.0（含 GPL2/LGPL2 例外）—— **本节已更正**

> **❗ 更正说明**：本节原先写作"GPL-2 / copyleft 风险，采用前必须审计"。**该描述是错误的，现已更正。**
> 更正依据（Lead 直接抓取一手来源核实）：
> - `OpenPrinting/cups-filters` 的 `COPYING` 文件中 `License:` 字段逐字为
>   **`Apache-2.0-with-GPL2-LGPL2-Exception`**（Debian copyright-format 1.0），
>   正文即 Apache-2.0 许可证文本 + CUPS 的两项例外（嵌入例外、GPLv2 链接例外）。
>   来源：<https://raw.githubusercontent.com/OpenPrinting/cups-filters/master/COPYING>
> - GitHub API 对同一仓库返回 `"license": {"spdx_id": "Apache-2.0"}`。
>
> **与 `02-prior-art-and-stacks.md` §A.1 一致**（该文一直写的是 Apache-2.0，两者原先相互矛盾，现以本节更正为准）。

- **已核实**：`cups-filters` 是 OpenPrinting 维护的独立项目，其 GitHub 仓库描述为 "filters, backends, driverless utility, ... **Everything which CUPS 2.x needs to be used on non-Mac systems**"。来源：[OpenPrinting/cups-filters](https://github.com/OpenPrinting/cups-filters)。
- **已核实**：它**曾经是 CUPS 的一部分，后被移出**（ArchWiki："Those are backends, filters, and other binaries that were **once part of CUPS but have been dropped from the project**"）。来源：[ArchWiki CUPS §OpenPrinting CUPS filters](https://wiki.archlinux.org/title/CUPS)。
- **已核实（更正后）**：许可证为 **Apache-2.0-with-GPL2-LGPL2-Exception** ——
  与 CUPS 本体同为商业友好，**且带有"允许链接 GPL2-only 软件"的例外**。

> **✅ 更正后对产品的含义**：`cups-filters` **不是** copyleft，**不构成"链接即需 GPL-2 开源"的风险**。
> 原先"采用前必须审计"的结论**不再成立**。
> **这削弱了"为规避许可证而避免 CUPS 组件"的论据** —— 但注意：
> 我们避免驱动栈的主因是**体积与维护成本**（cups-filters 246 MB），而非许可证。
> **真正的许可证地雷是 Ghostscript（见 6.3）**，而"只接受 PDF/PWG/JPEG 即无需光栅化"的策略**独立成立**。

### 6.3 Ghostscript：AGPL（⚠️ 部分核实）

- **已核实**：Ghostscript 官网的 Licensing 页面**不再自行列出条款**，而是写道："**Licensing is handled exclusively by Artifex Software, Inc.** Please visit the Artifex Licensing page." 来源：[ghostscript.com/licensing](https://www.ghostscript.com/licensing/index.html)。
- **已核实（间接但重要）**：ArchWiki 明确指出 **"Non-PDF printers require ghostscript to be installed."**，即 Ghostscript 在 Linux 打印栈中是**把 PDF 转成非 PDF 打印机所需格式**的关键组件。来源：[ArchWiki CUPS §Drivers](https://wiki.archlinux.org/title/CUPS)。
- **⚠️ 未能核实**：Artifex 页面未能取到，因此**「Ghostscript 采用 AGPL」这一具体许可证主张，我未能从官方页面逐字确认**（业界共识如此，Artifex 提供 AGPL 开源版 + 商业授权双轨，但我没有拿到本次抓取的原文证据）。

> **⚠️ 对产品的含义（无论 AGPL 细节如何，风险都很高）**：**AGPL 对网络服务是最严格的 copyleft** —— 即使你只是把它作为网络服务运行而不分发二进制，AGPL 也可能要求你向用户提供完整对应源码。**在一个要「发货」的产品里打包 Ghostscript，是本次调研中风险最高的动作。**
>
> **✅ 关键的规避策略（也是本调研最有价值的结论之一）**：
> **如果你的服务端只接受 PDF / PWG Raster / URF / PostScript 输入，你完全不需要 Ghostscript。**
>
> 推理链：
> - Ghostscript 的职责是 **把 PDF（或 PostScript）渲染成光栅/其他 PDL**，供**不理解 PDF 的设备**使用（ArchWiki："Non-PDF printers require ghostscript"）。
> - 如果**目标设备自己能直接吃 PDF / PWG Raster / JPEG / PostScript**，就**不存在渲染这一步**，Ghostscript 就是多余的。
> - 现代设备（2010 年后）绝大多数支持 AirPrint / IPP Everywhere，**天然接受 PDF 与 PWG Raster**（PWG 官方："Required: IPP/2.0, DNS-SD, **PWG Raster and JPEG JFIF** file formats (JPEG only required for color printers). **Recommended**: PDF, IPP-USB"，来源 [IPP Everywhere 页面](https://www.pwg.org/ipp/everywhere.html)）。
>
> **所以：优先选择「支持 PWG Raster / PDF 的设备 + 不做渲染的服务端」，可以从架构上完全绕开 Ghostscript 的许可证风险。**

### 6.4 许可证速查表

| 组件 | 许可证 | 已核实程度 | 产品化含义 |
|---|---|---|---|
| **CUPS (OpenPrinting)** | **Apache-2.0** + 嵌入例外 + GPLv2 链接例外 | ✅ 官方许可证页面 | **商业友好**，可链接/嵌入/闭源分发 |
| **cups-filters** | **Apache-2.0-with-GPL2-LGPL2-Exception** | ✅ `COPYING` 原文 + GitHub API | **商业友好**（**已更正**：非 GPL-2，无 copyleft 风险） |
| **Ghostscript** | AGPL（待官方页面确认） | ⚠️ Artifex 独家授权已核实，许可证名未核实 | **最高风险**；**可通过只接受 PDF/PWG Raster/JPEG 输入来完全规避** |
| **RFC 文档中的代码组件** | Simplified BSD（RFC 信托法律条款 §4.e） | ✅ RFC 8010/8011 版权声明原文 | 提取示例代码需附 BSD 文本 |

（RFC 版权声明原文："Code Components extracted from this document must include Simplified BSD License text as described in Section 4.e of the Trust Legal Provisions"—— 见 [RFC 8010](https://www.rfc-editor.org/rfc/rfc8010.txt) 与 [RFC 8011](https://www.rfc-editor.org/rfc/rfc8011.txt)。）

---

## 7. 服务端架构方案对比

| # | 方案 | 实现复杂度 | 协议合规性 | Windows | macOS/iOS | Linux | 格式转换责任 | 许可证风险 | 可维护性 |
|---|---|---|---|---|---|---|---|---|---|
| **A** | **复用 CUPS 作 IPP 服务端**<br>（`cupsd` + `-m everywhere` 队列 + DNS-SD 广播） | **极低**（配置为主，几乎不写代码） | **高**（cupsd 声明实现 IPP/2.1 + 大部分 IPP Everywhere） | ✅ 内置 IPP Class Driver (Win10 21H2+) | ✅ AirPrint | ✅ 原生 | CUPS + filters 承担 | 中（依赖 cups-filters/Ghostscript 时升高；纯 IPP Everywhere 路径可避开） | **高**（成熟项目，社区维护） |
| **B** | **基于 libcups 自建 IPP 服务端**<br>（用 libcups 做协议层，自己管队列与转发） | 中 | 高（协议层复用 libcups） | ✅ | ✅ | ✅ | 自己承担（或转交设备） | **低**（libcups 是 Apache-2.0） | 中高 |
| **C** | **从零实现 IPP Everywhere 服务端**<br>（自己解析 `application/ipp`、处理操作与属性） | **高** | 取决于实现质量，需自证合规 | ✅ | ✅ | ✅ | 完全自己承担 | **最低**（无第三方依赖） | **低**（协议面大，长期维护成本高） |
| **D** | **用 PWG ippsample / ippeveprinter 作内核**<br>（拿参考实现改造成产品） | 低-中 | 高（参考实现，声明符合 PWG 5100.14） | ✅ | ✅ | ✅ | 由 `-c command` 指定的外部命令承担 | 需核实 ippsample 许可证（⚠️ 未核实） | 中（依赖上游参考实现） |
| **E** | **纯转发/代理模式**<br>（服务端不渲染，把 IPP 请求原样转发给支持 IPP 的设备） | **低** | 高（对客户端呈现完整 IPP） | ✅ | ✅ | ✅ | **无**（设备自己处理） | **最低**（且**完全不需要 Ghostscript**） | **高** |
| **F** | **SMB 共享 + Point-and-Print** | 中 | N/A（非 IPP） | ⚠️ 可用但脆弱（需驱动分发、提权） | ❌ macOS 已弱化 SMB 打印 | ⚠️ 需 Samba | 客户端驱动承担 | — | **低**（且微软正在淘汰该路径） |
| **G** | **仅 raw 9100 透传** | 极低 | **无**（非标准化，无状态/无认证） | ⚠️ 需手配 Standard TCP/IP Port | ❌ | ⚠️ | 服务端必须预先渲染 | 最低 | 中（但功能与安全性差） |

### 推荐路线

**首选 E + A 的组合**：

1. **服务端对客户端呈现一个 IPP Everywhere 端点**（广播 `_ipp._tcp` / `_ipp._tcp,_print` 子类型 + 正确的 TXT 键）。
2. **转发到后端**：
   - 若目标是支持 IPP 的设备 → **走方案 E（纯 IPP 转发）**，零渲染、零许可证风险。
   - 若目标是 USB 或纯 PDL 设备 → 用 CUPS 作为后端引擎（**方案 A**），但把队列配成 `raw` 以避免双重过滤（§2.5 的 ArchWiki 警告），并在服务端侧自己控制格式。
3. **避免 SMB 与 9100 作为主路径**，只在前两者都不可用时作为显式降级选项，并给出安全警告。

**为什么这是最优的**：
- **协议方向与行业方向一致**（微软 2026-07 起优先 IPP class driver，2027-07 起第三方驱动更新终结 —— §5.4）。
- **三个平台的客户端路径统一收敛到 IPP**，无需为每个平台写不同的服务端逻辑。
- **可以完全避开 Ghostscript 的 AGPL 风险**（§6.3）。
- **实现量可控** —— 如果选方案 A/E，核心工作量在配置与转发逻辑，而不是重写协议栈。

---

## 8. 待核实清单（下一轮调研必须解决）

按对实现的影响程度排序：

| 优先级 | 待核实项 | 为什么重要 | 建议核实方式 |
|---|---|---|---|
| 🔴 **P0** | **Apple Bonjour Printing Specification 与 AirPrint 的原文要求**：`urf` 键的确切取值与语义、`_universal._sub._ipp._tcp` 子类型是否存在、AirPrint 必需的属性/服务类型清单 | 直接决定 macOS/iOS 客户端能否发现你的服务端 | 取 Apple 官方 Bonjour Printing Specification PDF；或用 `dns-sd -B` / `dns-sd -L` 对真实 AirPrint 打印机与 `ippeveprinter` 做实证对比 |
| 🔴 **P0** | **Windows IPP 客户端实际发送什么**：`document-format`（PDF？XPS？PWG Raster？）、使用哪些操作与属性 | 决定服务端必须支持哪些文档格式与属性，否则 Windows 打不出来 | 用 `ipptool` 或在服务端加日志抓真实 Windows 客户端的请求 |
| 🟠 **P1** | **Windows Protected Print Mode 官方原文**：引入版本、Mopria 认证要求、是否禁用第三方 v3/v4 驱动 | 决定未来 Windows 客户端的可行性边界 | 重新定位 `modern-print-platform` 页面的有效 URL（本次重定向到 support.microsoft.com 后 404） |
| 🟠 **P1** | **LPR Port Monitor 是否为 Windows 可选功能及其确切名称** | 决定 LPR 兜底路径是否可行 | Microsoft Learn 的 Windows 可选功能 / Features on Demand 文档 |
| ✅ **已解决** | ~~**cups-filters 的确切 SPDX 许可证**（GPL-2.0-only vs or-later）~~ | **已实测为 `Apache-2.0-with-GPL2-LGPL2-Exception`，非 GPL-2** | 见 §6.2 更正说明 |
| 🟡 **P2** | **Ghostscript 的确切许可证**（AGPL-3.0？） | 决定规避策略的紧迫性 | 读 Artifex licensing 页面（本次未取到） |
| 🟡 **P2** | **PWG ippsample 的许可证** | 若采用方案 D 则关键 | 读仓库 LICENSE |
| 🟢 **P3** | **CUPS 重新许可为 Apache-2.0 的确切年份与公告** | 仅影响文献引用准确性，不影响决策 | CUPS 历史/发布说明 |
| 🟢 **P3** | **Windows Standard TCP/IP Port 的 RAW/LPR 默认行为官方文档** | 仅影响降级路径的文档准确性 | Microsoft Learn |

---

## 参考来源

以下是本次调研中**实际抓取并阅读**的 URL。

### IETF RFC
- [RFC 8010 — Internet Printing Protocol/1.1: Encoding and Transport](https://www.rfc-editor.org/rfc/rfc8010.txt)
- [RFC 8011 — Internet Printing Protocol/1.1: Model and Semantics](https://www.rfc-editor.org/rfc/rfc8011.txt)
- [RFC 7472 — IPP over HTTPS Transport Binding and the 'ipps' URI Scheme](https://www.rfc-editor.org/rfc/rfc7472.txt)
- [RFC 3998 — IPP: Job and Printer Administrative Operations](https://www.rfc-editor.org/rfc/rfc3998.txt)
- [RFC 6763 — DNS-Based Service Discovery](https://www.rfc-editor.org/rfc/rfc6763.txt)
- [RFC 6762 — Multicast DNS](https://www.rfc-editor.org/rfc/rfc6762.txt)
- [RFC 1179 — Line Printer Daemon Protocol](https://www.rfc-editor.org/rfc/rfc1179.txt)
- [RFC 3996 — IPP: The 'ippget' Delivery Method for Event Notifications](https://datatracker.ietf.org/doc/rfc3996/)

### PWG（Printer Working Group）
- [PWG Published Standards（含 5100.14 IPP Everywhere、5100.18 INFRA 等完整清单）](https://www.pwg.org/standards.html)
- [PWG IPP Everywhere™ 技术主页](https://www.pwg.org/ipp/everywhere.html)
- [PWG "How to Use the Internet Printing Protocol"（IPP 实现指南）](https://www.pwg.org/ipp/ippguide.html)
- [PWG ippsample 参考实现仓库](https://github.com/istopwg/ippsample)

### OpenPrinting CUPS
- [CUPS License（Apache-2.0 + 例外条款）](https://openprinting.github.io/cups/doc/license.html)
- [OpenPrinting CUPS v3 项目主页](https://openprinting.github.io/cups/cups3.html)
- [cupsd.conf(5) — 服务器配置指令](https://openprinting.github.io/cups/doc/man-cupsd.conf.html)
- [cupsd(8) — scheduler](https://openprinting.github.io/cups/doc/man-cupsd.html)
- [cupsctl(8)](https://openprinting.github.io/cups/doc/man-cupsctl.html)
- [lpadmin(8) — 配置打印机与类](https://openprinting.github.io/cups/doc/man-lpadmin.html)
- [ippeveprinter(1) — IPP Everywhere 参考服务端](https://openprinting.github.io/cups/doc/man-ippeveprinter.html)
- [CUPS Printer Sharing（共享配置指南）](https://openprinting.github.io/cups/doc/sharing.html)
- [CUPS: Using Network Printers（AppSocket/IPP/LPD 与端口）](https://openprinting.github.io/cups/doc/network.html)
- [CUPS Implementation of IPP（操作与 operation-id 完整表）](https://www.cups.org/doc/spec-ipp.html)
- [OpenPrinting/cups-filters 仓库](https://github.com/OpenPrinting/cups-filters)

### Microsoft
- [Printer driver design guide（目录结构揭示 Modern print platform / End of servicing / Legacy v3-v4 三分法）](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/)
- [End of servicing plan for third-party printer drivers on Windows（驱动淘汰时间表 + FAQ）](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows)

### Apple
- [About AirPrint（Apple 支持页面，正文被截断，未获得实质性技术细节）](https://support.apple.com/en-us/102895)

### 其他
- [ArchWiki: CUPS（工作流、lpadmin/lpoptions/lpstat、cups-browsed 澄清、filter 双重配置警告、Ghostscript 依赖）](https://wiki.archlinux.org/title/CUPS)
- [Ghostscript Licensing 页面（指向 Artifex）](https://www.ghostscript.com/licensing/index.html)

### 抓取失败 / 无法核实的 URL
- `https://developer.apple.com/library/archive/documentation/Networking/Conceptual/PrintServices/Introduction/Introduction.html` → **HTTP 404**（Apple 已下线该文档）
- `https://learn.microsoft.com/en-us/windows-hardware/drivers/print/modern-print-platform` → **重定向至 support.microsoft.com 后 404**（WPPM 正文无法获取）
- `https://learn.microsoft.com/en-us/windows-hardware/drivers/print/protected-print-mode` → **HTTP 404**
- `https://learn.microsoft.com/en-us/windows-hardware/drivers/print/mopria-certification` → **HTTP 404**
- `https://openprinting.github.io/cups/doc/specs.html` → **HTTP 404**（页面已迁移）
- `https://openprinting.github.io/cups/doc/man-cups-browsed.html` → **HTTP 404**（页面已迁移）
- `https://www.msweet.org/blog/2023/09/25/airprint-vs-ipp-everywhere.html` → **HTTP 404**
- `https://github.com/OpenPrinting/cups/issues/103` → **请求超时**
- `https://blogs.windows.com/windows-insider/2023/12/06/windows-11-insider-preview-build-26020-canary-channel/` → **HTTP 404**
- **`web_search` 工具全程不可用**（DeepSeek API 返回 HTTP 401 认证失败），本次调研**完全依赖直接 `web_fetch` 已知权威 URL**。这意味着可能存在未被发现的更权威来源，建议在后续轮次中恢复搜索能力后做交叉验证。

---

*文档版本：2026 年调研，基于上述来源在撰写时点的内容。标注 ⚠️ 的项目在投入实现前必须完成核实。*
