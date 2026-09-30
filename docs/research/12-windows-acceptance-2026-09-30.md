# 附录 G：Windows 物理机验收（2026-09-30，v0.2 后）

真机交叉验收记录。环境：Windows 11 本机（192.168.110.40，主机名中文"生命"，
多网卡：以太网 192.168.110.0/24 + VPN 10.126.126.4 + 内网 10.2.3.x）×
Debian 14 真机 el（192.168.110.214 / VPN 10.126.126.1，9 月 29 日交付时部署的
dyj file 后端服务仍在运行）。**全程无纸**：Windows 侧用 NUL 端口隔离队列，
Linux 侧沿用 file 后端；10.126.126.2（Windows，真实打印机宿主）因无 SSH 凭据
未纳入（见 §4 交接 runbook）。

## 1. 结果总览

| 项 | 结果 | 证据 |
| --- | --- | --- |
| wingdi 真实驱动链（GDI→Epson ESC/P-R V4 类驱动→NUL） | ✅ | 作业 1–5 全部 state 9 completed；-copies 2 走 DEVMODE 补丁 |
| 文本/JPEG/PNG/PWG-Raster 四种输入 | ✅ | spool-w1/job-000001..000005 meta.json |
| TLS（ipps:// 自签） | ✅ | 作业 4 经 ipps://127.0.0.1:6642 提交完成 |
| Windows dyj 客户端 → Linux 服务端 | ✅ | el /tmp/dyj-e2e/out 新增 job-000004/000005 PNG |
| Linux dyj 客户端 → Windows 服务端 | ✅ | from-el.txt → job-000005 (w1) / job-000002 (w2) |
| **CUPS IPP Everywhere 客户端 → wingdi**（`-m everywhere` 自动 PPD） | ✅ | el 队列 dyj-win，作业 6 cups2win.txt completed |
| mDNS 双向发现 | ✅（修复后） | el avahi-browse 列出 dyj-win-accept/dyj-win-file；el CUPS 自动生成 implicitclass 临时队列 |
| file 后端（Windows） | ✅ | dyj-out/fileout/job-*.png |
| PDF 诚实拒绝（两端均无 poppler） | ✅ | `ipp-attribute-fidelity` 0x040B 干净报错，不静默损坏 |
| 管理页 UI（向导/状态/作业，品牌化） | ✅ | 截图验收，shadow DOM 渲染正常 |
| 管理 API 鉴权矩阵 | ✅ | 远程读 200；无令牌写 401；令牌写 200；回环免令牌；暂停后打印 0x0506 拒绝 |
| UI 远程写流（令牌保存→暂停→恢复，含确认框） | ✅ | accepting:false → true |
| doctor --json | ✅ | 8 项检查输出正常 |
| 托盘进程 | ✅ | dyj-tray-windows-amd64.exe 启动存活（图标交互留人工） |
| `service install`（SCM） | ⏸ 需管理员 | 未提权终端干净报错；随 .2 部署一并做 |
| Windows 原生 IPP（设置→IPP 添加） | ⏸ 需管理员 | `Add-Printer -IppUrl` 未提权拒绝访问；用户手工路径即 README 流程 |

## 2. 验收中发现并修复的缺陷（均已回归验证）

1. **中文主机名 + `-tls-port` 启动崩溃**：证书 DNS SAN 直接放 `os.Hostname()`
   （"生命"），x509 要求 IA5String。修复：`asciiHostName` 折叠 + `-hostname`
   接入证书（`cmd/dyj/serve.go`，含 CJK 回归测试）。
2. **多网卡 Windows 的 mDNS 通告出不了网**：每接口 socket 绑通配地址时
   Windows 忽略 `IP_MULTICAST_IF` 出口选择，通告全走默认路由网卡——
   el 的 python 原始抓包 30s 零包；最小 Go 复现对比确认
   （通配+选项=不可达，绑接口地址=全部到达）。修复：`pkg/discovery/sock_*.go`
   按接口地址 bind（Linux 同步加固）。
3. **`dyj status` formats 只显示第一个值**（显示缺陷，表实为四格式，
   ipptool 权威确认）。

## 3. 遗留与建议

- el 上常驻的 dyj 进程是 9 月 29 日 07:13 的**旧构建**（无管理页：banner 无
  admin 行，`/admin/` 落到 IPP 信息页）。建议以 v0.2+ 二进制重启
  （需 root：`systemctl`/`kill` 后重跑原命令加 `-backend file` 原参数即可）。
- 作业页"份数"列恒为 0（job 1 实为 -copies 2）——纯展示小缺陷，待修。
- 客户端发 PDF 前可先读 `document-format-supported` 本地预检，替代服务端
  fidelity 拒绝（体验优化，非缺陷）。
- `service install` 与 Win11 设置-IPP 添加两项待管理员会话补测。

## 4. 10.126.126.2 交接 runbook（Windows + 真实打印机）

现状：mesh 内 .1/.4/.6 探测 22/631/8080/9443/6631/6632 均"通但应用层静默"
（SSH 正常应答，其余端口 TCP 握手后无数据——疑似 mesh 客户端对无本地监听
端口的假握手）。**结论：.2 上没有可用的 dyj 监听**；SSH（OpenSSH for
Windows 9.5）拒绝全部既有公钥（root/admin/admina × 4 key）。

拿到凭据后按序执行（管理员 PowerShell）：

```powershell
# 0) 装公钥（任选其一：把本机 id_ed25519.pub 追加进去）
Add-Content C:\ProgramData\ssh\administrators_authorized_keys (Get-Content ~\.ssh\id_ed25519.pub)

# 1) 确认真实打印机队列名
Get-Printer

# 2) 无纸隔离队列（复用真实打印机驱动，绝不碰纸；NUL 端口免提权可建）
Add-PrinterPort -Name 'NUL:'
Add-Printer -Name 'dyj-accept' -DriverName '<真实打印机的 DriverName>' -PortName 'NUL:'

# 3) 防火墙放行（一次性）
netsh advfirewall firewall add rule name="dyj IPP" dir=in action=allow program="<dyj.exe 路径>" protocol=TCP localport=6631 profile=domain,private,public
netsh advfirewall firewall add rule name="dyj mDNS" dir=in action=allow program="<dyj.exe 路径>" protocol=UDP localport=5353

# 4) 启动（先无纸验收；纸路验收需人值守时再把队列换回真实端口）
dyj serve -backend wingdi -printer dyj-accept -name dyj-win2 -port 6631 -tls-port 6632

# 5) 验证（从 el 或本机）
dyj status 10.126.126.2:6631
dyj print  10.126.126.2:6631 note.txt
dyj service install   # SCM 服务化（管理员）
```
