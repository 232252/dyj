# dyj — LAN 打印机共享工具

把接在一台机器上的打印机（Windows 打印队列 / 任意后端）通过局域网共享出去，
让 **Windows、Linux、macOS** 客户端**免装驱动**直接打印。
单个静态二进制（8–9 MB），无运行时依赖。

> 当前阶段：**v0.4 已交付**（极简双模式界面：全产品 4 个页面、无侧栏、零设置项；
> 底座为 v0.3 的零配置引导 + 4 位码配对 + 接收端自动装队列 + 多打印机 +
> 全平台构建）。设计见 `docs/research/14` / `docs/adr/0011`，底座设计见
> `docs/research/13` / `docs/adr/0010`，故障排查见 `docs/troubleshooting.md`。
> 决策记录 `docs/adr/`（0001–0011）。

## 快速开始（推荐：向导）

**双击 `dyj`（或安装器创建的「dyj」图标）**——浏览器自动打开，只有两张卡：

- **服务端**：自动列出本机所有打印机（PDF/XPS 等虚拟打印机默认隐藏，已默认
  全选）→「一键共享」→ 完成后**永远停在这一页**：4 位配对码大字（托盘菜单
  也常驻可见）、共享清单逐台开关、连上的电脑、最近打印。之后 dyj 在后台
  自行运行（安装器装好系统服务 + 托盘）。
- **客户端**：自动扫描局域网（顺带也能添加任何 IPP 网络打印机）→ 点一台 →
  输对方屏幕上的 4 位配对码 → dyj 自动把它装成**系统打印队列**——之后
  WPS/Word/记事本的打印对话框直接可选，**dyj 不需要保持运行**。装完可一键
  打印测试页；"本机已安装"列表每台显示实时状态（就绪/服务端不在线/配对被撤销）。

没有设置页：端口、驱动、防火墙、连接地址全部自动适配。技术信息只住在右上角
**「帮助」**里——常见问题、运行自检（`dyj doctor` 同款检查）、一键**导出诊断包**
（日志 + 自检结果 + 配置打成一个 zip，发给能帮你的人即可）。

## 快速开始（命令行，高级路径）

服务端（Windows，打印机已装好驱动）：

```powershell
dyj serve -name "办公室打印机" -printer "EPSON L3250" -port 6631
```

- `-printer` 是 Windows 打印队列名（PowerShell `Get-Printer` 可查）；
- 加 `-tls-port 6632` 会同时提供 ipps://（自签证书）；
- 防火墙放行 TCP 6631 与 UDP 5353（mDNS）；
- 打开后访问**管理页** `http://127.0.0.1:6631/admin`：角色主页（服务端 = 配对码 +
  共享清单 + 连上的电脑 + 最近打印；客户端 = 扫描添加 + 已安装列表）+ 右上角
  「帮助」（自检 / 诊断导出 / 日志 / 关于）——局域网内任何设备都能打开
  （写操作需管理令牌）。

dyj 自带客户端（文本/图片/PDF 自动提交；本机回环免配对，局域网对端需先配对）：

```bash
dyj browse                                # 找到局域网里的打印机
dyj status  192.168.1.10:6631             # 查询能力
dyj print   192.168.1.10:6631 photo.jpg   # 打印（-copies -media -color）
dyj print   192.168.1.10:6631 doc.pdf     # PDF 透传（服务端装了 poppler 才声明）
dyj jobs    192.168.1.10:6631             # 查队列
dyj cancel  192.168.1.10:6631 1           # 取消作业
dyj doctor  --json                        # 诊断共享端（防火墙/端口/mDNS/后端）
dyj doctor  --client --json               # 诊断接收端（队列/可达/配对有效性）
```

Linux/macOS 服务端把接在本机的真实打印机共享出去（走本地 CUPS 队列，
自动检测；显式指定 `-backend cups -printer <队列名>`）：
`lpstat -p` 可查队列名。

### 数据面鉴权（v0.3 起）

- **本机回环**：完全放行（与打印机接在本机同级信任）。
- **局域网对端**：默认拒绝；用向导配对一次（4 位码换设备令牌，令牌藏在打印队列
  的 URI 路径里——Windows IPP Class Driver 不支持自定义 header），随时可在管理页
  「已配对设备」里撤销。

## 协议与兼容

- **IPP 2.0 + IPP Everywhere 属性表**（PWG 5100.14）+ **mDNS/DNS-SD**（`_ipp._tcp`）。
  这是三平台原生支持的唯一免驱组合；微软官方时间表正在淘汰第三方打印驱动，
  IPP 路线价值随时间上升（见 `docs/research/04`）。
