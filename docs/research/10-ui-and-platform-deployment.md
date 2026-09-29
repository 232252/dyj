# 10 — GUI、安装与系统集成调研（Windows / Linux / macOS）

> 调研时间：2026-09-29（Windows 部分）/ 2026-09-29（Linux、macOS 部分补齐）。
> 范围：面向普通用户的图形界面（布局与控件）、三平台安装器、服务/防火墙/自启动注册、分发渠道。
> 结论将沉淀为 ADR 后再动工；本文保留完整论据。
> 仓库现状基线：纯 CLI（`cmd/dyj`）+ 三平台服务化（`pkg/service`：Windows SCM / systemd /
> launchd），单二进制 8–9 MB 纯 Go 静态，`serve.go` 的 mDNS TXT 已广播 `AdminURL` 字段
> （cmd/dyj/serve.go:195）但尚无管理页可打开。

## 0. 结论速览

| 决策点 | 结论 | 一句话理由 |
| --- | --- | --- |
| GUI 形态（三平台一致） | **内嵌 Web 管理页（挂在现有 6631 HTTP 的 `/admin`）** 为唯一必做 UI | 零 CGO、体积 +1~2 MB、三平台零成本、可跨机管理（Syncthing/qBittorrent-nox/CUPS 已验证的模式） |
| 托盘 | **v1 只做 Windows**（`dyj-tray.exe`，`fyne.io/systray` 纯 syscall 零 CGO）；Linux/macOS 留作 v2 可选件 | GNOME 至今无原生托盘（要装扩展）；macOS 托盘必须 cgo + .app 壳；Linux 用户对 Web UI 接受度高 |
| 控件组织 | **原生 Web Components，一个控件一个 `.js` 文件**，无 npm 无构建链，`go:embed` 内嵌 | 满足"每个控件单独文件"且不引入 Node 工具链 |
| Windows 安装/自启 | Inno Setup per-machine；SCM 服务 + HKLM Run 托盘；防火墙三条 profile 全放行 | 详见 §3 |
| Linux 安装/注册 | **deb/rpm（goreleaser+nfpm）+ 加固 systemd unit + avahi 静态服务文件**；**不自动改防火墙**（改用管理页自检）；.desktop 入口 | 打包政策要求包只投放数据不碰运行时防火墙；avahi 在则注册走 avahi，自管 5353 变回退路径 |
| macOS 安装/注册 | LaunchDaemon（root，现有代码要修 3 处）+ **Homebrew formula（含 service 块）**；**不做未公证 cask/pkg** | formula/tarball 无 quarantine、Gatekeeper 不设防、免 $99/年公证；Homebrew 5.0 起 cask 必须已公证 |
| 代码签名 | Windows：Azure Artifact Signing + OV；macOS：当前阶段不做公证；Linux：deb/rpm GPG 签名仓库 | EV 特权 2024 已取消；macOS formula 路线不触发 Gatekeeper |
| 关键产品 gap | **Linux 服务端目前无法共享真实打印机**（只有 file/null 后端）；建议加 cups 后端（lp 起步，量级数天） | 否则 Linux 版只能"演示"，装了也打不出纸 |

---

## 1. GUI 形态选型

### 1.1 候选对比（2026-09 数据，来源见 §8）

| 方案 | 体积增量 | 托盘 | CGO | 维护状态 | 关键风险 |
| --- | --- | --- | --- | --- | --- |
| **Web 管理页 + 按平台可选托盘** | **+1~2 MB（Web）**；Windows 托盘 exe +1 MB | Windows 零 CGO；Linux 走 D-Bus SNI 纯 Go；macOS 需 cgo | Web 零 | systray v1.12.2（2026-06 发版） | 无原生窗口；依赖系统浏览器 |
| Wails v2 + systray | 总包 8~15 MB | v2 无内建，拼第三方 | Win 免 CGO；Linux 依赖 WebKitGTK | v2.10 进入维护模式 | WebView2/WebKitGTK 运行时变量 + 前端构建链 |
| Wails v3 | 总包 8~15 MB | **原生托盘 API 完整**（AttachWindow 弹主窗） | Win 免 CGO | 2026-08 进 Beta | 生产押注 Beta 有风险 |
| Fyne v2.8 | 总包 20~40 MB | v2.2 起内建，一等公民 | **必须**（Win 需 MinGW-w64） | 候选中最活跃 | 体积、CGO、交叉编译复杂化、非原生观感 |
| lxn/walk | +3~5 MB | NotifyIcon 有 | 无 | **多年无实质维护** | 无人背书，仅 Windows |
| Energy（CEF） | +150~200 MB | — | 是 | 活跃 | 分发整个 Chromium |
| Gio | +5~10 MB | 无内建 | 部分 | 活跃 | 表单/向导类 UI 开发成本过高 |

### 1.2 为什么是"Web 页为主、托盘为可选件"

1. **进程模型天然匹配**：GUI 的职责只是配置与观测，不该绑住 7×24 服务的生命周期。
   Web 页与后端完全解耦；`fyne.io/systray` 的 `Register()` 不独占进程，可独立托盘进程。
2. **跨机器管理是真实需求**：打印机接在 A 机器上，人坐在 B 笔记本前——局域网任意机器打开
   `http://<主机>:6631/admin` 直接配置，桌面 GUI 天然做不到。`serve.go` 的 mDNS TXT
   `AdminURL` 字段已经为这个场景预留了入口。
