# Changelog

All notable changes to this project are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/); versioning: semver.

## [0.7.1] — 2026-11-14

### Fixed
- **Win7 旧版通道 CI 编译修复**：`alias_test.go` 用了 `t.Context()`
  （Go 1.24+ API），旧版通道 go1.20.14 工具链不识别，导致 v0.7.0 发布
  流水线 build-legacy 失败、安装包未能发布。改用 `context.Background()`，
  测试行为不变。

## [0.7.0] — 2026-11-14

### Fixed
- **平板"按 IP 添加"终于可用**（[ADR-0017](docs/adr/0017-ipp-alias-reinstated.md)）：
  鸿蒙等系统打印对话框隐含 631 且无端口输入（research/17 §4 真机实证），
  而服务只听 6631——开放打印免的是 401，救不了建连失败。恢复 631 IPP
  别名监听（仅数据面，`aliasGuard` 挡住 /admin 与 /pair；被占自动跳过），
  配置热更新同步镜像路由，停机一并关闭。防火墙自检/一键修复/安装器同步
  放行 TCP 631（规则名 `dyj IPP Alias`）。
- **服务日志不再整体静默**：`io.MultiWriter` 首个写入器出错即中断后续全部
  写入——现场实测服务运行中日志环与文件双双为空。新增 `logx.Fanout`
  容错多路写入：单个输出失败不影响其余输出（回归测试钉死）。
- **升级不再丢配置**：安装器每次执行 `dyj service install`（无参数）此前
  无条件覆盖 service.json——升级一次，共享清单与常开免认证全部被重置。
  现在无显式参数且已有有效配置时保留原配置，只修复服务注册
  （`SaveOptionsIfAbsent`，损坏配置仍可被引导修复）。

### Added
- **防呆护栏**：虚拟打印机（PDF/XPS 等）向导默认不勾选、列表加"虚拟"徽章，
  全选虚拟一键共享前红色确认；关闭最后一台共享前确认（此前静默退回挑选态）；
  免认证打印开/关均先确认；客户端配对码输入自动滤除非数字。
- **平板/手机连接指引**：服务端主页常驻提示条（本机地址 + 免认证状态实时
  刷新）；帮助页新增平板排障 FAQ（含 HarmonyOS 7.0 升级建议）；关于区显示
  兼容端口。配对码点击即复制。
- **界面美化**：渐变主色按钮/品牌图标、分层阴影与大圆角卡片、柔和背景光晕、
  模态入场动效（尊重系统"减少动态效果"）、列表行悬浮反馈、统一焦点环。

### Changed
- 状态接口新增 `ipp_alias_port` 与每台打印机 `virtual` 标记（均可选字段，
  旧客户端不受影响）。

## [0.6.0] — 2026-11-13

### Fixed
- **启动端口竞态**（真机跨机测试发现，research/16）：`runNode` 先构建共享
  （把端口烤进打印机 URI 与 mDNS 通告）、后绑定端口，首次启动端口=0 →
  `discovery: invalid port 0`、URI 全是 `:0`、局域网发现不到。现把端口绑定
  提前到 buildShare 之前；回归测试 `TestBootPrinterURIsHaveRealPort` 钉死。
- **开放打印模式真正可用**（[ADR-0015](docs/adr/0015-discovery-hardening-and-open-txt.md)）：
  此前门禁虽放行，但 mDNS TXT 恒为 `pair=1`，自家客户端仍强制弹配对码框，
  免配对分支永远走不到。现在 `pair` 标记随开放状态实时生成（开放=`0`），
  开关/到期/常开切换都会立即重宣告；窗口到期挂定时器回落 TXT。
- **常开标志不再被配置保存清掉**：向导 PUT 不带 `open_print` 键时保留现值
  （此前整体替换 cfg 会静默丢失常开）；前端开放操作后同步配置快照，双保险。
- **打印机列表正确性**：Windows 枚举失败不再吞成空列表（区分「真无打印机」
  与「Spooler 停用」）；纯中文名打印机第三台不再撞键（`printer`/`-2`/`-3`
  唯一化），且配置保存继承既有共享键，已配对设备 URI 不断；扫描合并键改用
  UUID 优先，两台电脑共享同名打印机不再被吞成一行。
- **已装列表提速且不再闪空**：系统队列查询批量化（一次 Get-Printer 替代逐台
  起 PowerShell），IPP 探测并行 + 10s 缓存，总预算 2.5s；刷新失败保留旧列表。