- 服务端实现：Get-Printer-Attributes / Print-Job / Validate-Job /
  Create-Job + Send-Document（多文档）/ Get-Jobs / Get-Job-Attributes /
  Cancel-Job / Identify-Printer；gzip 文档压缩；`ipp-attribute-fidelity` 校验。
- **只声明真的能渲染的格式**（ADR-0007 诚实规则）：`image/pwg-raster`、
  `image/jpeg`、`image/png`、`text/plain` 永远在表中；`application/pdf`
  **仅当机器上装有 poppler（pdftoppm）时追加声明**（外部栅格化，无 AGPL 进程内
  代码，mDNS `pdl=` 承诺同步）。`image/urf` 仍不声明（无权威规范原文）。
- **真实打印路径复用系统驱动链**：Windows 走 GDI（wingdi），
  Linux/macOS 走本地 CUPS 队列（cups 后端，光栅页打包为图片 PDF 提交）——
  dyj 在所有平台零驱动零 PPD。

## 交付物

| | |
| --- | --- |
| 二进制 | `dist/dyj-{windows,linux,darwin}-{amd64,arm64}[.exe]`，8–9 MB，纯静态 |
| 服务化 | `dyj service install`（Windows SCM 服务 / systemd / launchd） |
| 测试 | `go test ./...`（单元 + 进程内 E2E）；`scripts/e2e.sh`（二进制级 E2E） |
| 构建 | `scripts/build-all.sh`（交叉编译 6 目标） |

## 仓库结构

```
cmd/dyj/           入口装配：无参引导（角色向导）/ open / setup-helper /
                   serve / browse / status / print / jobs / cancel /
                   service / doctor [--client] / tray
cmd/dyj-tray/      Windows 托盘交付入口（windowsgui 子系统）
pkg/attributes/    RFC 8010 编解码（唯一 wire 层）+ IPP Everywhere 属性表
pkg/ipp/           HTTP(S) 传输 + 操作语义 + JobSink 接缝 + 多打印机路由
                   （/p/<token> 配对门禁）
pkg/spooler/       作业状态机、流式落盘、崩溃恢复、历史清理、暂停开关
pkg/render/        PWG Raster 编解码、文本布局、图片光栅化、图片 PDF 封装
pkg/driver/        后端接口 + wingdi(Windows GDI) / cups(lp) / file / null
                   + PDF 条件栅格化（poppler）
pkg/admin/         Web 管理页 + /admin/api（回环全权、远程只读+令牌；
                   Extra 端点挂向导/配对/装队列 API）
pkg/pairing/       4 位滚动 PIN、限速锁定、设备令牌存储与撤销
pkg/localprint/    本机打印机枚举（EnumPrintersW / lpstat）+ 虚拟过滤
pkg/sysqueue/      系统队列安装/删除/验证（Add-Printer -IppUrl / lpadmin，
                   含一次性提权 helper）
pkg/logx/          结构化事件日志 + 轮转文件 sink（token/PIN 脱敏）
pkg/tray/          Windows 系统托盘（状态、配对码、暂停/恢复、自启开关）
pkg/client/        IPP 客户端库
pkg/discovery/     mDNS 通告/浏览（自管 socket，显式选接口，TXT 配对标记）
pkg/doctor/        跨平台诊断检查（CLI 与管理页共用，含 mDNS 出口自测）
pkg/service/       Windows SCM / 加固 systemd / launchd（bootstrap）
packaging/         nfpm(deb/rpm)、Inno Setup(.iss)、Homebrew formula、
                   avahi 服务文件、.desktop、install.sh
docs/adr/          架构决策记录（0001–0010）
docs/research/     调研与实证报告（13 = v0.3 零配置设计）
docs/troubleshooting.md   症状 → 事件 → 处理 的故障树
e2e/               进程内全管线测试
scripts/           build-all.sh、package-linux.sh、e2e.sh
.github/           release 工作流（tag → 全产物）
```

价值观（硬约束，PR 评审按此判定）：**体积小 · 好维护 · 长眼光（押标准协议）· 无巨石**。

## 贡献

- 提交信息使用 [Conventional Commits](https://www.conventionalcommits.org/)。
- 每个非平凡决策写一份 `docs/adr/NNNN-title.md`。
- 动 wire 格式的改动必须带黄金字节测试；`go test ./...` 与 `scripts/e2e.sh` 必须全绿。

## 许可证

Apache-2.0 —— 见 [ADR-0003](docs/adr/0003-license.md)。