3. **先例充分**：Syncthing（官方只做 daemon + Web UI，托盘全是社区项目）、qbittorrent-nox
   （纯 WebUI + systemd）、CUPS 自身（Web 管理界面）——"守护进程 + Web 管理页"在
   Linux 世界是默认形态，普通用户接受度已被十年验证。
4. **体积与工具链代价最小**：静态资源 `go:embed`，不引入 Node/CGO/运行时依赖。

### 1.3 托盘的三平台现实（Windows 版报告§1 的关键修正）

| 平台 | 托盘生态现状 | Go 实现路径 | 结论 |
| --- | --- | --- | --- |
| **Windows** | 托盘是用户习惯，必备 | `fyne.io/systray` 走纯 syscall，**零 CGO**，现流水线直接出 `dyj-tray.exe` | **v1 做** |
| **Linux** | **GNOME（45→49）始终无原生托盘**，必须装 AppIndicator 扩展（Ubuntu 预装、Fedora Workstation 不预装）；KDE/XFCE/Cinnamon/MATE 原生支持；GNOME 50 将移除 X11，旧式 XEmbed 托盘死亡，SNI（纯 D-Bus）是唯一路径，Wayland 下反而统一 | `fyne.io/systray` 在 Linux 已去 GTK 化（godbus 走 SNI，**无 CGO**，Tailscale 官方 systray 即此谱系 + `CGO_ENABLED=0` 静态构建）；纯 Go SNI 库 gogpu/systray（2026-05）太早期不押注 | **v1 不做**（Syncthing 模式：Web 页 + `.desktop` 入口足够）；v2 可加独立 tray helper（XDG autostart 启动，localhost API 通信，Tailscale 模式） |
| **macOS** | 菜单栏（NSStatusBar）应用必须打成 `.app` bundle（裸二进制行为不可靠），`LSUIElement=1` 做成无 Dock 图标的菜单栏代理 | `fyne.io/systray` 在 darwin 是 ObjC 桥 + **cgo**，意味着 macOS 构建要在 macos runner 上原生跑（`CGO_ENABLED=1`），打破现有纯交叉编译矩阵 | **v1 不做**；v2 做 `.app` 壳（Info.plist + icns 脚本即可生成，代码不用改），先例 syncthing-macos / ollama |

> 重要修正：Windows 版报告曾假设"systray 库在 Linux 需要 GTK+CGO"——**对 getlantern 谱系
> 旧版成立，对现 fyne-io/systray 不成立**（Linux 纯 D-Bus SNI）。但结论不变：Linux v1 仍不做
> 托盘，原因从"技术做不到"变为"生态不值得"（GNOME 裸装用户看不到托盘）。

### 1.4 明确放弃的选项

- **lxn/walk**：事实弃维护；Energy/CEF、Tauri sidecar：体积/工具链正面冲突。
- **Fyne**：CGO 打破"纯 `GOOS=windows` 交叉编译"流水线，20~40 MB 体积不可接受。
- **Wails v2**：为一个"配置+观测"页面引入 WebView 运行时变量和前端构建链不值；
  管理页前端资产可原样搬进任何壳，**Wails v3 转正后复评**（届时迁移成本最低）。
- **Flatpak（服务端部分）**：沙箱不能安装/管理宿主 systemd unit、不能写宿主 autostart、
  端口与 D-Bus 要逐条 override——对"系统级监听 LAN + mDNS 的守护进程"是负资产
  （将来 GUI-only 客户端才可考虑）。Snap 对系统 daemon 可行但接口授权繁琐且只有 Ubuntu
  用户受益，低优先级；AppImage 完全没有服务集成，不适配。

---

## 2. 推荐方案的界面设计

> 本章为三平台共享设计——Web 管理页本身完全平台无关；平台差异只在"入口"（§2.5 注记）。

### 2.1 用户与核心问题

目标用户：家庭/小办公室的普通用户（不是命令行用户）。他们打开界面只关心四件事：

1. "现在局域网里别人能不能打到这台打印机？"（**状态可见性**是第一需求）
2. 第一次怎么把它配起来？（**向导**，不能让人读 README 敲命令）
3. 出了问题怎么办？（**人话错误 + 自检诊断**，如 5353 被占、防火墙拦了）
4. 谁打了什么？（**作业历史**）

### 2.2 信息架构（5 页 + 1 向导）

```
┌────────────────────────────────────────────────────────────┐
│ 侧栏          主区                                          │
│ ──────────   ────────────────────────────────────────────  │
│ ● 状态        ┌──────────────────────────────────────┐     │
│   打印机      │  共享中  ●（徽章）   [暂停共享] 按钮   │     │
│   网络        │  "办公室打印机" 可被局域网发现         │     │
│   作业        │  ipp://192.168.1.10:6631/ipp/print   │     │
│   日志        │  [复制地址] [如何添加到各系统 →]        │     │
│              └──────────────────────────────────────┘     │
│ ──────────   ┌─────────────┐ ┌─────────────┐              │
│ ⚙ 高级        │ 今日作业 3   │ │ 发现错误 0   │  …卡片      │
│   （折叠）    └─────────────┘ └─────────────┘              │
└────────────────────────────────────────────────────────────┘
```

