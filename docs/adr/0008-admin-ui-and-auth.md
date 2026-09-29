# ADR-0008：Web 管理页、托盘与管理 API 鉴权

- **状态**：已接受
- **日期**：2026-09-29
- **依据**：[调研 10](../research/10-ui-and-platform-deployment.md)（GUI 选型、
  三平台集成、Windows/Linux/macOS 安装与自启动）
- **前置**：[ADR-0002 技术栈](0002-stack-and-architecture.md)、[ADR-0007 诚实声明](0007-render-pipeline.md)

## 背景

v0.1 是纯 CLI 交付。真实用户（家庭/小办公室，非命令行用户）需要图形界面：
配置打印机、看作业、看日志、暂停共享、诊断"为什么对面打不出来"。同时服务端
`serve.go` 的 mDNS TXT 已广播 `AdminURL` 字段，但一直没有页面可打开。

调研结论（调研 10 §1）：**Web 管理页为三平台唯一必做 UI**（Syncthing/
qbittorrent-nox/CUPS 已验证的"守护进程 + Web 界面"模式），托盘按平台分化：
Windows v1 交付（用户习惯 + `fyne.io/systray` 纯 syscall 零 CGO），
Linux（GNOME 无原生托盘生态）与 macOS（cgo + .app 壳）留待 v2。

## 决策

### D1. 管理页挂在 IPP 同一个 HTTP 端口（/admin）

`ipp.Server` 增加可选 `Admin http.Handler`，用 `http.ServeMux` 前缀路由挂载；
IPP 流量路径不变（黄金测试 + 新增共存测试锁定）。

- **体积**：整个 UI 用 `go:embed` 进二进制，全部产物仍 ≤9.3 MB。
- **零构建链**：原生 ES Modules + Web Components，一个控件一个
  `.js` 文件（`web/components/dyj-*`），一个页面一个文件
  （`web/pages/page-*`），无 npm/Node/CDN——评审 UI PR 按文件粒度判"无巨石"。
- **跨机管理**：局域网任意设备可打开 `http://<主机>:6631/admin`——
  "打印机接在 A 机器、人坐在 B 设备"是真实场景，桌面 GUI 做不到。

### D2. 鉴权模型（P0 定稿）：回环全权，远程只读 + 写令牌

监听端口在局域网上意味着同网段可达。模型：

| 来源 | 读（status/jobs/静态页） | 写（config/pause/cancel）与日志流/诊断 |
| --- | --- | --- |
| 127.0.0.1（回环） | 允许 | **允许**（本机用户已拥有机器，托盘/本地浏览器零配置） |
| 局域网 | 允许（与"打印机本身局域网共享"一致的可见度） | 需 `X-Dyj-Token` = 256-bit 随机令牌（`<stateDir>/admin-token`，0600，启动时生成并打印一次；UI 首次输入后存 localStorage） |

拒绝理由不成立时的取舍：不搞"局域网只读都不给"（管理页的跨机价值就没了），
也不搞"全开放"（改配置/取消作业/看日志是明显高于打印的权限）。

### D3. 暂停/恢复共享在服务进程内实现（SetAccepting）

`spooler.Spooler` 增加 `SetAccepting`：暂停 = `Begin` 拒绝新作业
（`server-error-not-accepting-jobs`）+ `printer-state-reason: paused`；
队列中作业继续打完。**不走 SCM/systemd stop**：普通用户进程无权启停系统服务，
且 stop 会连管理页一起杀掉。系统服务启停保留给安装器/管理员
（Linux 可选 polkit 规则，见调研 10 §4.3）。

### D4. 托盘是独立轻进程，只做皮

`pkg/tray`（Controller：轮询回环 status、暂停/恢复、自启注册）
+ `fyne.io/systray`（Windows 粘合）+ `cmd/dyj tray`（控制台开发入口）
+ `cmd/dyj-tray`（`-H=windowsgui` 交付入口，登录自启不闪控制台）。
托盘退出不影响服务；IPC 就是 127.0.0.1 HTTP，不引入命名管道。
自启 = `HKCU\...\CurrentVersion\Run`（托盘菜单可开关）。

### D5. Linux/macOS v1 的入口

Linux：deb/rpm 自带 `.desktop`（应用菜单"dyj 管理页"）。
macOS：mDNS `adminurl` TXT + 文档；v2 评估 `.app` 菜单栏壳
（syncthing-macos 模板）。均不自动弹浏览器。

## 后果

- 新增依赖：`fyne.io/systray`（Windows 产物；纯 syscall，Linux 走 godbus
  可后续复用，darwin 需 cgo——故 darwin 构建不 import 托盘，保住纯交叉编译）。
- `ipp.Options` 增加一个可空字段；无管理页的嵌入式用法零感知。
- 令牌打印在 serve 横幅且落盘 stateDir——服务日志（journald/Windows
  事件日志/launchd 日志）可见，属可接受范围（能读系统日志者已能读 spool）。
- UI 的组件文件粒度是**硬约定**：新增控件必须新文件，禁止往 app.js 里堆。
