# 调研 13：v0.3 零配置改版详细设计（单程序·角色向导·4 位码配对·全自动）

- **性质**：产品 + 技术详细设计（非实测记录）。决策结论见 [ADR-0010](../adr/0010-role-wizard-pairing.md)。
- **状态（2026-10-17）**：本文已在 v0.3.0 实施。§3 产品界面与 §4.8 UI 被
  [调研 14](14-minimal-two-mode-redesign.md) 取代；配对、装队列、多打印机、
  热生效、日志契约仍然有效。文中 `file:line` 是设计时的锚点，不是当前行号。
- **日期**：2026-09-30
- **输入**：用户对 v0.2 流程的反馈（"步骤太复杂，要越傻瓜越好"），以及
  [调研 12](12-windows-acceptance-2026-09-30.md)（真机验收暴露的流程断点）。
- **现状锚点**：commit `03edb10` + 工作区未提交的三项验收修复（multi-NIC mDNS
  egress、TLS 中文主机名、`dyj status` formats）。**文中引用的 `file:line`
  基于此状态，实施时需复核。**未提交修复是本设计的前提依赖（尤其 mDNS egress：
  接收端"自动扫描"的可靠性建立在它之上），实施前应先落库。

## 0. 一句话方案

`dyj` 保持单二进制：**首次运行自动弹出网页向导，只有两个入口——「共享本机打印机」
和「添加网络打印机」**。共享端自动枚举本机打印机、勾选（默认全选）、一键共享、
后台常驻；接收端自动扫描局域网、点选、输入服务端显示的 4 位配对码，dyj 自动把
打印机装成**系统队列**——此后 WPS/Word/记事本的打印对话框里直接可见可打，
**dyj 不在打印路径中、不需要常驻**。全部自动化步骤配检查点、结构化事件日志与
故障树，出错时可被普通人按文档定位。

## 1. 目标与非目标

### 1.1 三条产品原则（本设计的验收尺度）

1. **零输入**：向导全程不需要用户填写 IP、端口、队列名、URI——这些要么自动
   探测，要么根本不暴露。
2. **一键完成**：每个角色从打开向导到可用，点击次数 ≤ 4（选角色 → 选打印机 →
   [输配对码] → 完成）。
3. **失败可诊断**：任何自动化步骤失败时，用户当场看到一句人话 + 一个动作
   （重试/复制诊断/看指引）；维护者能凭事件日志 + 检查点表定位到具体环节
   （见 §5）。

### 1.2 非目标

- **不做高级设置面板**（用户明确不要）。端口/TLS/网卡选择等保留在 CLI 与
  `service.json`，不进向导 UI。
- 不做账号体系、云端中继。
- **不做 mDNS 跨网段转发。** mDNS 是链路本地组播（RFC 6762），路由器不得
  转发，mesh/VPN 也没有「组播给所有成员」的语义。跨网段添加不靠把组播桥
  过去：v0.4.1 起用手动地址（[调研 15](15-discovery-cross-subnet-mesh.md) P1），
  v0.4.2 起对已经添加过的地址做应用层重发现（P2）。产品仍然不自建组播隧道，
  也不内置 mDNS reflector。
- 不自研打印机驱动；渲染仍复用系统驱动栈（ADR-0004 架构不变）。
- 不改变 IPP wire 层（`pkg/attributes` 黄金字节不受影响）。

## 2. 现状差距分析

| # | 缺口 | 现状锚点 | 对零配置的影响 |
| --- | --- | --- | --- |
| 1 | 无参无引导 | `cmd/dyj/main.go:28-31` 无参数只打印用法退出 | 双击 exe 的用户第一秒就迷路 |
| 2 | 首启死锁 | `cmd/dyj/serve.go:320-323` Windows 缺 `-printer` 硬失败退出；`packaging/windows/dyj.iss:55` 安装器无参 `service install` 放大此问题 | 服务起不来 → web 向导（向导本体在 web 里）也打不开 |
| 3 | Windows 无本地打印机枚举 | 全仓库无 `EnumPrinters`/`Get-Printer` 调用；wingdi 只收队列名字符串（`pkg/driver/wingdi_windows.go:57-65`） | 共享端向导无法自动列出打印机，只能让用户手填队列名（现状向导即如此，`zh-CN.js:67` 甚至教用户跑 `Get-Printer`） |
| 4 | 数据面零认证、无配对机制 | `pkg/ipp/server.go:150-198` 任何来源的 IPP 请求都受理；admin token 是 64 位 hex（`pkg/admin/admin.go:376-424`） | 局域网任何人可打；也没有"4 位码"这种人类可输的配对通道 |
| 5 | 接收端装机零代码 | 无任何 `Add-Printer`/`lpadmin` 调用；靠文档教用户在 Windows 设置里手动加 IPP（`serve.go:285-287`、`zh-CN.js:52-55`）；[调研 12](12-windows-acceptance-2026-09-30.md) §29 记录 `Add-Printer -IppUrl` 需提权 | "添加后 WPS 直接可见"这一步完全靠人肉，是流程最痛的一环 |
| 6 | 单打印机模型 | `pkg/ipp/server.go:150` 所有路径一锅端映射到一个后端；`serveOptions` 只有单个 `printer` 字段（`serve.go:41-53`） | 无法"全部共享/勾选共享多台" |
| 7 | 可观测性薄弱 | 日志无 level、无结构化、无文件落盘（服务模式仅 stderr，`serve.go:91-93`）；doctor 7 项全在服务端视角（`pkg/doctor/doctor.go:49-61`），防火墙只给命令不执行 | 自动化越多，无声失败越难查——正是用户点名要补的 |

