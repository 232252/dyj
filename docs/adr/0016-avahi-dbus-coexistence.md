# ADR-0016：Linux 经 avahi D-Bus 发布服务（mDNS 共存）

- **状态**：已接受
- **日期**：2026-11-13
- **关联**：[ADR-0015](0015-discovery-hardening-and-open-txt.md)、[research/06](../research/06-mdns-coexistence-verified.md)、[research/16](../research/16-cross-machine-field-test.md)

## 背景

真机跨机测试（research/16）发现：Linux 服务端装了 avahi（CUPS 桌面发行版默认
常驻）时，dyj 自管的 mDNS 通告**发得出去但收不到查询**——抓包证实本机的
QM/QU 查询都到达远端，应答却全部来自 avahi，dyj 实例零应答。

根因是 Linux `SO_REUSEPORT` 组播语义：绑定同一组播组+端口的多个 socket，
内核只把入包投递给**一个** socket。avahi 与 dyj 同绑 5353，查询被 avahi 独占，
dyj 的 `serve()` 读不到 → 不应答。Windows 的 `SO_REUSEADDR` 语义不同，故本机
正常。这是 research/06 在 Linux 侧的未覆盖情形。

## 决策

1. **引入 `Publisher` 接口**（`pkg/discovery/publisher.go`）：抽象通告能力
   （Start/Close/Instance/SetTXT/TXT）。两个实现：
   - `*Advertiser`：原有自管 mDNS（Windows/macOS，及无 avahi 的 Linux）。
   - `*avahiPublisher`（仅 Linux）：经系统 avahi 的 D-Bus API 发布。
2. **`NewPublisher` 工厂**（Linux）：avahi 在跑（`Server.GetState()==RUNNING`）
   则走 D-Bus，否则/失败回退自管。node.go 只依赖接口，开放模式 TXT 同步
   （SetTXT）两条路径通用。
3. **avahi D-Bus 实现**（`avahi_linux.go`，签名经 avahi 0.8 内省核实）：
   `EntryGroupNew` → `AddService(iface=-1, proto=-1, flags=0, name, _ipp._tcp,
   local., host, port, txt aay)` + `AddServiceSubtype`（AirPrint，best-effort）
   → `Commit`；TXT 更新用 `UpdateServiceTxt`；关闭用 `Free`。iface/proto=-1
   让 avahi 在所有接口/双栈发布。
4. **依赖**：`github.com/godbus/dbus/v5` 由间接提为直接（原已随 systray 引入，
   无新增模块）。仅 Linux 编译进二进制；go1.20 legacy 通道验证通过。
5. **systemd 单元调整**（`packaging/linux/dyj.service` + postinstall）：
   - **静态专用用户 `dyj`** 取代 `DynamicUser=yes`。dbus 用 `SO_PEERCRED`
     按 UID 校验对端，会重置来自临时 DynamicUser UID 的连接
     （`recvmsg: connection reset by peer`）——这是 avahi 路径在服务态失败的
     真因。postinstall 创建该系统用户。
   - **`BindReadOnlyPaths=/run/dbus`**：`ProtectSystem=strict` 隐藏 /run，
     需只读绑定 D-Bus socket 目录。其余加固不变。

## 后果

- 正面：Linux 桌面（有 avahi）自动发现恢复——真机验证本机 Windows 能 browse
  到服务经 avahi 发布的共享，开放模式 pair TXT 经 `UpdateServiceTxt` 实时翻转。
- 回退：无 avahi 时服务正常运行（`via=self`），但**自管 mDNS 在 Linux 上本就
  不可靠**（同一组播投递限制，research/06）；此场景的发现仍靠深度扫描/按地址
  添加（ADR-0015）。avahi 路径是 Linux 发现的可靠解。
- 取舍：服务从 DynamicUser 改静态用户，牺牲了「无持久用户」的加固性，换取
  D-Bus 可用；状态目录属主随之迁移到 dyj 用户。
- 局限（后续）：avahi 中途崩溃/重启后，已注册的 EntryGroup 失效，当前不自动
  重注册（需重启 dyj 或下次配置保存）；名称冲突由 avahi 自行改名，dyj 暂不
  回读改名结果。两者记为后续增强。