# dyj 故障排查（症状 → 定位 → 处理）

本文是 v0.3 可观测性契约的调试入口（research/13 §5.6，ADR-0010 D7）。每条给出：先看什么
（日志事件 / doctor 检查）、常见原因、处理办法。日志文件在 `<stateDir>/logs/dyj.log`
（Windows `%ProgramData%\dyj\logs\`，Linux `/var/lib/dyj/logs/`，macOS
`/Library/Application Support/dyj/logs/`），事件为 `key=value` 格式，可直接搜索
`evt=域名.动作`。

## 快速命令

| 命令 | 用途 |
| --- | --- |
| `dyj doctor` | 共享端自检（端口/防火墙/后端/mDNS 出口） |
| `dyj doctor --client` | 接收端自检（队列存在/共享端可达/配对有效） |
| `dyj browse` | 列出局域网 IPP 广播（不含本机自己，Windows 限制） |
| `dyj list-interfaces` | 组播网卡清单（多网卡机器选网卡用） |
| 管理页 → 日志 | SSE 实时日志（`evt=` 前缀过滤） |

## 症状 1：接收端扫描不到任何打印机

- **先看**：接收端管理页 → 添加打印机（事件 `scan.result`）；共享端 `mdns.announce`。
- **先判定**：两端是否在**同一二层广播域**（同一 WiFi/同一路由器 LAN 口）。mDNS 是
  链路本地组播，**只有 mesh/VPN 相连（如 10.126.126.x）不属于"同一网络"**，跨网段
  扫不到是协议边界，不是故障——分析与修复设计见
  [research/15](research/15-discovery-cross-subnet-mesh.md)。
- **原因与处理**：
  1. 两台设备不在同一网段 / 只经 mesh-VPN 相连 / 路由器开了 AP 隔离 → 换同一 WiFi；
     mesh-only 场景等"按地址添加"入口（research/15 方案 P1）。
  2. 共享端防火墙拦 UDP 5353 → `dyj doctor` 的 firewall 检查给出 netsh 命令（三 profile 都要放行）。
  3. 多网卡机器广播走错网卡 → `dyj list-interfaces` + `-iface` 指定（Linux 的 doctor
     `mdns-egress` 检查会直接 FAIL；Windows 受平台限制只能跨机验证）。
  4. 共享端根本没共享 → 管理页状态页看"正在共享的打印机"是否为空。
  5. 共享端是旧构建（无 `dyj=1`/`pair=1` TXT 标记）→ UI 会显示为"普通打印机"且
     配对协议不匹配 → 先升级共享端二进制（如 el 的 9/29 旧 v0.2，research/12 §3）。

## 症状 2：扫到了但列表标"不可达"

- **先看**：`scan.probe` 事件（TCP 探活失败地址）。
- **原因与处理**：共享端防火墙拦 TCP 6631（安装器已预放行；绿色运行需手动）→ doctor
  firewall 检查的 netsh 命令。

## 症状 3：配对码总不对 / 配不上

- **先看**：共享端 `pair.attempt`（每次尝试含来源 IP 与结果）、`pair.pin.rotate`。
- **原因与处理**：
  1. 码刚轮换（10 分钟滚动）→ 接收端重新看一眼共享端屏幕，重输。
  2. 连续输错 10 次，IP 被锁 30 分钟 → 界面会显示剩余等待秒数（`pair.lock` 事件）；
     等待或在共享端点"换一个"并不能解锁（锁定按 IP，不随 PIN 消失）。
  3. 输的是另一台 dyj 的码 → 核对管理页顶部的机器名。

## 症状 4：配对成功但装队列失败

- **先看**：接收端 `queue.install` 事件（退出码 + 工具输出摘要，token 已脱敏）。
- **原因与处理**：
  1. UAC 弹窗被拒 → 向导里重试一次，弹窗点"是"（绿色免安装场景每次配对只弹这一次）。
  2. 组策略 / Windows Protected Print Mode 拦截 Add-Printer → 输出里会有具体错误。
  3. 队列重名 → dyj 自动加 ` (2)` 后缀，一般不会失败。

## 症状 5：装好了，WPS/Word 打印没反应

- **先看**：接收端 `dyj doctor --client`（三条检查一步定位）；共享端 `deny.ipp` /
  `job.recv` 事件。
- **原因与处理**：
  1. **队列在但共享端不可达**（对方关机/换 IP）→ `client-uri-reachable` FAIL → 重新扫描重装。
  2. **配对被撤销** → `client-token-valid` FAIL（共享端返回 401）→ 拿新配对码重新配对。
  3. **服务端暂停共享** → 共享端管理页恢复。
  4. **作业在打但慢**（大图渲染）→ 共享端管理页作业页看状态。
  5. 队列显示"脱机"且共享端换了 IP → 重开一次 dyj 管理页会自动重建队列。

## 症状 6：打出来乱码 / 空白

- **先看**：`job.recv`（提交格式）与 `job.render` 事件。
- **原因与处理**：客户端声明格式与实际不符（罕见，诚实声明语义见 ADR-0007）→
  按事件里的 `fmt` 换格式重打；仍异常时带 `job` 号反馈。

## 症状 7：打开 dyj 看不到向导 / 管理页打不开

- **先看**：`boot.listen` 事件。
- **原因与处理**：配置端口被占（常见：旧实例还在跑）→ dyj 自动换到 端口+1..+9，
  banner/`dyj open` 会显示实际端口；`dyj open` 自动探测活动端口。

## 事件速查（研究 13 §5.2 检查点全集）

| 域 | 事件 | 含义 |
| --- | --- | --- |
| boot | `boot.listen` / `boot.ready` / `boot.stop` | 端口就绪 / 全栈就绪 / 停机 |
| cfg | `cfg.save` / `cfg.rebuild` | 配置保存并热生效 / 重建异常 |
| share | `share.enumerate` / `share.probe` / `share.backend` | 本机枚举 / 逐台探测 / 后端就绪 |
| mdns | `mdns.announce` / `mdns.conflict` | 逐实例宣告（含网卡数）/ 名字冲突自动改名 |
| pair | `pair.pin.rotate` / `pair.attempt` / `pair.lock` / `pair.unlock` / `pair.ok` / `pair.req` / `pair.revoke` | 配对全生命周期（尝试事件含来源 IP，永不记录 PIN/token） |
| scan | `scan.result` / `scan.probe` | 接收端扫描汇总 / 逐地址探活 |
| queue | `queue.install` / `queue.remove` / `queue.verify` | 装队列（含退出码）/ 删除 / 装后验证 |
| job | `job.recv` / `job.spool` / `job.render` / `job.print` / `job.done` / `job.fail` / `job.testpage` | 作业生命周期 |
| deny | `deny.ipp` | 未配对来源被拒（路径已脱敏） |

> 脱敏铁律：配对 token 与 PIN 永不出现在任何日志行（logx 层兜底 `RedactPath`）。