既有可复用资产：web 向导 4 步骨架（`pkg/admin/web/pages/page-wizard.js`）、
mDNS 双路浏览与 TXT `Extra` 扩展点（`pkg/discovery/mdns.go:772-841`）、
Linux/macOS CUPS 队列枚举（`pkg/driver/cups/cups.go:132-162`）、SCM 服务与
崩溃自愈（`pkg/service/service_windows.go:99-131`）、托盘 + HKCU 自启
（`pkg/tray/tray_windows.go:189-225`）、LogRing/SSE 日志页、诊断组件
（`dyj-diagnostic.js`）。**本设计尽量长在这些资产上，而不是另起炉灶。**

## 3. 产品设计

### 3.1 角色模型

- UI 只出现两个功能动词卡片（不用"服务端/客户端"这种实现词）：
  - **「共享本机打印机」**（share）：把这台机器上的打印机共享给局域网。
  - **「添加网络打印机」**（use）：把局域网上别人共享的（或任何 IPP）打印机装到
    本机，任意应用可打。
- 一台机器可以两个都做（向导完成后再进向导选另一项即可），配置里
  `role ∈ {"", "share", "use", "both"}`，`""` = 未配置。
- `use` 角色不常驻：向导装完系统队列后 dyj 即可退出，之后打印走系统队列直连
  服务端。dyj 只在"再次配置/诊断"时被打开。

### 3.2 共享端向导（用户视角）

1. 打开 dyj（桌面图标或双击 exe）→ 浏览器自动打开向导，两卡片，点「共享本机打印机」。
2. 页面自动列出本机打印机（名称 + 驱动 + "默认"徽章），**默认全选**；虚拟打印机
   （Print to PDF 等）默认隐藏，页面提供一行小链接"显示虚拟打印机"（不算高级设置）。
   若某台打印机探测失败（打不开句柄），该行标红并注明原因，不阻塞其他行。
3. 点「开始共享」→ 向导原地显示进度（逐台探测 → 后端就绪 → mDNS 宣告，全部
   自动）→ 完成页：**大字显示 4 位配对码** + "局域网其他电脑现在可以添加这台
   打印机了"。
4. 之后 dyj 在后台运行（系统服务或托盘），用户无感。再打开 dyj = 管理页
   （状态/作业/日志/配对码/设备管理）。

### 3.3 接收端向导（用户视角）

1. 打开 dyj → 「添加网络打印机」。
2. 自动扫描，列表实时出现：**局域网所有 `_ipp._tcp` 打印机**——dyj 共享的带
   dyj 徽章与"需配对"标记，普通网络打印机（路由器 USB 共享、支持 IPP 的
   网络打印机）也直接列出、无需配对即可添加——顺带让 dyj 成为通用的
   "添加打印机"入口。
3. 点选一台：
   - dyj 共享且需配对 → 弹出 4 位码输入框（服务端管理页/托盘可见该码）→ 输对
     即配对成功；输错给剩余次数提示，锁定时告知等待时间。
   - 无需配对 → 直接进入安装。
4. dyj 自动把打印机装成系统队列（需要管理员权限时见 §4.6——安装器场景
   **零弹窗**，绿色场景弹一次 UAC/sudo）→ 装完自动验证 → 完成页提供
   **「打印测试页」**一键验证端到端。
5. 完成。此后在 WPS/Word/记事本打印对话框直接选择该打印机，dyj 无需运行。

### 3.4 配对码的呈现（服务端）

4 位数字码，三个可见渠道，任一即可完成配对：

| 渠道 | 位置 | 时机 |
| --- | --- | --- |
| 管理页首页 | 常驻卡片：4 位大字 + 剩余有效期倒计时 + 「换一个」按钮 | 共享端配置完成后始终可见 |
| 托盘菜单 | 菜单项「配对码：4831」+「刷新配对码」 | Windows 托盘 |
| 控制台 banner | serve 启动横幅追加一行 | CLI/服务日志场景 |

### 3.5 日常状态与"傻瓜"边界

- 共享端后台 = 已有 SCM 服务 + 托盘（ADR-0008 D4），不新增常驻形态。
- 接收端无常驻、无托盘、无打印路径组件——**这是刻意的设计边界**：中间层越
  少，WPS 兼容性越接近"真网络打印机"，故障面越小。
- 旧 CLI 子命令（`serve/browse/print/...`）全部保留，供高级用户与自动化；
  向导是新用户唯一被引导的路径。

## 4. 技术设计

### 4.1 无参入口 = 状态机

`dyj`（无参数，Windows 双击同此）：

```
读取 <stateDir>/service.json（pkg/service/service.go:36-49）
├─ 不存在或 role == ""   → bootstrap 模式
└─ role != ""
   ├─ share / both → 正常 serve（读持久化配置）
   └─ use         → 打开接收端管理页（已装队列列表 + 重新扫描/诊断），不常驻
```