| 页面 | 内容 | 关键控件 |
| --- | --- | --- |
| **状态（首页）** | 运行徽章、打印机名、URI 一键复制、mDNS 状态、"怎么添加"分平台折叠说明 | 状态徽章、复制字段、折叠面板 |
| **打印机** | 打印队列下拉（Windows 枚举 `Get-Printer`；Linux/macOS 枚举本地 CUPS 队列，见 §4.4）、共享名称、后端选择（默认折叠） | 下拉、文本框、单选 |
| **网络** | 端口、TLS 端口、接口多选、mDNS 开关——全部放"高级"折叠区，默认值即最优 | 数字框、开关、多选 |
| **作业** | 队列 + 历史（时间/用户/文件名/状态/页数），取消、重打 | 表格、行内按钮、空态 |
| **日志** | 实时日志流（SSE）、级别过滤、下载 | 日志视图、切换组 |
| **首次向导** | 欢迎 → 选打印机 → 起名 → 自检（防火墙/5353/端口）→ 完成页给出三平台添加指引 | 步进器、诊断卡 |

**向导是安装体验的核心**：Windows 上安装器装完勾选"启动 dyj"后自动打开管理页进入向导；
自检步骤把各平台的坑（Windows 防火墙 profile、Linux firewalld 默认拦、5353 占用）变成
**可点击的"立即修复/查看办法"**，而不是让用户去读 FAQ。

### 2.3 控件清单（每个控件一个文件）

全部实现为**原生自定义元素（Web Components）**，一个控件一个 `.js` 文件、
自注册 `customElements.define`、Shadow DOM 封装样式。无 npm、无构建链、无 TypeScript——
与"无巨石、好维护"一致；若未来迁 Wails，这些组件可原样搬进任何壳。

| 控件 | 文件 | 规格要点 |
| --- | --- | --- |
| 按钮 | `web/components/dyj-button.js` | variant: primary/secondary/danger；loading 态禁点；焦点环 |
| 开关 | `web/components/dyj-toggle.js` | 即时生效 + 失败回滚动画；ARIA `role="switch"` |
| 下拉 | `web/components/dyj-select.js` | 键盘可达；选项带描述行（队列名 + 驱动名） |
| 文本/数字框 | `web/components/dyj-textfield.js` | 内联校验（端口范围、名称非空）；错误文案人话 |
| 卡片 | `web/components/dyj-card.js` | 首页栅格单元，含标题/正文/动作插槽 |
| 状态徽章 | `web/components/dyj-status-badge.js` | 共享中(绿)/已暂停(灰)/故障(红)，色盲安全（加图标不只靠色） |
| 复制字段 | `web/components/dyj-copy-field.js` | URI 展示 + 一键复制 + "已复制"反馈 |
| 折叠面板 | `web/components/dyj-disclosure.js` | "高级设置"与"怎么添加"复用 |
| 表格 | `web/components/dyj-job-table.js` | 作业列表；列排序；行内取消/重打；空态 |
| 日志视图 | `web/components/dyj-log-viewer.js` | SSE 追加、自动滚底、暂停跟随、级别着色 |
| 步进器 | `web/components/dyj-stepper.js` | 向导骨架；可回退；断点续走 |
| 诊断卡 | `web/components/dyj-diagnostic.js` | 检查项：通过/失败/修复中/需手动，附"修复"或"查看办法" |
| 模态 | `web/components/dyj-modal.js` | 危险操作确认（改端口、关闭 mDNS） |
| 轻提示 | `web/components/dyj-toast.js` | 保存成功/失败反馈，自动消退 |
| 空态 | `web/components/dyj-empty-state.js` | 无作业/无打印机时的引导文案 + 动作按钮 |
| 侧栏/头栏 | `web/components/dyj-sidebar.js`、`dyj-header.js` | 导航 + 全局状态徽章 + 主题切换 |
| 加载态 | `web/components/dyj-spinner.js` | 骨架屏用 |
| 图标 | `web/components/dyj-icon.js` | 内联 SVG sprite，单一来源 |

页面同样一页一文件：`web/pages/page-dashboard.js`、`page-printer.js`、`page-network.js`、
`page-jobs.js`、`page-logs.js`、`page-wizard.js`。`web/app.js` 只做路由与布局壳，
`web/api.js` 是 `/admin/api` 的 fetch + SSE 客户端，`web/theme.css` 管设计令牌
（色板/字号/间距，深浅色跟随系统），`web/i18n/zh-CN.js`（默认）+ `en.js` 集中文案。

### 2.4 Go 侧结构与进程模型

```
pkg/admin/            管理页静态资源挂载 + /admin/api（复用现有 HTTP mux 的旁路路由）
pkg/admin/web/        //go:embed web（上面整棵 web/ 树）
pkg/tray/             fyne.io/systray 托盘（Windows v1 交付）
cmd/dyj/              新增 tray 子命令（开发/调试用，console 子系统）
cmd/dyj-tray/         Windows 托盘 exe 入口（-ldflags="-H=windowsgui"，见 §3.2）
```

进程模型：

- **服务进程**：现有 `dyj service run`（SCM/systemd/LaunchDaemon），无 UI，持
  IPP+mDNS+管理页。
- **Windows 托盘进程**：`dyj-tray.exe`，用户会话，仅做状态显示与快捷动作；
  通过 `http://127.0.0.1:<port>/admin/api/status` 轮询（服务在监听，回环天然可达）。
  **不引入命名管道**——管理 API 就在服务自己的 HTTP 端口上，为已覆盖的需求引
  `go-winio` + ACL 是负复杂度（管道留给 Wails v3 复评时再议）。
- **"暂停/恢复共享"**走管理 API（服务内部停/起 accept），**不动 SCM/systemd**——
  普通用户进程无权启停系统服务，且 stop 会连管理页一起杀掉。系统服务的启停保留给
  安装器/高级用户（Linux 可选 polkit 规则，见 §4.3）。

