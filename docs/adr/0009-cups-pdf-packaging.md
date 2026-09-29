# ADR-0009：cups 后端、PDF 可选声明与三平台打包

- **状态**：已接受
- **日期**：2026-09-29
- **依据**：[调研 10](../research/10-ui-and-platform-deployment.md) §4/§5
  （Linux 集成、macOS 集成、打包与分发）
- **前置**：[ADR-0007 诚实声明](0007-render-pipeline.md)（格式声明 = 渲染能力）

## 背景

v0.1 在 Linux 上只有 file/null 后端——装了也无法把接在本机的真实打印机
共享出去，产品目标（LAN 打印共享）在 Linux 缺失一半。同理 PDF——真实世界
最常见的打印格式——v0.1 因"无渲染器"而明确不声明（ADR-0007 D1）。
打包层面 v0.1 只有裸二进制；调研 10 给出了三平台安装/自启/注册的完整结论。

## 决策

### D1. cups 后端：光栅页 → 图片 PDF → 本地 CUPS 队列（lp）

Linux/macOS 的真实打印路径：`pkg/driver/cups` 用共享解码前端
（`driver.Pages`）把任意已声明文档变成光栅页，经 `render.PagesToPDF`
打包成**一页一图 PDF**（无字体无矢量，确定性输出、黄金测试锁定），
提交给本地 CUPS 队列（`lp -d <queue>`）。

- **为什么经 CUPS 而不是直连打印机**：与 wingdi 同一原则——复用操作系统
  已装的驱动链（CUPS filter），dyj 永远零驱动。
- **为什么 lp 而不是 IPP-to-localhost**：lp 走 CUPS socket，所有发行版
  免配置可用；cups-client 随一切桌面发行版安装。
- **进程纪律**：可执行文件在 New() 时经 LookPath 钉为绝对路径，参数数组
  无 shell，来自 IPP 请求的值（队列/介质/sides）必须是裸 token
  （不得以 `-` 开头），否则该选项直接省略。
- **后端自动选择**（无 `-backend` 时）：Windows→wingdi；其他平台——
  存在 CUPS 队列→cups，否则 file。管理页/向导显式选择始终覆盖默认。

### D2. application/pdf：有 pdftoppm 才声明（诚实规则的延伸）

ADR-0007 禁止虚假声明的原则不变。新增**条件声明**：

- 机器上存在 poppler `pdftoppm` 时，服务端声明 `application/pdf`
  （`attributes.DefaultFormats() + application/pdf`），mDNS TXT 的
  `pdl=` 承诺同步；解码走 `pdftoppm -r DPI -png` 外部进程
  （无 Ghostscript AGPL 进程内代码，无 CGO）。
- 不存在时维持 v0.1 声明表——**未安装 poppler 的机器行为与 v0.1 完全一致**。
- 客户端 `dyj print *.pdf` 按 `%PDF-` 魔数嗅探后**原样透传**
  （`application/pdf`），由服务端栅格化；服务端不支持时按既有
  fidelity 规则得到明确的 0x040B 错误。
- `image/urf`（AirPrint）仍然不声明：无权威规范原文（调研索引 P0 #4 未解除）。

### D3. Linux 打包：nfpm deb/rpm + preset 语义 + avahi 静态通告

- `packaging/nfpm.yaml` 一份配置出 deb/rpm（amd64/arm64），
  `scripts/package-linux.sh` 为统一入口（CI 与本地同一条命令）。
- unit 装在 **`/usr/lib/systemd/system`**（vendor 槽；`/etc` 只留给管理员），
  加固集：DynamicUser + StateDirectory、NoNewPrivileges、ProtectSystem=strict、
  空能力集（6631/5353 均为高位端口）、`AF_NETLINK` 放行（接口枚举）。
  `dyj service install`（管理员手工路径）写 `/etc/systemd/system` 并使用
  同一加固模板——两处行为已由同一渲染函数保证一致。
- 启用走 **preset 语义**（deb-systemd-helper / `systemctl preset`），
  不裸调 `systemctl enable`；脚本对非 systemd 系统自动退化。
- **包绝不改运行时防火墙**（Debian/Fedora 政策）；firewalld 场景由
  管理页自检与安装后提示给出命令。
- avahi 静态服务文件（`/etc/avahi/services/dyj.service`）随包投放：
  avahi 存在时由它代为通告（`_ipp._tcp` + 管理页 `_http._tcp`），
  dyj 自管 5353 socket 保留为无 avahi 系统的回退——两者互为冗余。
- `.desktop` 入口（`xdg-open http://localhost:6631/admin`）进应用菜单。

### D4. macOS：LaunchDaemon 修正 + formula 交付

- `dyj service install` 修正三处（真机阻塞项）：plist 落盘后
  **chown root:wheel + chmod 644**（launchd "Dubious permissions" 拒载）、
  `launchctl load` → **bootstrap/bootout**（10.10+ 域语法，旧系统回退）、
  补 StandardOutPath。单元渲染函数与 launchd 权限由测试锁定。
- 分发：**Homebrew formula（含 service 块）**为首选——brew 下载不设
  quarantine，Gatekeeper 不检查，无需 Developer ID 公证；
  未公证 cask/pkg 不做（Homebrew 5.0 起 cask 强制公证）。
  `packaging/macos/org.dyj.printer.plist` 提供 brew 之外的手工 LaunchDaemon 模板。
- 托盘/GUI 维持调研 10 §1.3 结论：v1 用 Web 管理页，v2 评估 `.app` 壳。

### D5. Windows 打包与签名

- `packaging/windows/dyj.iss`（Inno Setup 6.4+，GitHub runner 预装 ISCC）：
  per-machine 安装；**防火墙规则安装器预建且三个 profile 全放行**
  （"小办公室网络被误判为公用"是搜不到打印机的头号来源）；HKLM Run
  写托盘自启（可跳过）；卸载倒序清理并询问是否保留 `%ProgramData%\dyj`。
- 产物：`dyj.exe` + `dyj-tray.exe`（windowsgui）双二进制随架构分发。
- 签名：CI 预留 Azure Artifact Signing 步骤位；证书到位前 README 标注
  SmartScreen 提示的处理方式。

### D6. CI（.github/workflows/release.yml）

tag 触发：ubuntu 跑 `build-all.sh`（8 个二进制）+ `package-linux.sh`
（4 个系统包）→ windows runner 跑 ISCC 出 `dyj-setup-x64.exe` →
全部产物挂 GitHub Release；formula sha256 刷新输出打印在日志里。

## 后果

- 新增运行时外部依赖（均可选、可探测）：`lp/lpstat`（cups 后端）、
  `pdftoppm`（PDF）。二者缺失时行为退回 v0.1，`dyj doctor` 明确报告。
- Linux 默认后端从 file 变为 cups（有队列时）——行为变化记录在 CHANGELOG。
- 打包产物清单（每次发版）：8 二进制 + 4 Linux 包 + 1 Windows 安装器 +
  4 tarball（formula 源）。
- 仍开放的真机验收（不变）：Win11 添加打印机 GUI、wingdi 真实打印机、
  加固 unit 下 spool 写入与 avahi 静态通告（Debian 真机可低成本验证）、
  macOS bootstrap 流程。
