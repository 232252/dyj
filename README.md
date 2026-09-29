# dyj — LAN 打印机共享工具

把接在一台机器上的打印机（Windows 打印队列 / 任意后端）通过局域网共享出去，
让 **Windows、Linux、macOS** 客户端**免装驱动**直接打印。
单个静态二进制（8–9 MB），无运行时依赖。

> 当前阶段：**v0.2 已交付**（服务端 + 客户端 + Web 管理页 + Windows 托盘 +
> cups 真实打印机后端 + 可选 PDF + 三平台安装交付），Debian 14 真机验收通过。
> 决策记录见 `docs/adr/`（0001–0009），调研论据见 `docs/research/`。

## 快速开始

服务端（Windows，打印机已装好驱动）：

```powershell
dyj serve -name "办公室打印机" -printer "EPSON L3250" -port 6631
```

- `-printer` 是 Windows 打印队列名（PowerShell `Get-Printer` 可查）；
- 加 `-tls-port 6632` 会同时提供 ipps://（自签证书）；
- 防火墙放行 TCP 6631 与 UDP 5353（mDNS）；
- 打开后访问**管理页** `http://127.0.0.1:6631/admin`：改配置、看作业和日志、
  暂停共享、自检诊断——局域网内任何设备都能打开（写操作需管理令牌，
  启动横幅会打印一次）。

客户端（任何平台，CUPS 直接添加）：

```bash
# Linux / macOS
lpadmin -p dyj -E -v ipp://192.168.1.10:6631/ipp/print
lp -d dyj notes.txt
```

Windows 11：设置 → 蓝牙和其他设备 → 打印机和扫描仪 → 添加设备 →
手动添加 → **通过 IP 地址添加打印机** → IPP → `ipp://192.168.1.10:6631/ipp/print`

dyj 自带客户端（文本/图片/PDF 自动提交）：

```bash
dyj browse                                # 找到局域网里的打印机
dyj status  192.168.1.10:6631             # 查询能力
dyj print   192.168.1.10:6631 photo.jpg   # 打印（-copies -media -color）
dyj print   192.168.1.10:6631 doc.pdf     # PDF 透传（服务端装了 poppler 才声明）
dyj jobs    192.168.1.10:6631             # 查队列
dyj cancel  192.168.1.10:6631 1           # 取消作业
dyj doctor  --json                        # 诊断本机（防火墙/端口/mDNS/后端）
```

Linux/macOS 服务端把接在本机的真实打印机共享出去（走本地 CUPS 队列，
自动检测；显式指定 `-backend cups -printer <队列名>`）：
`lpstat -p` 可查队列名。

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
cmd/dyj/           入口装配：serve / browse / status / print / jobs / cancel /
                   service / doctor / tray
cmd/dyj-tray/      Windows 托盘交付入口（windowsgui 子系统）
pkg/attributes/    RFC 8010 编解码（唯一 wire 层）+ IPP Everywhere 属性表
pkg/ipp/           HTTP(S) 传输 + 操作语义 + JobSink 接缝（含 /admin 挂载）
pkg/spooler/       作业状态机、流式落盘、崩溃恢复、历史清理、暂停开关
pkg/render/        PWG Raster 编解码、文本布局、图片光栅化、图片 PDF 封装
pkg/driver/        后端接口 + wingdi(Windows GDI) / cups(lp) / file / null
                   + PDF 条件栅格化（poppler）
pkg/admin/         Web 管理页 + /admin/api（回环全权、远程只读+令牌）
pkg/tray/          Windows 系统托盘（状态点、暂停/恢复、自启开关）
pkg/client/        IPP 客户端库
pkg/discovery/     mDNS 通告/浏览（自管 socket，显式选接口）
pkg/doctor/        跨平台诊断检查（CLI 与管理页共用）
pkg/service/       Windows SCM / 加固 systemd / launchd（bootstrap）
packaging/         nfpm(deb/rpm)、Inno Setup(.iss)、Homebrew formula、
                   avahi 服务文件、.desktop、install.sh
docs/adr/          架构决策记录（0001–0009）
docs/research/     调研与实证报告（含 Phase 1 前瞻：10-ui-and-platform-deployment）
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
