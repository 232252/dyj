# dyj — LAN 打印机共享工具

把接在一台机器上的打印机（Windows 打印队列 / 任意后端）通过局域网共享出去，
让 **Windows、Linux、macOS** 客户端**免装驱动**直接打印。
单个静态二进制（8–9 MB），无运行时依赖。

> 当前阶段：**可用的 v0.1 已交付**（服务端 + 客户端 + 服务化 + 全平台产物）。
> 决策记录见 `docs/adr/`，调研论据见 `docs/research/`。

## 快速开始

服务端（Windows，打印机已装好驱动）：

```powershell
dyj serve -name "办公室打印机" -printer "EPSON L3250" -port 6631
```

- `-printer` 是 Windows 打印队列名（PowerShell `Get-Printer` 可查）；
- 加 `-tls-port 6632` 会同时提供 ipps://（自签证书）；
- 防火墙放行 TCP 6631 与 UDP 5353（mDNS）。

客户端（任何平台，CUPS 直接添加）：

```bash
# Linux / macOS
lpadmin -p dyj -E -v ipp://192.168.1.10:6631/ipp/print
lp -d dyj notes.txt
```

Windows 11：设置 → 蓝牙和其他设备 → 打印机和扫描仪 → 添加设备 →
手动添加 → **通过 IP 地址添加打印机** → IPP → `ipp://192.168.1.10:6631/ipp/print`

dyj 自带客户端（文本/图片自动转 PWG 光栅后提交）：

```bash
dyj browse                                # 找到局域网里的打印机
dyj status  192.168.1.10:6631             # 查询能力
dyj print   192.168.1.10:6631 photo.jpg   # 打印（-copies -media -color）
dyj jobs    192.168.1.10:6631             # 查队列
dyj cancel  192.168.1.10:6631 1           # 取消作业
```

## 协议与兼容

- **IPP 2.0 + IPP Everywhere 属性表**（PWG 5100.14）+ **mDNS/DNS-SD**（`_ipp._tcp`）。
  这是三平台原生支持的唯一免驱组合；微软官方时间表正在淘汰第三方打印驱动，
  IPP 路线价值随时间上升（见 `docs/research/04`）。
- 服务端实现：Get-Printer-Attributes / Print-Job / Validate-Job /
  Create-Job + Send-Document（多文档）/ Get-Jobs / Get-Job-Attributes /
  Cancel-Job / Identify-Printer；gzip 文档压缩；`ipp-attribute-fidelity` 校验。
- **只声明真的能渲染的格式**：`image/pwg-raster`、`image/jpeg`、`image/png`、
  `text/plain`（自研光栅管线，见 ADR-0007）。PDF/URF 未声明——虚假声明会让客户端
  发来打不出的作业，比不支持更糟。
- Windows 投递走 **GDI 打印路径**：复用系统已装厂商驱动做光栅化，dyj 零驱动零 PPD。

## 交付物

| | |
| --- | --- |
| 二进制 | `dist/dyj-{windows,linux,darwin}-{amd64,arm64}[.exe]`，8–9 MB，纯静态 |
| 服务化 | `dyj service install`（Windows SCM 服务 / systemd / launchd） |
| 测试 | `go test ./...`（单元 + 进程内 E2E）；`scripts/e2e.sh`（二进制级 E2E） |
| 构建 | `scripts/build-all.sh`（交叉编译 6 目标） |

## 仓库结构

```
cmd/dyj/           入口装配：serve / browse / status / print / jobs / cancel / service
pkg/attributes/    RFC 8010 编解码（唯一 wire 层）+ IPP Everywhere 属性表
pkg/ipp/           HTTP(S) 传输 + 操作语义 + JobSink 接缝
pkg/spooler/       作业状态机、流式落盘、崩溃恢复、历史清理
pkg/render/        PWG Raster 编解码、文本布局、图片光栅化
pkg/driver/        后端接口 + wingdi(Windows GDI) / file / null
pkg/client/        IPP 客户端库
pkg/discovery/     mDNS 通告/浏览（自管 socket，显式选接口）
pkg/service/       Windows SCM / systemd / launchd
docs/adr/          架构决策记录（0001–0007）
docs/research/     Phase 0 调研与实证报告（含对抗性核查）
e2e/               进程内全管线测试
scripts/           build-all.sh、e2e.sh
```

价值观（硬约束，PR 评审按此判定）：**体积小 · 好维护 · 长眼光（押标准协议）· 无巨石**。

## 贡献

- 提交信息使用 [Conventional Commits](https://www.conventionalcommits.org/)。
- 每个非平凡决策写一份 `docs/adr/NNNN-title.md`。
- 动 wire 格式的改动必须带黄金字节测试；`go test ./...` 与 `scripts/e2e.sh` 必须全绿。

## 许可证

Apache-2.0 —— 见 [ADR-0003](docs/adr/0003-license.md)。
