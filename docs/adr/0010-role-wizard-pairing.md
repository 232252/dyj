# ADR-0010：零配置改版——单程序角色向导、4 位码配对与接收端自动装队列

- **状态**：已接受
- **日期**：2026-09-30
- **依据**：[调研 13](../research/13-zero-config-redesign.md)（v0.3 详细设计：
  产品流程、配对协议、检查点/日志/调试规范、阶段划分）
- **前置**：[ADR-0008 管理页与鉴权](0008-admin-ui-and-auth.md)、
  [ADR-0009 cups/打包](0009-cups-pdf-packaging.md)

## 背景

v0.2 真机验收（[调研 12](../research/12-windows-acceptance-2026-09-30.md)）后
用户反馈：流程对非命令行用户过于复杂——服务端要 `Get-Printer` 查队列名再拼
CLI flags；接收端要在 Windows 设置里手工按 IP 添加 IPP；首次安装甚至因
`service install` 不带参数而服务起不来（向导也打不开，首启死锁）。用户给出
明确方向：**一个程序、两个角色（共享/接收）、全程自动扫描、4 位码配对、
装完即在 WPS 里直接可见，"越傻瓜越好"，不要高级设置**；并特别要求设计文档
把检查点、日志、调试做扎实——自动化越多，无声失败越难查。

差距分析与完整设计见调研 13 §2/§4；本文只收敛决策。

## 决策

### D1. `dyj` 无参启动 = 状态机引导入口；旧 CLI 全部保留

读 `<stateDir>/service.json` 的 `role` 字段（新增，`""` 未配置 / share /
use / both）：未配置 → **bootstrap 模式**（最小 HTTP 服务：仅 admin UI +
配置/配对/枚举 API，不创建打印后端、不做 wingdi 校验——服务必须永远起得来，
这是对首启死锁的根治）；已配置 → 按角色运行。接收端（use）**不常驻**：
向导装完系统队列即退出。`serve/browse/print/...` 子命令原样保留。

### D2. 向导载体 = 内嵌 Web（复用 ADR-0008 栈），自动打开浏览器

bootstrap 与已配置状态都自动打开 `http://127.0.0.1:<port>/admin/#/setup`；
"已配置"判定从浏览器 localStorage（现状 `dyj_seen_wizard`）改为**服务端
`role` 字段**。新页面遵循一控件一文件/一页面一文件硬约定；向导只有两张
角色卡片（「共享本机打印机」/「添加网络打印机」），无高级设置面板。

### D3. 配对 = 4 位 PIN（限速 + 滚动轮换）一次性换设备 token；数据面 token 走 URI 路径

- PIN：4 位、滚动 10 分钟轮换、可手动刷新；`POST /pair` 校验后发 128-bit
  设备 token（`paired-devices.json` 只存哈希，可撤销）。**每 IP 限速 + 失败
  锁定必须内建**（4 位仅 10⁴ 组合），锁定只封配对端点不影响已配对设备。
- 数据面鉴权：`ipp://host:port/p/<token>/ipp/print[/key]`，**token 放路径
  而非 header**——Windows IPP Class Driver 不支持自定义 header，URI 路径是
  三平台系统打印栈唯一通用通道。未配对 401；**回环豁免**（与 ADR-0008 D2
  "回环全权"同语义）。mDNS TXT 增加 `dyj=1`、`pair=1`、`dyjver=3`（经
  `BuildTXT` 的 Extra map，不动 wire）。
- 明确安全边界：这是家庭/小办公"防蹭打印"，不是企业安全边界（调研 13 §8-Q4）。

### D4. 接收端 = 系统 IPP 队列（Add-Printer -IppUrl / lpadmin everywhere），dyj 不在打印路径中