**待决策（P0-3）**：管理 API 的鉴权模型。监听在局域网端口上意味着同网段可访问写接口。
倾向：写操作需 token（安装时生成于状态目录，浏览器首次输入后存 localStorage；托盘菜单可
复制）；本机回环读状态免鉴权。需在动工前定稿。

### 2.5 各平台的"入口"

| 平台 | v1 入口 | 备注 |
| --- | --- | --- |
| Windows | 托盘（左键开管理页/右键菜单）+ 开始菜单快捷方式 | 菜单：打开管理页/暂停共享/复制地址/登录时启动/疑难解答/退出托盘 |
| Linux | `.desktop`（`/usr/share/applications/dyj-admin.desktop`，`Exec=xdg-open http://localhost:6631/admin`）进应用菜单 | `xdg-open` 须在用户会话内执行；不自动弹浏览器 |
| macOS | v1：文档 + mDNS TXT `AdminURL`（Safari/查找器 Bonjour 可发现）；v2：`.app` 菜单栏壳 | 不自动弹浏览器 |

### 2.6 交互原则（评审 UI PR 时按此判定）

1. **默认即最优**：所有设置有合理默认值，向导走完即可用；高级项一律折叠。
2. **人话错误**：不暴露 Go error 原文；每条错误配"发生了什么/怎么修"。
   例：5353 被占 → "你的电脑上有其他程序（可能是 iTunes/Bonjour/系统 mDNS 服务）占用了
   打印机发现通道，[查看解决办法]"。
3. **危险操作隔离 + 确认**：改端口、关 mDNS 需模态确认并说明后果。
4. **即时反馈**：每个写操作有 toast；进行中有 loading；失败可回滚提示。
5. **可诊断性**：自检页按平台检查——防火墙规则（Win）、firewalld/ufw 状态（Linux）、
   5353 占用方、端口监听、mDNS 自见。
6. **中英文从第一天分离**：字符串集中 i18n 文件，控件不硬编码文案。
7. **可达性**：键盘全导航、焦点环、`aria-*`、字号 ≥14px、状态不只靠颜色区分。

---

## 3. Windows 集成

### 3.1 "注册"的范围澄清

本项目**不需要注册任何打印驱动、文件关联或 COM 组件**——IPP Everywhere 免驱是路线
自带优势。需要注册的只有三样：**SCM 服务、防火墙规则、自启动项**。

### 3.2 自启架构：SCM 服务 + Run 键托盘（双 exe）

- **服务端必须走 SCM**：Run 键/启动文件夹都要等用户登录；登录前监听 6631/5353 只有服务
  做得到。Session 0 隔离下服务**不能**显示托盘——"服务 + 托盘分离"是硬性要求。
  现有 `pkg/service`（`mgr.CreateService` + StartAutomatic + 恢复动作）已是正确实现，
  唯一建议：账户从默认 LocalSystem 降为 **NetworkService**（最小权限；微软明确建议
  非必要不用 LocalSystem），需真机验证 NetworkService 下 wingdi 能否枚举打印队列（P0-6）。
- **托盘走 HKLM Run**（per-machine 安装写全用户登录自启）或托盘首启写 HKCU。
  不用任务计划程序（对托盘无增益）。
- **为什么是双 exe**：`dyj.exe` 是 console 子系统（CLI 输出需要它）；Run 键拉起 console
  程序会闪黑色控制台窗。所以 Windows 产物增加 `dyj-tray.exe`
  （`-ldflags="-H=windowsgui"`，约 +1 MB，`go-versioninfo` 塞版本资源与 manifest），
  复用同一份 `pkg/tray`。

### 3.3 Inno Setup 安装器（per-machine）

选择依据：GitHub Actions `windows-latest` **预装 ISCC**，脚本类 INI + Pascal Script 学习成本最低，
`/VERYSILENT /SUPPRESSMSGBOXES /NORESTART` 静默参数被 winget `InstallerType: inno` 自动映射。
WiX/MSI 只有进企业 GPO/Intune 渠道才值得；MSIX 对带服务的 Win32 工具是负资产。
（NSIS 必须 ≥3.11，旧版有安装期提权 CVE；选 Inno 此条仅作记录。）

安装清单（全部在 `.iss` 内完成，顺序执行）：

```
[Files]    dyj.exe → {app}；dyj-tray.exe → {app}
[Run]      netsh advfirewall firewall add rule name="dyj IPP"  dir=in action=allow
             program="{app}\dyj.exe" protocol=TCP localport=6631 profile=domain,private,public
           netsh advfirewall firewall add rule name="dyj mDNS" dir=in action=allow
             program="{app}\dyj.exe" protocol=UDP localport=5353 profile=domain,private,public
           dyj service install -- -name "…" -printer "…"   （或装后向导里配）
           dyj service start
           注册表 HKLM\...\CurrentVersion\Run → "dyj-tray.exe"（可勾选）
           [Icons] 开始菜单"dyj 打印机共享"（指向 dyj-tray.exe）
```

要点（每条都有真实事故背书，来源见 §8）：

1. **防火墙必须安装器预建、显式列三个 profile**。"Windows 安全中心警报"弹窗默认不勾
   "公用网络"，而**无网关/静态 IP/部分路由器环境会被归为公用**且界面不给切换——
   这是"客户端搜不到打印机"的头号来源。程序作用域 + 具体端口已收窄暴露面。