### Added
- **Linux avahi 共存**（[ADR-0016](docs/adr/0016-avahi-dbus-coexistence.md)）：
  装有 avahi 的 Linux 服务端（CUPS 桌面发行版默认）此前 dyj 自管 mDNS 收不到
  查询（SO_REUSEPORT 组播只投递给一个 socket，被 avahi 独占），自动发现失效。
  现在检测到 avahi 时经其 D-Bus API 发布服务（EntryGroup/AddService/
  UpdateServiceTxt），无 avahi 自动回退自管。开放模式 pair TXT 经 avahi 实时
  翻转。打包单元改用静态 dyj 用户（DynamicUser 临时 UID 被 dbus 凭据校验拒绝）
  并只读绑定 /run/dbus。godbus 提为直接依赖（原已随 systray 引入）。
- **局域网发现极致加强**（[ADR-0015](docs/adr/0015-discovery-hardening-and-open-txt.md)）：
  查询多轮退避重试 + 缺失记录定向补查；查询套接字加入组播组（收得到纯组播
  应答的第三方设备）；结果校验并行化；修复冲突探测 `timeAfter(220)` 实为
  220 纳秒的 bug；恢复 RFC 6762 §8.3 的 TTL/2 周期重宣告；放宽浏览接口筛选
  （mesh/虚拟网卡可扫）；可达探测并行并返回真实 RTT；离线条目 90s TTL 治理；
  扫描结果服务端缓存 10s + 单飞，客户端页可见态 20s 自动重扫。
- **跨网段深度扫描**（research/15 P3 落地）：客户端页「深度扫描」按钮，按
  各网卡 /24 + 可选 CIDR 逐个地址探测，6631 为主、显式网段补 631；TCP 快筛
  后必须 IPP 确认（防 mesh 假握手）；后台任务 + 单飞，仅手动触发。
- 服务端主页每 30s 重拉本机打印机列表，新插/新装打印机不用切页即出现。

### Notes
- IPv6 发现本期不做（现链路全 IPv4，双栈涉三平台 socket/zone 语义，稳定优先）。
- 全部新代码 go1.20 可编译（Win7 legacy 通道）；IPP wire 未动，黄金字节测试原样护航。

## [0.5.0] — 2026-11-09

### Added
- **开放打印模式**（[ADR-0014](docs/adr/0014-open-printing-mode.md)）：管理页
  服务端页新增「免认证打印」区——开启后局域网内任何设备无需配对码即可直接
  IPP 打印。默认「开 10 分钟」自动关闭（内存态，重启即清，可随时关闭）；「一直
  开启」需经风险确认弹窗，持久化到 service.json 并在界面上常驻「有风险」标识。
  `POST/GET/DELETE /admin/api/open`（管理面鉴权不变，开放只作用于数据面），
  `GET /admin/api/status` 透出 `open_printing`/`open_print_always`/
  `open_remain_sec`；数据面放行记 `allow.ipp` 审计日志。
- **自检自修复**：帮助页自检新增「自动修复」。防火墙检查从固定提示升级为
  真实探测（按本机规则的放行端口与程序路径逐项核对，缺失时标为「需注意」
  并给出修复按钮）；可修复项（防火墙放行、加密证书重建、状态目录、PDF 组件
  安装）逐项显示「自动修复」，另有「一键修复」批量执行。`POST
  /admin/api/repair`（沿用管理面鉴权；防火墙修复经一次管理员确认弹窗），
  命令行同等能力：`dyj doctor --fix <检查名|all>`。修复结果以复查为准，
  修复后自动重跑自检。

### Removed
- **鸿蒙方案整体撤回**（[ADR-0014](docs/adr/0014-open-printing-mode.md)）：删除
  `harmony/` 接收端 App；回退「配对即信任设备 IP」（`pairing.TrustedIP`）；
  回退 631 端口别名监听与 mDNS TTL/2 周期重宣告（均未随任何版本发布过）。
  免认证打印改由通用的「开放打印模式」承担（见上）。`pkg/client` 的黄金字节
  wire 锁定保留。