**bootstrap 模式**（新增，修复缺口 #2 的根本手段）：

- 起一个**最小 HTTP 服务**：只挂 admin UI、`/admin/api/*`、`/pair`、本地枚举/
  装队列 API。**不创建任何打印后端、不做 wingdi 校验**——服务必须永远起得来。
- 端口：默认 6631；被占用则自动递增探测空闲端口（`net.Listen` 试探），并在
  banner/向导里提示实际端口。bootstrap 连"打印共享"都没有，占用 6631 的往往
  是旧 dyj 实例——此时直接打开旧实例的 admin（探活 `GET /admin/api/status`）。
- 自动打开浏览器：`http://127.0.0.1:<port>/admin/#/setup`（新路由，角色选择
  页；复用 `app.js:92-101` 的首跑跳转判定，改为服务端 `role` 字段驱动，不再
  依赖 localStorage 的 `dyj_seen_wizard`——"已配置"必须落盘在服务端）。
- Windows 控制台窗口：绿色用户双击 exe 会有控制台窗（v0.3 接受，托盘仍是
  `dyj-tray`）；安装器用户的桌面快捷方式指向新子命令 **`dyj open`**（打开
  管理页后即退，无控制台残留问题——`-H=windowsgui` 的 dyj-tray 亦可直接
  调用）。

**配置应用 = 进程内重建**（不是进程重启）：

- `PUT /admin/api/config` 保存后，服务进程内**优雅拆除再重建** IPP handler、
  mDNS Advertiser、spooler、后端实例；**HTTP listener 不关闭**（端口不变），
  管理页连接不断。现状 `restart_required` 恒真的问题（`admin.go:305-320`）
  就地消失，也不需要 SCM 重启权限。
- 拆除顺序：先停 mDNS 宣告（发 goodbye，`mdns.go:211-229`）→ 停接收新作业
  （spooler drain）→ 等在打作业完成或超时 → 换新实例。全程事件日志（§5.2
  的 CFG-* 检查点）。
- SCM"优雅退出 + 恢复动作自动拉起"（`service_windows.go:99-131` 已配崩溃
  自愈）保留为**兜底**：进程内重建失败时才 `os.Exit(特殊码)` 让 SCM 拉起。
  主动退出码是否触发 recovery action 需实测（开放问题 §8-Q1）。

### 4.2 配对协议

**PIN（4 位数字）**

- 生成：`crypto/rand` ∈ [0, 9999]，格式化 `%04d`（前导零合法）。
- 生命周期：滚动有效——默认每 **10 分钟**自动换新，用户可随时「换一个」立即
  作废旧码。服务端同时只承认一个 PIN。
- 存放：仅内存 + 事件日志（只记哈希前缀，不记明文）；服务重启即换新（旧码
  自然失效，无需持久化）。

**配对端点**（挂在 IPP 同端口，无需认证；bootstrap 之外才有意义）

```
POST /pair
{"pin": "4831", "device_id": "<uuid>", "device_name": "小明的笔记本"}
→ 200 {"token": "<32 chars>", "server_name": "...",
       "printers": [{"key": "epson-l3250", "name": "办公室打印机",
                     "uri_path": "/p/<token>/ipp/print/epson-l3250"}]}
→ 401 {"error": "bad_pin", "remaining_attempts": 2}
→ 403 {"error": "locked", "retry_after_sec": 1800}
```

- `device_id`：接收端首次运行时生成、存 `<stateDir>/device-id`；同一 device
  重复配对 = **轮换**该设备的 token（旧 token 立即失效）。
- **限速（必须，4 位只有 10⁴ 组合）**：每源 IP 滑动窗口 10 次失败/10 分钟 →
  锁定该 IP 30 分钟；全局失败速率异常（如 100 次/分钟）打 `warn` 事件。锁定
  只封配对端点，不影响已配对设备正常打印。
- 成功后发 **128-bit token**（`crypto/rand` 16 字节，base32 无歧义字符集）。
  落盘 `paired-devices.json`：`[{id, name, token_hash, added_at, last_seen,
  source_ip}]`——**存哈希不存明文**（token 会出现在 URI 里，日志脱敏规则见
  §5.1；落盘明文会在备份/误传时泄露）。

**撤销**：管理页"已配对设备"列表逐个撤销（DELETE）→ 内存 + 落盘同时失效。
撤销后该设备的打印请求得到 401；接收端 doctor 检测到 401 会提示"已被服务端
撤销，请重新配对"（§5.4）。

### 4.3 数据面鉴权：token 进 URI 路径

**决策**：token 放在 IPP URI 路径里——`ipp://<host>:<port>/p/<token>/ipp/print[/key]`。

- **为什么是路径而不是 header**：Windows IPP Class Driver（接收端队列的实际
  发包者）不支持自定义 HTTP header；URI 路径是三平台系统打印栈唯一通用通道。
- 鉴权范围：**全部 IPP 操作**（含属性查询）。未配对来源收到
  `401 + WWW-Authenticate`（与 admin 鉴权风格一致）。