2. **不碰系统 mDNS 策略**：Windows 10 1709+ 的 Dnscache 内置 mDNS 会绑 UDP 5353，
   iTunes/Bonjour 也会占口。安装器**不改** `EnableMulticast`（系统级策略）；
   管理页自检页负责检测占用并给出排查指引（`Get-NetUDPEndpoint -LocalPort 5353`）。
3. `sc`/`netsh` 的 `[Run]` 调用统一 `runhidden`；`PrivilegesRequired=admin`；
   注意 64 位模式 System32 重定向。

卸载清单（`[UninstallRun]` 倒序）：杀托盘进程 → `dyj service stop` → `dyj service uninstall` →
删两条防火墙规则 → 删 Run 键 → 删安装目录（Inno 自动）。`%ProgramData%\dyj`
（spool/UUID/TLS 钥匙/配置）**默认保留**，完成页给"同时删除配置"勾选。

签名：**Azure Artifact Signing**（原 Trusted Signing，2026-01 更名）Basic $9.99/月，
对个人开发者开放，官方 GitHub Action 直连 CI，密钥不出 HSM。EV 的 SmartScreen 免警告
特权 2024 年已被微软取消，OV 即可；**必须签名 + 时间戳，长期同一身份**，安装器与两个 exe 都签。

### 3.4 Windows 分发渠道

**GitHub Releases（签名的 setup.exe 为唯一真源）→ CI 用 `wingetcreate submit` 自动提
winget-pkgs PR（每个版本一个）→ 自建 scoop bucket（checkver/autoupdate 指向 Releases）**。
三渠道同源，维护集中在一条 CI。不做 Microsoft Store（MSIX 与服务模型冲突）。

---

## 4. Linux 集成

### 4.1 现有 `pkg/service/service_unix.go` 与平台规范的差异（代码级发现）

| # | 现状 | 规范做法 | 影响 |
| --- | --- | --- | --- |
| 1 | unit 写到 `/etc/systemd/system/` | 包安装投放 **`/usr/lib/systemd/system/`**（`/etc` 是管理员覆盖区，包不得直接写；脚本安装写 `/etc` 可接受） | 打 deb/rpm 后升级/卸载语义混乱 |
| 2 | `User=root` | 加固集：`DynamicUser=yes` + `StateDirectory=dyj`、`NoNewPrivileges=yes`、`ProtectSystem=strict`、`ProtectHome=yes`、`PrivateTmp=yes`、`CapabilityBoundingSet=`（留空，6631/5353 均 >1024 无需任何 capability）、`RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK`（mDNS 接口枚举需要 AF_NETLINK）；用 `systemd-analyze security` 打分 | 以 root 跑一个网络监听服务是过度权限 |
| 3 | 安装即 `systemctl enable`（裸调） | 包走 **preset 语义**：deb 用 `deb-systemd-helper enable` + `deb-systemd-invoke start`，rpm 用 `%systemd_post`/`%systemd_preun`（本质 `systemctl preset`） | 尊重发行版/管理员 preset 策略，且对非 systemd 系统自动失效 |
| 4 | launchd plist 写入后直接返回，未设属主 | `/Library/LaunchDaemons` 的 plist **必须 root:wheel、0644**，否则 launchd 报 "Dubious permissions" 拒载 | 装完起不来的静默失败 |
| 5 | `launchctl load/unload` | 自 10.10 起废弃，应为 `launchctl bootstrap system <plist>` / `bootout` / `kickstart` | 现在仍能用，但随时可能移除 |
| 6 | — | `Type=exec`（systemd）、`StandardOutPath/StandardErrorPath`（launchd，现只有 Error） | 调试体验 |

以上属服务化代码的修正项，动 GUI 前顺手修（量级小）。

### 4.2 安装与打包

- **deb/rpm 是一等公民渠道**，工具选 **nfpm**（goreleaser 官方配套，一份 YAML 同时出
  deb/rpm/apk/arch；`contents:` 投 unit/avahi 服务文件/.desktop；`scripts:` 共用 postinstall
  脚本需自行 guard 发行版）。goreleaser 的 `nfpms:` 段与现有交叉编译流水线原生集成，
  CI 无需目标系统环境。
- **AUR**：适配好，惯例是 unit 装 `/usr/lib/systemd/system` 且**包内不 enable**
  （Arch 政策：启停交给用户 `systemctl enable --now`）。
- **无头服务器**：`install.sh`（探测发行版 → 提示下载对应包或直接铺二进制 + unit →
  `systemctl enable --now`），复刻 Docker/Tailscale 的脚本模式。
- Flatpak/AppImage/Snap：对 daemon 部分全部不适配（§1.4），不做。

### 4.3 注册与系统集成清单

1. **systemd unit**（包投放 `/usr/lib/systemd/system/dyj.service`）：§4.1#2 的加固集 +
   `After=network-online.target`（已有）+ `Restart=on-failure`（已有）。
2. **防火墙：不自动改**。Debian 默认无启用防火墙、Ubuntu ufw 预装但默认 inactive——
   这两个最大用户群不需要任何动作；Fedora/RHEL firewalld 默认 public zone 拦入站，是唯一
   真正会踩的场景。**社区规范明确：打包脚本动运行时防火墙被视为侵入**（Debian Policy、
   Fedora Scriptlets 指南；包只可投放 firewalld 的 service XML 定义这类"数据"）。
   替代方案：**管理页自检**（入站不可达时给出各发行版一行放行命令：
   `firewall-cmd --permanent --add-port=6631/tcp && firewall-cmd --reload`；
   5353/udp 通常已被 firewalld 的 mdns 服务放行）+ 文档。