### Changed
- **官网（dyj.qdzwwqd.top）视觉重构**：深蓝渐变模板（渐变大字标题 + emoji 图标 +
  圆角浮卡 + pill 徽章）改为浅色克制工具风——白底墨字 + 单一强调色 `#2563eb`
  （与管理页 `--dyj-primary` 同源）；全站图标改内联 SVG：品牌位用
  `assets/brand/dyj-mark.svg` 的「打印机 + 广播波纹」母题，界面图标用 Lucide
  图形（ISC，research/11 §3）；信息架构收敛为 Hero / 三步 / 能力 / 下载 /
  快速开始五段；深色模式重做为近黑中性色（原深蓝版只是浅色的反面）。
  `website/`（Worker + 部署脚本）自此纳入版本控制。

## [0.4.4] — 2026-11-07

### Added
- **Windows 7 SP1 legacy 通道**：发布页新增 `dyj-legacy-win7-{386,amd64}.zip`
  （Go 1.20.14 构建，最低 Win7 SP1；zip 内含 install/uninstall 脚本与
  README-Win7.txt）。仅支持「打印服务器」角色——driverless 客户端需要
  Win8+/Win10 21H2+（Windows 能力边界）。XP/Vista 不支持（Go 运行时层面
  不可行，Go ≥1.11 无法在 XP 运行）。
- windows/386 构建（Win10 32 位；Inno 安装器仍拒绝 32 位系统，32 位用户
  取裸 exe 或 legacy zip）。
- 低于 Win7 SP1（6.1 SP1）启动时给出友好提示后退出（internal/winver，
  ntdll RtlGetVersion），防 legacy 包被误装到 XP/Vista 时以运行时崩溃收场。

### Changed
- spooler prune 重写为单趟 `slices.DeleteFunc`：去掉 O(n²) 索引删除，
  顺手清理 order/jobs 失同步的僵尸项，RemoveAll 移到解锁后执行。
- slices/cmp 与 min/max 经 `internal/slices`/`internal/cmp` 垫片适配
  go1.20 legacy 工具链——现代构建仍直连标准库，`go.mod` 保持 go 1.27.3。
- release workflow 新增 build-legacy job（go1.20.14 × windows 386/amd64
  便携 zip，独立 artifact 随 Release 发布）。

## [0.4.3] — 2026-10-30

### Fixed
- **Linux/macOS 装包后服务起不来（deb/rpm 安装后点图标没反应）**: 打包的
  systemd/launchd 单元直接运行 `dyj service run`，而全新安装时
  `service.json` 还不存在（Windows 安装器会跑 `dyj service install`，
  deb/rpm 的 postinstall 没有），服务每 5 秒崩溃重启，6631 永远不监听，
  桌面图标（`dyj open` → 管理页）自然毫无反应。`service run` 现在对
  缺失的 options 文件回退为引导默认配置（`{}`：向导角色 + 标准端口），
  服务正常起来后由浏览器向导完成并持久化真实配置——与裸运行 `dyj`
  的"首次运行=引导"契约一致。读取失败的其它错误仍然致命，避免覆盖
  真正的机器状态问题。三个平台（SCM/systemd/launchd）共用此路径。

## [0.4.2] — 2026-10-17

### Added
- **Known-printer rediscovery (research/15 P2)**: opening the client page
  probes every previously added share address with IPP Get-Printer-Attributes
  (not a TCP dial — mesh/VPN overlays complete a handshake on ports that have
  no listener) and pins the ones that still answer at the top of the scan
  list, marked "以前添加过". A revoked pairing stays visible and asks for a
  new code; a live token path can be reinstalled directly. Logged as
  `scan.known`.
- Installer finish page offers a checked-by-default "立即打开 dyj" (`dyj open`).
  `dyj open` retries for a few seconds so it still finds a service that has
  only just started.

### Fixed
- **Pairing code boxes rendered empty**: `dyj-pin-input` called `t()` in its
  constructor without importing it. The custom element upgrade threw, the
  shadow tree stayed empty, and the dialog looked blank. Same failure class
  as the old `dyj-diagnostic` constructor throw; it was not specific to an
  embedded browser.
- Client scan no longer answers 502 with the text "mDNS scan failed" when
  browsing errors. That string hid the manual-address form and leaked a
  banned word onto the client page. An empty list (plus the address form)
  is the mesh/VPN path; the error stays in the `scan.result` log.
- A scan error in the page still shows the address form and a rescan button.
- Receiver status (`added-printers`) and `dyj doctor --client` judge the
  share side by an IPP exchange. A TCP-open/application-silent peer is
  offline, not "ok" / "可达".