- **回环豁免**：来自 127.0.0.1 的请求免 token（与 ADR-0008 D2"回环全权"同一
  语义：本机用户已拥有机器）。旧 CLI `dyj print` 在服务端本机照常可用；从
  其他机器裸用旧 CLI 将开始收到 401——**Breaking**，CHANGELOG 标注。
- token 校验：URI 前缀 `/p/<token>/` 常数时间比较哈希；命中后 strip 前缀再走
  既有路由（`server.go:150` 的宽容路由不动）。
- 撤销/轮换后：旧路径 401。服务端记 `DATA-2 deny` 事件（§5.2）。
- mDNS TXT 增加标记（经 `BuildTXT` 的 `Extra` map，`mdns.go:772-841`）：
  `dyj=1`（dyj 共享）、`pair=1`（需配对）、`dyjver=3`。接收端扫描列表据此
  显示徽章。
- 明文 http 的风险（LAN 内嗅探可得 token）接受，与现状 admin LAN 明文同级；
  升级路径：`-tls-port` 的 ipps:// URI 同样带 `/p/<token>/` 前缀（P1 可选、
  P3 评估默认化）。

### 4.4 服务端多打印机模型（P2）

- `service.json` 结构演进：`printer: string` → `printers: [{key, name, queue,
  backend}]`；读入时旧字段自动迁移成单元素数组（迁移有单测，§7）。
- `key`（slug）：队列名转 `[a-z0-9-]`，冲突加序号；用于 URI 与 spool 子目录。
- IPP server：`map[key]*Printer` 多 profile（每台独立 `ipp.DefaultPrinter()`
  派生的属性/UUID/格式声明）；路由按尾段选 profile；**单打印机时保持
  `/ipp/print` 兼容旧路径**（老队列不断链）。
- mDNS：每台一个实例，实例名 `"<人读名> @ <主机>"`（CUPS 惯例）；Advertiser
  扩为多组 (instance, TXT)。名字冲突探测（`mdns.go:556-607`）逐实例执行。
- spooler：`<spool>/<key>/` 每台独立目录与 worker（物理打印机本就独立；
  现单 worker 串行是"单台打印机"约束，`spooler.go:443-470`）。
- 后端工厂：`backend.ForQueue(cfg) (driver.Interface, error)`，wingdi/cups
  按需多实例化；`file/null` 调试后端照旧。

### 4.5 Windows 本地打印机枚举（新增，P0）

- 实现：`EnumPrintersW(PRINTER_ENUM_LOCAL|PRINTER_ENUM_CONNECTIONS, Level 2)`
  → `PRINTER_INFO_2W`（`golang.org/x/sys/windows` syscall，零新依赖）；
  `GetDefaultPrinterW` 标默认。
- **虚拟打印机过滤规则**（默认隐藏，可一键显示全部）：

| 判据 | 规则 |
| --- | --- |
| 端口名 | `PORTPROMPT:`、`nul:`、`SHRFAX:` → 虚拟 |
| 打印机名 | 含 `OneNote`、等于 `Fax` → 虚拟 |
| 驱动名 | `Microsoft Print To PDF`、`Microsoft XPS Document Writer` → 虚拟 |

  （规则表驱动 + 单测；真实环境遇到新虚拟打印机靠"显示虚拟打印机"兜底，
  doctor 提示。）
- 探测：对每台选中打印机做 `OpenPrinterW` 试开（复用 wingdi 已有门面
  `wingdi_windows.go:57-65`），失败行内标红——把"共享了打不开的队列"挡在
  向导里（检查点 SHARE-2）。
- Linux/macOS 直接复用 `cups.Queues()`（`cups.go:132-162`，`lpstat -p`），
  天然无虚拟打印机问题。

### 4.6 接收端自动装系统队列

**Windows（`Add-Printer -IppUrl`，自动选 Microsoft IPP Class Driver）**

- **主路径（安装器场景，零弹窗）**：接收端机器装了 dyj（服务以 SYSTEM 常驻），
  向导的装队列请求走 admin API `POST /admin/api/queue/install`，由**服务进程
  代执行** `Add-Printer`——SYSTEM 权限足够，用户全程无感知。
- **绿色路径（无服务，弹一次 UAC）**：向导进程（非提权）直接 `Add-Printer`
  被拒（[调研 12](12-windows-acceptance-2026-09-30.md) §29 实测"需管理员"）→
  dyj 以 `ShellExecuteW(runas)` 重启自身一次性提权 helper
  （`dyj setup-helper --install-queue --json <spec>`，stdin 传参避免命令行
  泄露 token，stdout 回 JSON 结果）→ 完成。**一次 UAC，仅此一次。**
- 队列名 = 服务端打印机人读名（如"办公室打印机"）；重名自动加 ` (2)`。
- 装后验证：`Get-Printer -Name <队列名>` 存在即成功（检查点 USE-5）。
- **测试页**：完成页「打印测试页」= 经新队列提交内嵌的一页 PNG（go:embed，
  复用 spooler 正常链路）→ 端到端实证。

**URI 的 host 选择（健壮性关键决策）**

- 队列 URI 的 host **优先用 mDNS 主机名 `<name>.local`**（扫描时从
  `Found.Host` 获得，`browse.go:17-28`），回退 IP。服务端 DHCP 换 IP 后队列
  依然可用（Avahi/Win10+ 原生解析 .local）。