3. **mDNS：双轨注册**。包投放 `/etc/avahi/services/dyj.service` 静态服务文件
   （`_ipp._tcp` + `_http._tcp` 管理页 + TXT `rp=ipp/print`）；运行时探测 avahi
   （system bus 的 `org.freedesktop.Avahi`），**avahi 在时走 avahi 通告、dyj 根本不必监听
   UDP 5353**；无 avahi（无头最小系统）再回退现有自管 socket。这与已在 Debian 真机验证的
   "自管 socket 与 avahi 共存"互为冗余，但静态文件对发行版集成更地道（TrueNAS/SavaPage
   等均如此发布）。
4. **`.desktop`**：`/usr/share/applications/dyj-admin.desktop` → 应用菜单"打开 dyj 管理页"。
   登录自启留给 v2 托盘 helper（XDG autostart 规范，`/etc/xdg/autostart/`）；**不要**
   登录时自动弹浏览器。
5. **普通用户启停服务（可选）**：polkit JS 规则
   `/etc/polkit-1/rules.d/50-dyj.rules` 精准放行 `org.freedesktop.systemd1.manage-units`
   且 `unit == "dyj.service"`（旧的 pkla 格式无法按 unit 过滤，不要用）。但由于管理页在
   服务进程内提供"暂停/恢复共享"，此规则只在 CLI/GUI 控制器场景才需要——v1 可不做。
6. **卸载清理**：prerm 停服务 → postrm 删 unit（deb-systemd-helper 已处理的 enable 状态
   会自动清）、avahi 服务文件、.desktop；`/var/lib/dyj`（spool/UUID）默认保留。

### 4.4 关键产品 gap：Linux 无法共享真实打印机（cups 后端）

现状：`pkg/driver` 只有 wingdi（Windows）/file/null。**Linux 上 dyj 装完只能当演示服务器**
（收作业存文件），"把接在这台 Linux 机器上的打印机共享出去"没有实现路径。三条改造路径：

| 路径 | 做法 | 量级 |
| --- | --- | --- |
| A. shell 出 `lp`（推荐起步） | `lp -d <queue> -` 从 stdin 提交本地 CUPS 队列（CUPS 默认监听 localhost:631 本地免认证）；`lpstat -p` 枚举队列；依赖 cups-client 包 | **数天** |
| B. Go IPP 客户端转发到 localhost:631 | phin1x/go-ipp（已在本仓库 test 依赖，有 CupsClient）或 OpenPrinting/goipp（ipp-usb 同源）做队列发现/属性映射/状态回传 | 1–2 周 |
| C. IPP 反向代理 | 把收到的 IPP 请求体重写到 `ipp://localhost:631/printers/<queue>`（CUPS 本身就是标准 IPP listener），与 ipp-usb 同型 | 2–3 周，URI 重写细节多 |

建议：做**可选 cups 后端**（检测到 `/run/cups/cups.sock` 或本机 `_ipp._tcp` 即在管理页
可选队列）；v1 用路径 A（错误回报粗但够用），v2 升 B。CUPS 自己有队列，不必深做状态回传。

### 4.5 Linux 分发优先级

① GitHub Releases 的 deb/rpm + `install.sh`（无头场景）→ ② AUR → ③ 站稳后自托管
apt/yum 仓库（reprepro + 静态托管 / Cloudsmith；**YUM 仓库必须 GPG 签名**）。
deb/rpm 里的二进制同样来自现有 `build-all.sh` 产物，流水线只加 nfpm 一层。

---

## 5. macOS 集成

### 5.1 launchd：LaunchDaemon 承载服务端（现有代码修 3 处）

- **LaunchDaemon**（`/Library/LaunchDaemons/org.dyj.printer.plist`，root、开机即跑、
  登录前可打印）是对的位置——且 **root daemon 天然绕开"本地网络隐私"弹窗的主要雷区**
  （§5.4）。现有 plist 的 `RunAtLoad`+`KeepAlive`+`StandardErrorPath` 方向正确。
  不用 launchd 的 `Sockets` 代持监听（对长驻服务无收益，还改 fd 模型）。
- 需修（对应 §4.1 表 #4/#5/#6）：写入后 **`chown root:wheel` + `chmod 644`**（否则
  "Dubious permissions" 拒载）；`launchctl load` 改 **`bootstrap system`**（卸载 `bootout`）；
  补 `StandardOutPath`。安装动作需要 root，文档与安装器走 `sudo`。
- 并存路径：**Homebrew formula 的 `service` 块**（`brew services start dyj`）本质就是
  launchd 包装器，可作极简替代安装路径（gui 域 Agent 或 sudo 装 system 域 Daemon）。

### 5.2 GUI 形态

v1 与 Linux 一致：**Web 管理页为唯一 UI**。v2 可选托盘：同一 Go 二进制 + 最小 `.app` 壳
（`Info.plist` 设 `LSUIElement=1` 无 Dock 图标 + icns，脚本即可生成），
`fyne.io/systray`（darwin 侧 cgo/ObjC，需 macos runner 原生构建），菜单"Open Web Admin"
即 `open http://localhost:6631/admin`——照抄 **syncthing-macos**（Go daemon + LSUIElement
菜单栏壳 + LaunchAgent 保活 + "Open WebGUI"）与 ollama 的成熟模板。纯 Go 托盘
（gogpu/systray）不押注。

### 5.3 分发与 Gatekeeper 定位