装队列由**常驻服务进程代执行**（SYSTEM/root 权限，安装器场景零弹窗）；绿色
场景 Windows 以 `runas` 一次性提权 helper（stdin 传参，token 不进命令行），
Linux/macOS 给 pkexec/命令复制指引。装后 `Get-Printer` 验证 + 一键测试页。
队列 URI 的 host **优先 `<name>.local`**（DHCP 换 IP 免疫），回退 IP；失配时
删旧重建。接收端记录 `added-printers.json` 供管理/诊断。

### D5. Windows 安装器一次提权，之后全自动；配置热生效 = 进程内重建

安装器只做：装文件、防火墙、bootstrap 服务、托盘自启、桌面快捷方式
（`dyj open`）。配置保存后**进程内优雅拆除再重建**（mDNS goodbye → spool
drain → 换实例，HTTP listener 不关），不依赖 SCM 重启权限；SCM"主动退出 +
recovery 自动拉起"仅作兜底。serve 移除 wingdi 缺 `-printer` 的硬失败。

### D6. 多打印机 = per-printer slug 路径 + 每台一个 mDNS 实例 + per-printer spooler

`service.json` 演进为 `printers[]`（旧字段自动迁移）；路由
`/ipp/print/<key>`，单打印机保留 `/ipp/print` 兼容；wingdi/cups 后端按
队列多实例化。排在 P2 实施，P0 先以单打印机构成完整傻瓜闭环。

### D7. 可观测性 = 结构化事件日志（logfmt）+ 文件落盘 + 检查点断言 + doctor 客户端扩展

- 新包 `pkg/logx`：level + `evt=<域>.<动作>` + 固定字段字典；**token/PIN
  永不入日志**（脱敏铁律）；文件日志 `<stateDir>/logs/dyj.log` 10 MB×3 轮转。
- **检查点清单是硬契约**（调研 13 §5.2）：每个自动化步骤（BOOT/CFG/SHARE/
  PAIR/USE/DATA 共 24 项）实施时必须同时落"断言 + 事件 + 失败的用户可见反馈"
  三件套。
- doctor 增加接收端检查（队列存在/URI 可达/token 有效/.local 解析）与
  `server-mdns-egress` 自发自收自测（防调研 12 的 multi-NIC 类缺陷复发）；
  故障树（症状 → 事件 → 处理）随实施进 `docs/troubleshooting.md` 并与
  doctor Fix 互链。

## 备选方案与拒绝理由

- **终端问答向导**（零依赖浏览器）：对双击 exe 的目标用户不友好；Web 栈与
  向导骨架已存在（ADR-0008），重复建设。
- **token 走 HTTP header / mDNS 交付密钥**：Windows IPP Class Driver 不发
  自定义 header；mDNS 无加密通道。URI 路径是唯一通用方案。
- **IP 白名单式配对**（配对即记 IP）：DHCP 环境脆；token 可撤销、可轮换、
  不随地址漂移。
- **接收端常驻代理**（dyj 中转打印）：多一层故障面与兼容性风险；系统队列
  直连才是"像真网络打印机一样"的正确形态。
- **SCM 重启式热生效**：需要服务自停自启的权限语义且体验有黑窗期；进程内
  重建更稳，SCM 仅兜底。

## 后果

- **Breaking（P1 起）**：LAN 上无 token 的旧客户端/脚本对 dyj 服务端的
  IPP 请求开始收到 401（回环不受影响）；`dyj print` 需加 `--token`/自动读
  本机配对记录作过渡，CHANGELOG 显式标注。
- 新增状态文件：`paired-devices.json`、`added-printers.json`、`device-id`、
  `logs/`；`service.json` 增 `role`（P0）与 `printers[]`（P2，带迁移）。
- 新子命令：`dyj open`（打开管理页）；`setup-helper`（内部提权 helper，
  不面向用户）。
- 实施顺序 P0→P3 见调研 13 §6；**P0 动工前先落库当前工作区未提交的三项
  验收修复**（multi-NIC mDNS egress 是接收端扫描可靠性的前提）。
- 开放问题（SCM 退出码、.local 解析、WPPM、双角色交互等）与验证计划见
  调研 13 §8，逐项在对应阶段真机闭环。