- `.local` 解析失败的兜底：doctor 检查项 + 接收端再次打开 dyj 时重新解析并
  **重建队列**（见下）。

**Linux/macOS（`lpadmin -m everywhere`）**

- 安装器场景同样由 root 服务代跑（deb 的 systemd / brew cask 的 launchd）：
  `lpadmin -p <name> -E -v ipp://<host>:<port>/p/<token>/ipp/print/<key> -m everywhere`。
- 绿色路径：先直接跑，`EPERM` 则按平台给 pkexec/sudo 一次性提权或复制命令
  指引（Linux 桌面环境差异大，不硬造弹窗）。

**记录、变更与卸载**

- 接收端落盘 `<stateDir>/added-printers.json`：
  `[{queue_name, server_host, server_port, uri_path, device_id, added_at}]`
  （token 不落此文件，仅在系统队列 URI 里）。这是接收端"我的打印机"管理页
  的数据源。
- 变更处理（服务端换 IP/端口/撤销）：接收端打开 dyj 时逐条校验（mDNS 重发现
  → 探活 → 试 Get-Printer-Attributes），失配则**删旧队列重建**（Windows
  `Remove-Printer` + `Add-Printer -IppUrl`；不尝试原地改端口属性，IPP URL 是
  端口对象的属性，改不如重建可靠），用户看到的是"已更新"。
- 卸载：管理页逐台「移除」→ 服务代跑 `Remove-Printer` / `lpadmin -x` + 清
  记录。

### 4.7 后台运行与安装器（用户已选：一次提权，之后全自动）

- **安装器（`dyj.iss`）改动**：
  1. `dyj service install` 改为写入 `role: ""` 的 bootstrap 配置（现状不带
     flags 的 install 正是天然 bootstrap，缺的是 serve 端不再硬失败）。
  2. 新增桌面快捷方式「dyj」→ `dyj open`。
  3. 防火墙（已有，`.iss:50-51`）、托盘自启（已有 HKCU）不变。
- 安装完成即：服务在跑（bootstrap）+ 托盘在 + 桌面有图标 → 用户点图标 →
  向导 → 之后全自动。**管理员权限只在安装器那一次。**
- 绿色路径：无参 `dyj` 前台跑（控制台窗 + 自动开浏览器 + 可配合 dyj-tray），
  向导完成页提示"安装 dyj 到本机可获得开机自启"。
- Linux deb/rpm、macOS brew：与 Windows 同构（bootstrap systemd/launchd +
  `.desktop` 菜单项指向 `dyj open`）。

### 4.8 admin API 与 UI 增量

| 增量 | 说明 |
| --- | --- |
| `GET /admin/api/local-printers` | 枚举本机打印机（§4.5），返回 `{name, driver, port, default, virtual, probe_ok, probe_err}` |
| `POST /pair` | §4.2，公开端点 |
| `GET /admin/api/pairing` | 当前 PIN + 剩余秒数（仅回环/写权限，PIN 不给 LAN 只读） |
| `POST /admin/api/pairing/rotate` | 立即换码 |
| `GET/DELETE /admin/api/devices` | 已配对设备列表/撤销 |
| `POST /admin/api/queue/install`、`DELETE .../queue/<name>` | 接收端装/删队列（服务代跑，§4.6） |
| `GET /admin/api/scan` | 接收端视角 mDNS 扫描（复用 `Browse`，SSE 增量可选） |
| UI 新页面 | `#/setup` 角色选择；`#/printers-local` 共享端勾选；`#/add` 接收端扫描/配对/安装；首页加配对码卡片与设备列表 |

组件粒度遵守 ADR-0008 D1 硬约定（一控件一文件、一页面一文件）。

## 5. 可观测性设计（自动化越多，这里越重要）

### 5.1 结构化事件日志规范

- 新包 `pkg/logx`：level（`debug|info|warn|error`）+ 事件名 + key=value 字段，
  **保持 logfmt 人类可读**（不引 JSON 行——家庭用户会直接打开日志文件看）：

```
2026-09-30T10:12:03+08:00 lvl=info  evt=mdns.announce ifname=以太网 host=dyj-lab.local printers=1 dur=4ms
2026-09-30T10:14:11+08:00 lvl=warn  evt=pair.attempt src=192.168.1.23 ok=false remaining=2
2026-09-30T10:14:14+08:00 lvl=info  evt=pair.ok src=192.168.1.23 device=9f3a12 name=小明的笔记本
2026-09-30T10:15:02+08:00 lvl=info  evt=queue.install qname=办公室打印机 rc=0 dur=812ms
2026-09-30T10:16:40+08:00 lvl=info  evt=job.recv job=000017 printer=epson-l3250 device=9f3a12 fmt=image/pwg bytes=1823441
```

- **字段字典**（固定语义）：`evt` 事件名；`src/dst` 地址；`printer` 打印机
  key；`qname` 系统队列名；`job` 作业号；`device` 设备 id 前 4 位；`ifname`
  网卡名；`rc` 子进程退出码；`dur` 耗时；`err` 错误串。