| 渠道 | 判定 |
| --- | --- |
| **Homebrew formula（含 service 块）** | **首选**。formula 走 brew 自身下载管线，不设 quarantine 扩展属性，**Gatekeeper 根本不检查**——免签名免公证 |
| **裸 tarball + curl\|sh** | 次选。同理免疫 Gatekeeper；作"无 Homebrew"官方路径 |
| cask（.app） | **不做未公证版**：Homebrew 5.0（2025-11）起 cask 必须 codesigned + notarized，未达标 2026-09 起被移除，`--no-quarantine` 绕过已弃用 |
| pkg 安装器 | 未公证 pkg 会弹"无法验证开发者"；做 pkg 就必须公证——与 cask 同等待遇，暂不做 |
| Mac App Store | 沙盒+审核+$99/年，对系统级打印服务不合适，排除 |

**公证（notarization）当前阶段不做**：Developer ID 只能随 Apple Developer Program（$99/年）
获得，而 formula/tarball 路线不触发 Gatekeeper，公证只对 .app/pkg 分发才有意义。等做
"给普通用户双击的 .app 安装器"且用户量证明值得时再上（notarytool + stapler 标准流程）。
背景：Sequoia 15 已移除"右键打开"绕过（只剩 系统设置→隐私与安全性→"仍要打开"），
Tahoe 26 上未公证 app 更多被报"已损坏"——**绕过路线已死，只有"不触发检查的渠道"和
"正经签名公证"两条路**。

### 5.4 防火墙与本地网络隐私

- **应用防火墙默认关闭、pf 默认关闭**——macOS 出厂对入站 6631/5353 无人拦截，比 Windows
  省一整节。用户手动开了防火墙后，未签名 app 首次监听会弹"接受传入连接"（可
  `socketfilterfw --add/--unblockapp` 预置，仅文档提及即可）。
- **本地网络权限（Local Network Privacy，Monterey 起）**：不基于 TCC、无查询 API、
  Sequoia 升级后出现大面积权限重置重弹；launchd Agent 场景有"批准后仍被拒"的实测报告。
  **root LaunchDaemon 是规避该不确定性最稳的形态**（理论不适用限制），但文档仍需写兜底：
  系统设置 → 隐私与安全性 → 本地网络 手动允许。

### 5.5 mDNS 共存

macOS `mDNSResponder` 常驻占 UDP 5353，但它以 `SO_REUSEADDR/SO_REUSEPORT` 方式绑定，
第三方设置相同选项可共享（现有 `pkg/discovery` 自管 socket 在 darwin 上需确认 socket
选项顺序，见 P0-9）。备选方案是走 `dns_sd.h` 注册让系统代为广播——那是 cgo，不采纳；
自管 socket 与 Windows/Linux 保持同一实现。

---

## 6. 落地顺序建议

| 里程碑 | 内容 | 前置 |
| --- | --- | --- |
| M0 | `pkg/service` 修正（§4.1 表 6 项；Windows NetworkService 验证） | 无 |
| M1 | `pkg/admin`：管理 API（状态/配置读写/作业/日志 SSE）+ 纯静态状态页（先通鉴权模型） | P0-3 定稿 |
| M2 | Web Components 控件集 + 五页 + 向导；`go:embed` | M1 |
| M3 | Windows：`pkg/tray` + `dyj tray` / `dyj-tray.exe`；HKLM Run 开关 | M1 |
| M4 | 打包三线：Inno Setup（Win）、nfpm deb/rpm + avahi 静态文件 + .desktop（Linux）、formula + `install.sh`（macOS）；CI 集成（ISCC/nfpm 均无需目标机） | M2、M3 |
| M5 | cups 后端（路径 A：lp），管理页打印机页支持本地 CUPS 队列 | M1 |
| M6 | winget/scoop（Win）、AUR + 自建仓库（Linux）、公证评估（macOS） | M4 |

M1/M3/M5 可并行。CLI 全程保留不动——GUI 只是 CLI 之上的皮，`dyj serve -printer …` 永远可用。

---

## 7. ⚠️ P0 待验证清单（动工前必须实测，不要当成已知事实）

| # | 待验证问题 | 为什么 | 验证方式 |
| --- | --- | --- | --- |
| **1** | `fyne.io/systray` 在真机 Win11：Explorer 崩溃重建图标（`TaskbarCreated`）、多 DPI、Exit 线程模型 | 托盘图标消失=用户以为程序死了 | Win11 真机：重启 Explorer、锁屏解锁、多屏拔插 |
| **2** | 管理页在目标浏览器矩阵的实际表现（Edge/Chrome 常青；显式放弃 IE） | Web Components 是 ES2018+ 特性 | 目标浏览器真机过一遍 |
| **3** | **管理 API 鉴权模型定稿**（token vs 本机 only vs 局域网只读） | 管理页与 IPP 共用局域网端口，写接口暴露面必须先想清楚 | 安全评审 + 实测 |
| **4** | Inno Setup 全流程真机验证（含公用 profile 误判场景、`sc` 32/64 重定向） | §3.3 每条坑都有真实事故 | Win10 + Win11 各一轮 |
| **5** | `dyj-tray.exe`（windowsgui）经 HKLM Run 自启：登录延迟、无控制台闪现、多用户切换 | 自启是硬需求 | 真机登录/切换用户 |
| **6** | Windows NetworkService 账户下 wingdi 能否枚举打印队列并投递 | §3.2 降权建议的前提 | 降权 + 真打一张 |
| **7** | 加固 systemd unit（DynamicUser 等）下 spool 目录读写、avahi 静态文件被 avahi 正确加载并通告 | §4.1/§4.3 的前提 | Debian 真机（192.168.110.214 现成）装 deb → `avahi-browse` 自见 |
| **8** | firewalld 默认拦场景下管理页自检给出的放行命令实测有效 | Fedora 是唯一默认拦截的大发行版 | Fedora/Alma 容器或真机 |
| **9** | darwin 自管 mDNS socket 与 mDNSResponder 的 5353 共享（SO_REUSEADDR/REUSEPORT 顺序） | macOS 上从没跑过 dyj serve | 任意 Mac 实测 browse/advertise |
| **10** | LaunchDaemon bootstrap 流程真机验证（root:wheel 权限、bootout 卸载、日志落盘） | §5.1 三处修正的验收 | 任意 Mac 实测 |
| **11** | cups 后端路径 A（lp）在真机 CUPS 上出纸 + `lpstat -p` 队列枚举格式 | §4.4 的前提 | Debian 真机已有 CUPS |
| **12** | Wails v3 转正时间跟踪（当前 Beta） | 复评触发条件 | 6~12 个月后复查 |