- Recent-prints "copies" no longer shows 0 when the job omitted the
  attribute. The IPP default is 1 (research/12 §3).
- PIN remaining time uses the same clock as PIN rotation, so an injected
  clock cannot report a negative TTL.

### Changed
- Go module path and packaging homepage/download URLs are
  `github.com/232252/dyj`, matching the repository. Historical research
  notes that record the original Windows profile path are unchanged.

## [0.4.1] — 2026-09-30

### Added
- **Add a printer by address (research/15 P1)**: the client page has a manual
  entry block below the scan list (also in the empty state) — type the other
  computer's address (`192.168.1.20:6631`, bare host defaults to 6631, `[IPv6]`
  accepted) plus the optional 4-digit pairing code. This is the only way to add
  a printer when the two machines are connected only via a mesh/VPN overlay:
  mDNS is link-local and can never cross subnets, but pairing and printing are
  plain unicast. The queue URI echoes the address you entered (pair proxy
  already did; now reachable from the UI).
- Wrong-PIN attempts during pairing no longer trigger the admin-token banner
  (the share side's 401 passthrough was indistinguishable from a stale
  admin token; `pair` is now excluded from the 401 broadcast).

### Fixed
- **Scan listed one printer per network interface (research/15 D-1)**: the raw
  and reference mDNS browsing paths report the same service under different
  per-interface SRV targets, and the dedupe key included that host — one
  physical printer showed up as 2+ rows. The merge key is now instance label +
  port, and both views' addresses are unioned before the reachability probe.
- **Pairing dialog auto-connect no longer depends on the PIN component's
  value setter** (`connect(code)` takes the code directly; the `dyj-complete`
  event no longer passes the event object into the connect handler).

## [0.4.0] — 2026-09-30

### Changed
- **Minimal two-mode UI (v0.4, ADR-0011 / research/14)**: the admin console is
  gone — the whole product is now 4 routes with no sidebar (`#/` role cards,
  `#/server`, `#/client`, `#/help`); a configured machine lands directly on its
  role's home page (server: PIN + per-printer share switches + connected
  devices + an 8-row recent-prints block; client: scan → 4-digit code modal →
  install → installed list with live per-printer status). Old v0.3 hashes
  redirect. The network page (port/TLS/mDNS/hostname/ifaces), the printer
  management page, the URI copy fields, the per-platform manual setup guides
  and the metrics cards are all REMOVED from the UI — ports, drivers, queue
  names and connection hosts are automated behavior now (research/14 §4);
  CLI flags and service.json remain the only "advanced" surface.
- **Copy rules enforced by a test**: a banned-words guard scans both i18n
  tables; engineering jargon may only appear under `help_`/`about_`/`token_`
  keys (the help page is the single read-only home for technical info: doctor
  self-check, log tail, about line).
- **Tray menu follows the role**: pairing code and pause/resume only appear
  on machines configured as share/both; receiver machines see open/autostart/quit.

### Added
- **`GET /admin/api/diag/export`** (help page button): one zip with summary,
  live config (no secrets), doctor checks (share + receiver sides) and the
  256 KB log tail — send one file, get a diagnosis.
- **`GET /admin/api/added-printers` now carries a per-printer `status`**
  (`ok` / `offline` / `missing` / `revoked`), a read-only receiver snapshot
  for the client home list.
- **`GET /admin/api/jobs?limit=N`** returns the newest N jobs (newest first)
  for the server home "recent prints" block.
- **4-digit PIN input component** (auto-advance, backspace, whole-code paste,
  one-time-code autocomplete) behind the client pairing dialog.

## [0.3.0] — 2026-09-30

### Added
- **Zero-config first run (ADR-0010 D1/D2, research/13)**: `dyj` with no
  arguments opens a browser wizard with two roles — "share local printers"
  (auto-enumerated via EnumPrintersW/lpstat, virtual writers hidden, one
  click shares the selection) and "add a network printer" (LAN scan, 4-digit
  pairing, automatic system-queue install, one-click test page). The node
  ALWAYS boots — an unconfigured machine serves the wizard instead of dying
  on a missing -printer (the v0.2 first-boot deadlock), ports probe upward
  when taken, and `dyj open` (the installer desktop shortcut) finds the
  running node.
