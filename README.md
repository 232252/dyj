# dyj — LAN 打印机共享工具

把接在一台机器上的打印机（USB / 本地）通过局域网共享出去，让 **Windows、Linux、macOS、iOS**
客户端无需安装厂商驱动即可直接打印。

> 当前阶段：**调研中（Phase 0）**。仓库尚无生产代码，只有调研文档与决策记录。
> 请先读 `docs/research/` 再动代码。

## 项目价值观（硬约束，PR 评审按此判定）

| 约束 | 含义 | 反面例子（拒绝） |
| --- | --- | --- |
| **体积小** | 静态二进制/镜像尽量小；运行时依赖越少越好 | 拖一个 500MB 的 JRE/Python 运行时 |
| **好维护** | 模块边界清晰，单个文件小，可单独测试 | 一个 5000 行的 `server.py` |
| **长眼光** | 押注有 RFC/标准背书的协议（IPP、mDNS/DNS-SD），不押私有协议 | 自造二进制协议 / 依赖会被删除的厂商 API |
| **无巨石** | 单体可执行文件 + 明确的内部分层；扩展走 **插件/后端接口**，而不是往核心塞 `if` | 把所有厂商怪癖硬编码进核心调度器 |

## 目标使用场景

```
┌──────────────┐   USB   ┌─────────────────────┐   LAN (mDNS 自动发现)   ┌─────────────┐
│   打印机      │◄───────►│  本机 (host)         │◄──────────────────────►│  Win 客户端  │
│ (USB/并口)    │         │  dyj 打印服务        │    IPP / AirPrint      │  Linux 客户端│
└──────────────┘         │  :631 IPP + mDNS     │                        │  macOS/iOS  │
                         └─────────────────────┘                        └─────────────┘
```

核心目标：**客户端零驱动安装**。macOS/iOS 走 AirPrint，Windows 10/11 走内置 IPP 客户端，
Linux 走 CUPS 的 `ipp://` 后端。

## 仓库结构（规划）

```
docs/
  research/          调研报告（Phase 0 产物）
  adr/               架构决策记录 (Architecture Decision Record)
cmd/ or src/         入口/装配层 —— 只做依赖注入，不放业务逻辑
internal/            各功能模块（见下）
```

**"无巨石"的落地方式**：核心不是一个大进程里堆功能，而是拆成职责单一的模块，
彼此只通过**明确接口**通信；驱动适配走**插件接口**（对应 CUPS 的 backend/filter 模型）。

```
transport(监听/TLS)   ipp(协议编解码)   discovery(mDNS/DNS-SD)
      │                     │                   │
      └──────────┬──────────┴─────────┬─────────┘
                 ▼                    ▼
            jobspool(队列/持久化)   driver(打印机适配/插件)
```

## 快速开始

尚未实现。计划中的用法（占位，未验证）：

```bash
dyj serve --printer usb:/dev/usb/lp0 --name "Office Printer"
```

## 贡献

- 提交信息使用 [Conventional Commits](https://www.conventionalcommits.org/)。
- 每个非平凡决策写一份 `docs/adr/NNNN-title.md`。
- 不要提交真实凭据；`.env` 已在 `.gitignore` 中。

## 许可证

尚未确定 —— 见 [ADR-0002](docs/adr/0003-license.md)（待决，涉及 CUPS/GPL 传染性评估）。
