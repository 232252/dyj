# 附录 I：真机跨机测试（Windows ↔ Debian）—— 0.6.0 现场验证

**状态**：✅ 实测（2026-11-13）。Windows 11（192.168.110.40）↔ Debian forky
（192.168.110.214，主机名 debian，avahi-daemon 常驻）。远端原跑 v0.4.3 系统
服务（DynamicUser 沙箱），测试升级到 0.6.0。

## 1. 验证通过的能力

| 能力 | 结果 | 证据 |
| --- | --- | --- |
| 开放打印模式（免配对） | ✅ | 开窗前本机直连 `dyj status` 返回 401；远端 `POST /admin/api/open` 开窗后，本机免配对 `status`/`print` 全部放行 |
| 跨机完整打印管线 | ✅ | 本机 `dyj print 192.168.110.214:6631 note.txt` → 远端 job 1 `completed`，file 后端落成 `job-000001-doc0-page001.png`（55987 字节） |
| 深度扫描（跨网段兜底） | ✅ | 本机 `POST /admin/api/deep-scan {cidr:192.168.110.0/24}` → 命中 `192.168.110.214:6631 NoSpacePrinter`（IPP 确认，绕过 mDNS） |
| 数据面门禁 | ✅ | 关闭开放模式时局域网裸 IPP 一律 401；回环管理面免令牌 |

## 2. 修复的真实 bug：启动顺序竞态（port 0）

**症状**：远端服务启动后 `mdns.announce ... err="discovery: invalid port 0"`，
status 里打印机 URI 全是 `ipp://host:0/...`，局域网完全发现不到。

**根因**：`runNode` 先调 `buildShare()`（内部 `startMDNS` 并把 `n.actualPort`
烤进打印机 URI 与通告），但 `n.actualPort` 直到后面 `listenUpward` 才赋值。
首次启动时端口=0 → 通告器 `New()` 校验失败、URI 端口为 0。

**修复**：把 `listenUpward` + `n.actualPort` 赋值提前到 `buildShare()` 之前
（`cmd/dyj/node.go`）。回归测试 `TestBootPrinterURIsHaveRealPort` 钉死。
修复后远端 URI 正确（`ipp://192.168.110.214:6631/...`），通告日志无 invalid port。

## 3. 环境兼容缺陷：avahi 共存（已修复，ADR-0016）

> **2026-11-13 已实施**：采用方案 A——检测到 avahi 时经 D-Bus 发布服务
> （`pkg/discovery/avahi_linux.go` + `Publisher` 接口），无 avahi 回退自管。
> 服务态另需静态用户（DynamicUser 的临时 UID 被 dbus SO_PEERCRED 拒绝）+
> `BindReadOnlyPaths=/run/dbus`，已固化进打包单元。真机复验：本机 Windows
> 能 browse 到服务经 avahi 发布的共享，开放模式 pair TXT 经 UpdateServiceTxt
> 实时翻转（pair=1→0→1）。详见 [ADR-0016](../adr/0016-avahi-dbus-coexistence.md)。
> 以下为原始诊断记录。

**症状**：远端 avahi-daemon 常驻时，dyj 的 mDNS 通告**发得出去但收不到查询**
——本机与远端自己都 browse 不到 dyj 共享的打印机，只能看到 avahi 代理的 CUPS
广播（`dyj on debian`）。Windows 无 avahi，本机通告正常。

**抓包证据**（远端 enp2s0，udp 5353）：本机 QM/QU 查询都到达远端，但应答全部
来自 avahi（`192.168.110.214.5353`，CUPS 记录）；dyj 的 `NoSpacePrinter` 实例
**零应答**。dyj 日志称 `mdns.announce ifaces=8` 成功，实则监听 socket 收不到包。

**根因判断**：Linux `SO_REUSEPORT` 对组播只把包投递给绑定组播组的**一个**
socket。avahi 与 dyj 同绑 5353，组播查询被 avahi 独占，dyj 的 `serve()` 读不到
查询 → 不应答。这是 research/06「mDNS 共存」在 Linux 侧的未覆盖情形（Windows
的 SO_REUSEADDR 语义不同，故本机正常）。

**影响**：凡 Linux 服务端装了 avahi（CUPS 桌面发行版默认），dyj 自动发现失效。
**兜底已可用**：深度扫描（§1）与按地址添加不受影响，直连 IPP 完全正常。

**候选方案**（待决策，未实施）：
- A. 检测到 avahi 时经 D-Bus 用 avahi 发布服务（标准共存做法，引入 D-Bus 依赖）；
- B. 文档明示「Linux 服务端建议停用 avahi 或改用深度扫描/按地址添加」；
- C. 研究 dyj 监听 socket 的组播绑定方式，争取与 avahi 同时收包（内核语义限制，
  未必可行）。

## 4. 测试残留与恢复

- 远端 `/usr/bin/dyj` 已升级 0.6.0；旧版备份在 `/usr/bin/dyj.bak-043`。
- 远端共享配置 `/var/lib/private/dyj/service.json`（file 后端 Test Printer，
  输出/缓存在状态目录内）；测试用手动进程已停，正式服务 active。
- 远端 `/home/admina/dyj-test/` 留有测试二进制与输出 PNG（可清理）。
- 开放窗口已 DELETE 关闭；本机测试实例与临时文件已清理。