- **脱敏铁律**：token 明文、PIN 明文、完整 URI 带 token 的路径**永不入日志**
  （只记 `/p/****/...`）；`pair.attempt` 不记用户输的码。
- 事件命名：`<域>.<动作>`，域 = `boot|cfg|share|mdns|pair|queue|scan|job|deny|doctor`。

### 5.2 检查点清单（每个自动化步骤的前置/后置断言）

实施时每行落为：断言代码 + 事件 + 失败的用户可见反馈。**这张表是"自动化不出
错则已，出错必能定位"的契约。**

| ID | 阶段 | 断言 | 失败处理（用户可见） | 事件 |
| --- | --- | --- | --- | --- |
| BOOT-1 | bootstrap | 端口监听成功（或递增找到空闲） | 向导页顶红条"端口被占，已用 NNNN" | `boot.listen` |
| BOOT-2 | bootstrap | `GET /admin/api/status` 自检 200 | 理论不可达（自环），error 级 | `boot.ready` |
| CFG-1 | 配置应用 | service.json 写入成功（原子替换） | "保存失败：磁盘/权限" + doctor | `cfg.save` |
| CFG-2 | 进程内重建 | 拆除完成（mDNS goodbye 发出、spool drain ≤30s） | "有作业未打完，稍后自动重试" | `cfg.rebuild` |
| SHARE-1 | 共享向导 | 枚举到 ≥1 台（过滤后为 0 且全被隐藏 → 提示显示虚拟） | 空列表文案 + 「显示虚拟打印机」 | `share.enumerate`（含 total/visible/virtual 计数） |
| SHARE-2 | 共享向导 | 每台选中 `OpenPrinterW` 试开成功 | 行内红字"无法访问：<err>"，可取消勾选 | `share.probe`（逐台） |
| SHARE-3 | 共享向导 | 后端实例创建成功（wingdi DEVMODE 读到） | 行内红字 + doctor 链接 | `share.backend` |
| SHARE-4 | 服务启动 | mDNS 每接口宣告成功（逐接口事件） | "mDNS 未在 <ifname> 宣告：可能防火墙拦 UDP 5355" + doctor | `mdns.announce`（逐 ifname） |
| SHARE-5 | 服务启动 | 名字冲突探测无冲突 | 自动加序号重宣告（不问用户） | `mdns.conflict` |
| SHARE-6 | 服务启动 | 回环 Get-Printer-Attributes 自检 200 | warn：IPP 端点异常 | `share.selftest` |
| PAIR-1 | 配对码 | PIN 生成/轮换/过期 | 无（后台） | `pair.pin.rotate` |
| PAIR-2 | 配对 | 每次尝试记 src/结果/剩余次数 | 输入框下"配对码不对，还剩 N 次" | `pair.attempt` |
| PAIR-3 | 配对 | 锁定/解锁 | "尝试过多，请 M 分钟后再试" | `pair.lock` / `pair.unlock` |
| PAIR-4 | 配对 | 成功发 token、写 paired-devices.json | — | `pair.ok` |
| USE-1 | 接收向导 | 扫描结果（数量、来源 lib/raw 双路一致性） | 0 结果 → "检查两台电脑是否同一网络" + doctor | `scan.result` |
| USE-2 | 接收向导 | 候选 TCP 探活（记 rt） | 列表灰显"不可达" | `scan.probe` |
| USE-3 | 接收向导 | 配对请求成功（客户端侧） | 复用 PAIR-2 文案 | `pair.req` / `pair.ok`（客户端侧） |
| USE-4 | 装队列 | Add-Printer/lpadmin 退出码 0，stdout/stderr 摘要入日志 | "安装失败：已复制诊断命令" + UAC 重试入口 | `queue.install`（rc、dur、out 摘要） |
| USE-5 | 装队列 | 装后 `Get-Printer` 验证存在 | "系统未出现该队列"→重试一次→doctor | `queue.verify` |
| USE-6 | 测试页 | 测试页作业进入 completed | "测试页未打出：查看服务端状态" | `job.testpage` |
| DATA-1 | 数据面 | 每作业 lifecycle：recv→spool→render→print→done/fail | 管理页作业列表（已有）+ fail 摘要 | `job.recv/spool/render/print/done/fail` |
| DATA-2 | 数据面 | 未授权请求拒绝（401）计数 | 服务端（无感）；接收端 doctor 可查 | `deny.ipp`（src、脱敏 path） |

### 5.3 文件日志

- 路径：`<stateDir>/logs/dyj.log`，**INFO 起步**，`-v` 开 DEBUG；按 10 MB 轮转
  保留 3 份（`dyj.log.1/2/3`）。服务模式双写 stderr（journald/SCM 保留现状）
  + 文件；绿色模式同。这补上"服务模式下 stderr 不可得/不可搜"的最大盲区
  （现 LogRing 只有 500 行滚动窗口，`admin.go:426-500`）。
- admin 日志页（SSE 直播）不变，另加"打开日志文件位置"提示。

### 5.4 doctor 扩展（接收端/配对视角，P1–P3）