- **4-digit PIN pairing with device tokens (D3)**: rolling 10-minute PIN
  (admin dashboard, tray menu, console banner), per-IP rate limiting with
  lockout, constant-time compare, 128-bit tokens persisted only as SHA-256
  hashes, per-device revoke. Data plane: loopback exempt, LAN default-deny;
  tokens ride in the IPP URI path (`/p/<token>/ipp/print/<key>`) because the
  Windows IPP Class Driver cannot send custom headers. mDNS TXT gains
  `dyj=1`/`pair=1`/`dyjver=3`.
- **Receiver-side automatic queue install (D4)**: `Add-Printer -IppUrl` on
  Windows with automatic name dedup and a one-prompt UAC path (the wizard
  relaunches this binary via runas and picks up the result file); `lpadmin
  -m everywhere` on Linux/macOS. An added-printers registry
  (`added-printers.json`) drives the manage/remove UI and client-side
  doctor.
- **Multi-printer sharing (D6)**: service.json grows `role` + `printers[]`
  (v0.2 configs migrate on load); each printer gets its own IPP profile,
  spooler and mDNS instance behind one listener; legacy single-printer
  paths keep working.
- **In-process config hot-reload (D5)**: saving config in the admin UI
  rebuilds the share stack (mDNS goodbye → spool drain → new backends)
  without closing the listener — `restart_required` is gone.
- **Observability contract (D7)**: `pkg/logx` structured events
  (logfmt `lvl/evt/kv`), rotating file logs under `<stateDir>/logs/` with
  token/PIN redaction enforced in tests, the 24-checkpoint event set from
  research/13 §5.2, doctor additions — mDNS-egress self-test (share side)
  and `dyj doctor --client` (queue exists / server reachable / token
  validity) — plus `docs/troubleshooting.md`, the symptom → event → fix
  fault tree.
- **Pairing management UI**: dashboard pairing-code card with countdown and
  rotate, paired-device list with revoke; share wizard and add-printer
  wizard pages; first-run redirect keyed off the server-side role.
- **Branded tray gains the pairing code** as a live menu item.
- **Brand identity** (docs/research/11): self-drawn vector mark (printer +
  broadcast waves, `assets/brand/dyj-mark.svg`) and full-color app tile
  (`assets/brand/dyj-logo.svg`); favicon (SVG + 4 KB multi-size ICO) served
  from the embedded admin UI. `dyj-icon` now vendors 23 icons from Lucide
  v1.48.0 (ISC) with a provenance header — nav semantics fixed (status →
  activity, network → topology graph, jobs → files).
- **Branded Windows tray icon**: the 32×32 tray icon is now the blue brand
  tile with a white printer glyph plus a status badge (green/gray/red),
  rasterized at runtime from signed-distance fields — still zero asset
  files, with a theme-aware badge rim for light/dark taskbars
  (`AppsUseLightTheme` polled by the Windows glue).
- **Windows exe resources**: dyj.exe / dyj-tray.exe embed a multi-size ICO
  (16–256), version info (product name/version/copyright) and an app
  manifest via committed `rsrc_windows_*.syso` (go-winres) — Explorer, the
  taskbar and the uninstaller no longer show the default Go icon.
  `scripts/build-all.sh` restamps resources when go-winres + VERSION are
  available; the release workflow installs go-winres.
- **Linux desktop icon**: dyj hicolor icons (16–256 + scalable SVG) ship in
  the deb/rpm; the `.desktop` entry uses `Icon=dyj` instead of the generic
  theme printer icon. `scripts/genicons` regenerates every raster
  derivative from the 512 px master render.

### Changed
- **LAN data plane requires pairing (breaking)**: IPP requests from
  non-loopback peers without a valid device token receive 401 (v0.2 served
  anyone). Loopback keeps full access; the wizard is the supported path for
  LAN clients.
- **`service install` without flags persists a bootstrap config** (role "")
  instead of options that fail to boot; the installer's argument-less
  install now lands in the reachable wizard state.
- **Admin config saves apply live**; the UI no longer claims a restart is
  required.
- **Windows installer** adds a desktop "dyj" shortcut (`dyj open`) and a
  start-menu setup entry alongside the tray.

### Fixed
- **serve crashed at startup with a non-ASCII machine name** when
  `-tls-port` was set: the self-signed certificate's DNS SAN folded the
  host name to `生命.local`, which x509 rejects (DNS names are IA5String);
  host names are now folded to `[a-z0-9-]` with a `dyj` fallback, and the
  `-hostname` override feeds the certificate too (regression test with a
  CJK host name included; found in Windows physical-machine acceptance).