---

## 8. 主要来源

**GUI/托盘**
- Wails：v3 Beta 公告（v3.wails.io/blog/wails-v3-beta，2026-08）、v3 托盘文档（2026-07）、v2 issue #1521、v2 changelog（2025-02）
- Fyne：GitHub Releases（fyne-io/fyne，2025 末~2026-09）；systray 系：pkg.go.dev/fyne.io/systray（v1.12.2，2026-06）、getlantern/systray README（darwin cgo/.app bundle 要求）、tailscale/systray + Tailscale linux-systray 文档（2026-09）、gogpu/systray（2026-05，太早期）
- Linux 托盘生态：GNOME 扩展 #615 AppIndicator Support（extensions.gnome.org）、Hackaday "StatusNotifierItem: How Standard Non-Standards Tear Linux Desktops Apart"（2025-06）、Red Hat bug #1709401（Fedora 不预装）、GNOME 49/50 X11 移除报道（2026）
- 先例：Syncthing docs（Community Contributions——官方不做托盘）、qBittorrent wiki（nox + systemd）、syncthing-macos 仓库、ollama 仓库
- WebView2：Microsoft Learn — distribution / evergreen-vs-fixed（现行版）；lxn/walk：仓库 + tailscale/walk

**Windows**
- Inno Setup：jrsoftware.org isdl.php（6.4.x）/ ishelp — setupcmdline；NSIS CVE：AmberWolf（2026-04）；WiX Releases（v6/v7，2025-08）
- 服务/自启：Microsoft Learn — Using the LocalSystem Account、Service Recovery Actions、Run and RunOnce Registry Keys（2026-02）、Session 0 综述；go-winio IPC 对比（2026-05）
- 防火墙 profile 坑：Microsoft Learn Q&A（2025-12）、Hitachi/Penn State 产品手册（弹窗默认只勾专用）
- mDNS 5353：cesanta/mongoose#3125、grandcat/zeroconf#69、Microsoft Q&A（Bonjour jail，2024-11）
- 签名：Microsoft Learn — Azure Artifact Signing overview（2026-08）、windows-dev-docs code-signing-options（2026-04，EV 特权取消）
- 分发：Microsoft Learn — winget package submission（2024-11）、winget-pkgs、wingetcreate（2026-07）、jreleaser.org

**Linux**
- 打包：nfpm.goreleaser.com（configuration/tips）、goreleaser.com/customization/nfpm、Debian dh_installsystemd(1)（trixie，2025-03）、Debian Policy maintainer scripts、Fedora Packaging Guidelines — Scriptlets/Systemd、OpenTelemetry Collector releases issue #1334（enable 取舍）
- systemd 加固：systemd.exec(5)、Rocky Linux 加固指南、`systemd-analyze security`；unit 放置：freedesktop systemd.unit(5)
- 防火墙默认态：Debian 安全手册 §5.14、ufw/firewalld 官方文档、unix.stackexchange.com/questions/323933
- avahi：avahi-daemon(8)/avahi-service DTD、Sunshine 文档（D-Bus EntryGroup 注册）、SavaPage/TrueNAS 静态服务文件实例
- XDG：desktop-entry-spec、autostart-spec（freedesktop.org）、xdg-open(1)
- polkit：freedesktop polkit docs（JS rules vs pkla）
- 托盘可行性先例：Tailscale linux-systray 文档（CGO_ENABLED=0 + SNI）

**macOS**
- launchd：launchd.info（2026 仍维护）、Apple BPSystemStartup（官方存档）、man launchd.plist；Chromium 安装脚本（root:wheel 0644 实例）；brew Services DSL（docs.brew.sh）
- Gatekeeper/公证：Apple support 102445（Sequoia Open Anyway）、mjtsai.com（2024-07 Sequoia 收紧；2024-10 起本地网络隐私系列）、workbrew.com/blog/homebrew-5-0-0（2025-11，cask 强制公证）、Homebrew discussion #6482、Apple notarytool 文档（altool 2023-11 退役）
- 防火墙/本地网络：Apple Platform Security Guide、mjtsai 本地网络隐私汇总（Quinn/Tempelmann/Allen 实测）、eclecticlight.co 系列（2024-2026）
- mDNS：stackoverflow.com/questions/78326690（5353 REUSEPORT 共存，2024）、man mDNSResponder、HELIOS mdns 支持文档