| 新检查 | 断言 | 失败时的 Fix 文案 |
| --- | --- | --- |
| `client-queue-exists` | added-printers 里每台在系统队列中存在 | "队列丢失，点此重装" |
| `client-uri-reachable` | 队列 URI TCP 可达 | "服务端不在线/换了地址，点此重新扫描" |
| `client-token-valid` | Get-Printer-Attributes 非 401 | "配对已被撤销，请重新配对（需服务端配对码）" |
| `client-mdns-resolve` | `<host>.local` 可解析 | "回退为 IP 直连并重建队列" |
| `server-mdns-egress` | 自发自收一次 `_ipp._tcp` 查询能收到自己应答 | "多网卡出口异常，附 netsh/iface 指引"（防 multi-NIC bug 复发，正是 [调研 12] 修的那类） |
| `job-stuck` | spooler 无 pending > 10 分钟的作业 | "重启共享端服务" |

CLI：`dyj doctor`（服务端视角，现状）与 `dyj doctor --client`（接收端视角）
共用 Check 结构与 UI 渲染（`dyj-diagnostic.js` 已有 level 徽标 + Fix 复制）。

### 5.5 admin UI 可观测增量

- 首页配对码卡片（§3.4）+ 已配对设备列表（名/IP/添加时间/最近打印/撤销）。
- 向导各步骤内嵌诊断（现状 `page-wizard.js` 已有骨架）扩展到新页面。
- 状态页加"最近拒绝"计数（`deny.ipp` 聚合）——主人能发现有人试图蹭打。

### 5.6 故障树（症状 → 定位 → 处理）

调试手册按症状组织；每条给出：看哪个事件（服务端/接收端各一行）、常见原因、
处理。实施时随功能同步写进 `docs/troubleshooting.md` 并被 doctor Fix 文案链接。

| 症状 | 先看 | 常见原因 → 处理 |
| --- | --- | --- |
| 接收端扫不到任何打印机 | 接收端 `scan.result`；服务端 `mdns.announce` | ① 子网/AP 隔离 → 换网络；② 服务端防火墙拦 UDP 5353 → doctor `server-mdns-egress` 给 netsh；③ 服务端没起 → 开共享端 dyj |
| 扫到 dyj 机但列表灰"不可达" | `scan.probe`（rt/err） | 服务端防火墙拦 TCP 6631 → doctor 防火墙项（现已有指引） |
| 配对码总不对 | 服务端 `pair.attempt`、`pair.pin.rotate` | ① 码刚轮换 → 服务端刷新显示；② IP 被锁 → 等 `retry_after`；③ 输错 → 剩余次数提示 |
| 配对成功但装队列失败 | `queue.install` rc/out | ① UAC 被拒 → 重试一次；② WPPM 机器策略 → 见 §8-Q3；③ 重名冲突 → 自动加 ` (2)` 后重试 |
| 装好了，WPS 打印没反应 | 接收端 `client-uri-reachable`/`client-token-valid`；服务端 `deny.ipp`/`job.recv` | ① 服务端 IP 变且 .local 解析失败 → 重扫重建；② token 被撤销 → 重新配对；③ 服务端暂停共享 → 管理页恢复；④ 作业在打但慢 → 管理页作业列表 |
| 打出来乱码/空白 | `job.render`/`job.print` | 格式声明与实际不符 → ADR-0007 诚实声明语义，按事件里的 fmt 缩小 |
| 共享端自己打不出向导 | `boot.listen` | 6631 被旧实例占 → bootstrap 已自动换端口/复用旧实例，看 banner |
| 服务重启后客户端全失效 | `pair.ok` 时间 vs 重启时间 | PIN 重启即换（设计使然），token 不变；若 token 失效 → `deny.ipp` + `client-token-valid` 走重新配对 |

## 6. 阶段划分与验收标准

每阶段独立可交付、可验收；P0 最小到"共享端傻瓜化闭环"。

### P0：引导与共享端（修复首启死锁 + 角色向导 + 自动枚举 + 一键共享）

- 无参状态机 + bootstrap 模式 + `dyj open`；serve 移除 wingdi 缺 printer 的
  硬失败（降级为"未配置后端"警告 + admin 可用，`serve.go:320-323`）。
- `pkg/logx`（level + key=value 事件 + 文件日志轮转）落地，BOOT/CFG/SHARE
  检查点接入。
- `GET /admin/api/local-printers`（EnumPrintersW + 过滤 + 逐台探测）。
- `#/setup` + 共享端勾选页 + 进程内重建（配置热生效）。
- 安装器改 bootstrap install + 桌面快捷方式。
- **验收**：全新 Windows 机器装安装器 → 全程零 CLI → 浏览器向导 → 勾选真实
  打印机 → 另一台机器 `dyj print`/系统添加成功；`service.json` 手工写坏、
  打印机被拔掉、6631 被占三类故障均有清晰提示与日志（对照 §5.2 逐条核）。

### P1：配对与接收端（4 位码 + 自动装队列 + 测试页）

- `/pair` + PIN 生命周期 + 限速锁 + `paired-devices.json` + 撤销。
- 数据面 `/p/<token>/` 鉴权 + 回环豁免 + `deny.ipp` 事件（**Breaking**：LAN
  无 token 旧客户端开始被拒）。
- TXT `dyj=1/pair=1/dyjver=3`；接收端 `#/add` 扫描（含非 dyj 打印机）→ 配对
  → 服务代装队列（Windows 主路径零弹窗、绿色一次 UAC；Linux/macOS lpadmin）
  → 装后验证 → 测试页。