- **mDNS announcements never left the wire on multi-homed Windows** (and
  could mis-route on multi-homed Linux): per-interface sockets bound the
  wildcard address, where Windows ignores `IP_MULTICAST_IF` for egress, so
  every announcement followed the default route's NIC and LAN peers heard
  nothing. Sockets now bind the interface address, pinning both send and
  receive (measured: Debian peer's Avahi saw nothing before, both services
  plus CUPS auto-created queues after).
- **`dyj status` printed only the first declared format**: the table
  carries four, the client used a first-value getter; it now joins the
  full `document-format-supported` list.
- **Admin UI white-screen at `/admin` without a trailing slash**: the
  handler served index.html in place, so browsers resolved `app.js` against
  the server root and rejected its text/plain MIME; `/admin` now 301s to
  `/admin/` (regression test included).
- **Wizard and diagnostic components never rendered**: `dyj-stepper` and
  `dyj-diagnostic` used `this.shadowRoot` without attaching a shadow root,
  so the first `steps=` / `check=` assignment threw and left both empty;
  both attach in the constructor now (caught by live UI verification while
  shipping the icon work).

### Notes
- The items in **Added** above are what shipped in 0.3.0 (ADR-0010). An
  earlier draft of this note said the redesign was "design only"; that
  sentence was already false when 0.3.0 was tagged. The design record
  remains `docs/research/13-zero-config-redesign.md`. LAN clients without
  a device token receive 401.

## [0.2.0] — 2026-09-29

Complete client + server delivery: human-facing UI, real-printer support on
Linux/macOS, optional PDF, per-platform installers (ADR-0008, ADR-0009;
research: docs/research/10).

### Added
- **Web admin UI** (`http://<host>:6631/admin`, embedded, zero build chain):
  status dashboard, printer/network settings (restart-to-apply), job list
  with cancel, live SSE log view, first-run wizard with per-platform
  diagnostics. Vanilla Web Components — one control per file
  (`pkg/admin/web/components/`), one page per file, zh-CN default + en.
- **Admin API** under `/admin/api/*`: status, jobs, config
  (validated, persisted, restart-required), pause/resume, cancel, log SSE,
  diagnostics. Auth: loopback has full access; LAN gets read-only
  status/jobs and needs the `X-Dyj-Token` header for everything else
  (256-bit token in `<stateDir>/admin-token`, printed once at startup).
- **cups backend** (`-backend cups`, auto-picked when CUPS queues exist):
  Linux/macOS servers can now share REAL printers — spooled raster pages
  are wrapped into an image-only PDF (new `render.PagesToPDF`, golden
  tested) and submitted to a local CUPS queue via lp. Hardened process
  discipline: pinned absolute executable, argv-array only, bare-token
  validation for externally-sourced values.
- **Optional PDF support**: when poppler's `pdftoppm` is installed the
  server declares `application/pdf` (and syncs the mDNS `pdl=` promise);
  decoding runs pdftoppm as an external rasterizer. Without it, behavior
  is byte-identical to 0.1. Client: `dyj print *.pdf` passes PDF through
  sniffed by `%PDF-` magic.
- **`dyj doctor`**: per-machine diagnostics (state dir, port, 5353
  occupancy, TLS cert, PDF capability, firewall guidance, backend probe)
  in human or `--json` form; the same checks power the admin page.
- **Windows tray** (`dyj tray` + shipping `dyj-tray.exe`, windowsgui
  subsystem): status dot icon (green/gray/red), open admin page,
  pause/resume sharing, HKCU Run autostart toggle, quit-without-touching-
  the-service. `scripts/build-all.sh` now emits both Windows binaries.
- **Installers & packaging**:
  - Linux: `packaging/nfpm.yaml` → deb/rpm (amd64+arm64) shipping the
    hardened unit into `/usr/lib/systemd/system`, an avahi static service
    file (`/etc/avahi/services/dyj.service`, `_ipp._tcp` + admin page
    `_http._tcp`), and an admin-page `.desktop` entry; enable/start via
    preset semantics; the package never touches the running firewall.
    `scripts/package-linux.sh` is the one entry point (locally + CI).
  - Windows: `packaging/windows/dyj.iss` (Inno Setup) — per-machine
    install, firewall rules pre-created on ALL three profiles, HKLM Run
    tray autostart, ordered uninstall cleanup with keep-my-data prompt.
  - macOS: `packaging/macos/dyj.rb` Homebrew formula (with `brew services`
    support; no notarization needed via the formula path) and
    `org.dyj.printer.plist` LaunchDaemon template.
  - `packaging/install.sh`: curl-sh installer for headless hosts.
  - `.github/workflows/release.yml`: tag → binaries + packages + ISCC
    installer on the GitHub Release (signing steps pre-wired).