- `added-printers.json` + 接收端管理页 + 重扫重建 + doctor `--client` 首批
  检查。
- **验收**：接收端全 GUI ≤4 次点击完成且 WPS 打出测试页；第三台未配对机器
  打印被拒且 doctor 给出指引；撤销后接收端 doctor 提示重新配对；配对暴力
  尝试被锁定（自动测试）。

### P2：多打印机

- §4.4 全量：`printers[]` 迁移、多 profile 路由、mDNS 多实例、per-printer
  spooler、后端工厂；向导多选共享。
- **验收**：共享 2+ 台（其一用 NUL 无纸队列模拟）各自独立打印/取消；旧单
  打印机 `/ipp/print` 路径回归不破坏；旧 `service.json` 迁移单测。

### P3：可观测性完善与自愈

- 检查点表全量覆盖（补齐 USE/DATA 全部事件）、admin 设备管理完善、"最近拒绝"
  聚合、`server-mdns-egress` 自测、故障树文档 `docs/troubleshooting.md` 定稿
  并与 doctor Fix 互链。
- 评估 ipps:// 默认化（token 过 URI 在 TLS 下的暴露面收窄）。
- **验收**：按故障树逐条人为制造故障，全部能凭事件日志 + doctor 在 5 分钟内
  定位（演练记录追加为调研 14/附录 H）。

## 7. 测试计划

- **单元（表驱动，标准库风格）**：PIN 生成/轮换/过期（假时钟）；限速窗口与
  锁定；token 哈希校验与常数时间比较；路径路由（`/p/<tok>/ipp/print[/key]`、
  旧 `/ipp/print`、未知 key）；EnumPrinters 过滤规则表；`printer → printers[]`
  配置迁移；日志脱敏（token/PIN 永不出现）。
- **HTTP**：`/pair` httptest 全分支（200/401/403/重放/轮换后旧 token 401）；
  回环豁免判定；`queue/install` 的 helper 协议（stdin spec → stdout JSON）。
- **进程内 e2e（`e2e/` 扩展）**：share → pair → 带 token IPP 200 → 无 token
  401 → 撤销 → 401 → 重新配对 → 200；多打印机路由（P2）。
- **真机验收（延续 [调研 12] 方法论）**：Windows 物理机 + NUL 无纸队列；
  `Add-Printer -IppUrl` 真实提权路径；WPS 实打印测试页；多网卡机器复测
  mDNS（egress 修复的回归）。
- **wire 不变说明**：TXT 新键走 `BuildTXT` 单测即可（mDNS TXT 非 IPP wire，
  不触发黄金字节要求）；`pkg/attributes` 零改动。

## 8. 风险与开放问题

- **Q1 SCM 主动退出码是否触发 recovery action**：进程内重建是主方案、SCM
  重启仅兜底，故此问题只影响兜底路径；P0 实测一次并记录。
- **Q2 `.local` 主机名跨平台解析可靠性**（尤其 Windows 对 mDNS 名的解析时延
  与失败率）：P1 真机验证；回退 IP + doctor `client-mdns-resolve`。
- **Q3 WPPM（Windows Protected Print Mode）机器上 `Add-Printer -IppUrl` 的
  行为**：调研 P0 #8（research README）仍未闭环；列为 P1 真机验收项，若被
  策略拦截则故障树给出指引。
- **Q4 4 位 PIN 的安全边界**：限速下爆破需 ~10⁴ 请求/10 窗口，家庭/小办公
  "防蹭打"足够；**明确不是企业安全边界**。升级路径（6 位码/一次性配对开窗）
  预留配置项但不进 UI。
- **Q5 token 暴露面**：系统队列属性、Spooler 日志可能含带 token 的 URI；
  缓解 = 撤销机制 + 日志脱敏 + LAN 信任域假设（与打印机明文共享同级）。
- **Q6 Breaking 兼容**：LAN 上无 token 的旧 dyj 客户端/脚本在 P1 后被拒
  （回环不受影响）——CHANGELOG `Changed` 显式标注；`dyj print` 加
  `--token`/自动读取本机配对记录作为过渡。
- **Q7 双角色 both 的交互**：同机共享+接收的 UI 导航与事件命名（share/use
  域并列）在 P1 设计时细化，先保证两角色可先后独立配置。
- **Q8 接收端不常驻的代价**：服务端离线时用户只在 WPS 里看到"脱机"，没有
  主动提醒；若真实使用反馈强烈，P3 后评估可选的轻量托盘（仅状态展示）。

## 9. 与既有 ADR 的关系

- 继承不变：ADR-0004（驱动栈复用）、0007（诚实声明）、0008 D1（Web UI 栈与
  组件粒度硬约定）、0008 D2（回环全权——本设计的回环豁免即其延伸）。
- 修订：ADR-0008 D2 的"IPP 数据面不设防"事实被本设计（数据面 token）收紧；
  ADR-0009 打装链路扩展 bootstrap 语义。
- 新决策（角色向导/配对/装队列/热生效/多打印机/可观测规范）全部收敛进
  [ADR-0010](../adr/0010-role-wizard-pairing.md)。