- **Homebrew-style service support**: `brew services start dyj` maps to
  `dyj service run` (documented in the formula).

### Changed
- Linux default backend (no `-backend`): **cups** when local CUPS queues
  exist, else file. The explicit flag always wins.
- mDNS TXT `adminurl` now points at `/admin` (previously the bare IPP
  GET endpoint).
- `dyj serve` banner prints the admin URL and admin token.

### Fixed
- **systemd service (`dyj service install`) hardened**: DynamicUser +
  StateDirectory (no more root), NoNewPrivileges, ProtectSystem=strict,
  empty capability bounding set (6631/5353 need none), AF_NETLINK allowed
  for interface enumeration; unit renderer shared by CLI install and the
  packaged unit (ADR-0009 D3).
- **launchd service fixed on three counts**: plists are now chowned
  root:wheel and chmod 644 (launchd otherwise refuses with "Dubious
  permissions"), load/unload replaced by bootstrap/bootout (10.10+
  domain syntax, legacy fallback), StandardOutPath added.


## [0.1.0] — 2026-09-29

First working delivery: full server + client, all platforms.

### Added
- **Server** (`dyj serve`): IPP 2.0 endpoint with the complete
  driverless operation set — Get-Printer-Attributes, Print-Job,
  Validate-Job, Create-Job + Send-Document (multi-document),
  Get-Jobs, Get-Job-Attributes, Cancel-Job, Identify-Printer.
  gzip document compression (IPP-level and HTTP-level),
  `ipp-attribute-fidelity` rejection with unsupported-attributes
  group, optional ipps:// (self-signed TLS), mDNS/DNS-SD
  advertisement with per-interface host names and conflict probing.
- **Spooler**: job state machine (held→pending→processing→completed/
  canceled/aborted), streaming to-disk spool, crash recovery
  (requeue pending jobs; docs are only recorded after their file is
  complete), bounded finished-job history.
- **Render pipeline**: byte-exact PWG Raster (PWG 5102.4) reader and
  writer against the CUPS implementation; text layout engine
  (bitmap font, word wrap, pagination); JPEG/PNG rasterization to
  page bitmaps (fit-scale, sRGB/sGray).
- **Drivers**: `wingdi` (Windows GDI print path through the installed
  vendor driver; DEVMODE paper/copies patching), `file` (page-per-PNG,
  debugging + E2E), `null`.
- **Client** (`dyj browse/status/print/jobs/cancel`): mDNS browsing,
  attribute inspection, document submission (txt/jpg/png/pwg → PWG
  raster), queue listing, job cancellation.
- **Service integration**: real Windows SCM service (install/uninstall/
  start/stop, recovery actions), systemd unit, launchd LaunchDaemon.
- **Build**: `scripts/build-all.sh` — CGO-free static binaries for
  windows/linux/darwin on amd64/arm64 (8–9 MB each); `scripts/e2e.sh`
  binary-level end-to-end test.
- **Attributes layer**: strict streaming RFC 8010 decoder that keeps
  every value of 1setOf attributes (replacing go-ipp's overwrite-bug
  decoder, ADR-0006); 9-octet resolution values (RFC 8011 §5.1.10);
  `printer-uuid`; honest document-format table (ADR-0007).

### Fixed
- operations-supported / all enums: 4-octet wire values (RFC 8010
  §3.8), fixing a desynchronizing 2-octet encoding.
- Attribute value-pair field order (tag|name-length|name|value-length|
  value), fixing responses no client could parse.
- resolution values carried the missing units octet (8→9 bytes).
- printer-current-time now carries the real UTC offset instead of
  local time mislabeled as +00:00.

### Security
- Private key material (TLS) is generated at runtime, persisted
  user-read-only, never committed; service install reads options from
  disk, never from command lines visible to other users; process
  spawning is limited to fixed allowListed helpers with constant
  arguments.

### Notes
- go-ipp remains a dependency for test-side cross-validation only
  (ADR-0006); production encode/decode is entirely in pkg/attributes.
