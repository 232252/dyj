# 17 · HarmonyOS 系统打印接管：取证与实验

日期：2026-11-09（调研），实验进行中
关联：ADR-0012（被部分取代的方向）、ADR-0013（新决策）、调研 12 §CUPS 验证、调研 16 §Harmony 接收端

## 0. 一句话结论

三方 App 无法在 HarmonyOS 上注册"打印服务扩展"（systemapi 关死），但**系统自带打印栈是 CUPS 血统、有"设置 → 打印机和扫描仪 → 添加（自动搜索/IP添加）"入口**；dyj 服务端 IPP Everywhere 已经齐活，唯一冲突是 401 配对门禁——解法是"**配对即信任设备**"（App 配对一次 → 服务端记信该设备 IP → 之后免令牌 IPP，系统直连）。服务端信任已实现（commit 196f55f），真机实验见 §4。

## 1. 三方应用的路：关死

| 途径 | 结论 | 出处 |
| --- | --- | --- |
| `PrintExtensionAbility`（对标 Android PrintService，打印厂商扩展走的路） | **系统接口，仅系统应用**，API 10 起 | OpenHarmony 6.0 文档目录标注"(系统接口)"；bundleManager 文档"仅系统应用支持"；docs 仓 `PrintExtensionAbility-sys.md` 命名 |
| `@ohos.print`（应用侧） | `print.startPrint` 等可发起打印（`ohos.permission.PRINT` 为 normal 级）；打印机注册/管理侧接口（addPrinter 等）为系统接口 | 权限列表 permissions-for-all；`js-apis-print-sys.md` |
| `ohos.permission.PRINTER_DRIVER` | 受限开放权限，**仅 PC/2in1**，起始 26.0.0，需华为审批 | 受限权限列表 restricted-permissions.md |
| 华为生态现状 | 非认证厂商走"应用内打印"（如智睦云打印）或厂商驱动 App；系统打印面板的打印机来源是系统自己的打印服务 | 华为消费者支持文档 zh-cn16091885 |

推论：**dyj 想让打印机出现在系统打印面板，唯一路径是让系统自带打印客户端直接连 dyj 服务端**（把自己伪装成一台"普通网络打印机"——本来就是）。

**2026-11-09 二次复核**：`@ohos.print` 的 `addPrinter` / `deletePrinter` / `startDiscoverPrinter` 等打印机管理接口仍标注 systemapi（仅系统应用），未查到任何开放计划；三方应用侧可用的只有发起打印（`print.print` 等，normal 级权限）。厂商接入路径仍是预置/审批的打印扩展（print_print_fwk 的发现指令链：PrintSpooler → PrintSA → PrintExtension → CustomPrintExtension）。结论不变，且进一步排除"等开放"的侥幸。

## 2. 系统打印入口：存在且是 CUPS 血统

- 华为官方指南：**设置 → 打印机和扫描仪 → 添加打印机和扫描仪**，支持自动搜索与 **IP添加**（名称+IP）。
- OpenHarmony 打印架构（论坛《打印系统简介》+ print_print_fwk）：打印预览应用自己做打印机发现（**P2P & mDNS**），打印后台基于 CUPS 构建。
- **本机取证（LRT-W20 平板，API 26）**：`ps -ef` 可见 `print_service`（打印框架服务）、`cupsd -f -c /etc/cups/cupsd.conf`、`scan_service`/`sane_service`，mDNS 由系统 `mdnsmanager` 提供（无 avahi）。系统打印栈字面意义上在跑 CUPS。
- CUPS 客户端对 dyj 的兼容性**已有实证**（调研 12）：`lpadmin -m everywhere` 自动建队 → wingdi 后端打印成功。
- 华为官方指南细节（zh-cn16091885，2026-11 复核）：适用 **HarmonyOS 5.0+**；自动搜索约 3–5 秒列出设备，点击名称即添加；「IP添加」输入框**只有名称 + IP**（平板实测另有 驱动[自动] / 协议[ipp]），**没有端口字段 → 隐含 631**；IP 添加的一体机不支持扫描（只管打印）——对我们无影响。
- 生态印证（同文档）：系统面板吃不到的打印机由华为应用市场的**厂商驱动 App** 兜底（惠普「慧适联驱动」HarmonyOS 7+、富士胶片打印驱动、东芝、兄弟、柯尼卡美能达），或第三方「智睦云打印」建 "(Cloud)" 前缀的虚拟打印机。华为自家对"系统面板没有的打印机"的答案与 dyj 同构：**要么厂商出 App，要么虚拟打印机 + 系统面板**——dyj 选后者，且不走云。

## 3. 唯一架构冲突与解法：配对门禁

- 现状：LAN 侧 IPP 必须带令牌（`/p/<token>/ipp/print/<key>`），否则 401；mDNS 广播的 `rp=` 不含令牌。ADR-0010 当初的设计理由：URI 路径是三平台系统打印栈唯一通用通道（Windows IPP Class Driver 不支持自定义 header）。
- 冲突：HarmonyOS 的 IP添加只填 IP（无法携带令牌路径）；自动搜索按 mDNS 的 `rp=` 直连（同样无令牌）。
- 解法（已实现，commit 196f55f）：**配对即信任设备**——
  - `pkg/pairing`：`Device.SourceIP` 本就持久化；新增 `Store.TrustedIP(host)`（含 IPv4-mapped IPv6 归一化，命中刷新 LastSeen）。
  - `cmd/dyj` `gate()`：loopback → 令牌 → **受信 IP** → 401。
  - 撤销即断：管理页已有设备列表+DELETE 撤销（`/admin/api/devices`），撤销后令牌与 IP 信任同时失效。
  - 威胁模型不变（阻断随意蹭打，不是防坚决攻击者）：IP 信任的攻击面 = 同 LAN 内伪造源 IP（需 TCP 竞争/ARP 欺骗），与 4 位 PIN 的既有强度同级。
- 产品语义：真实网络打印机在局域网内本来就是免鉴权的；dyj 的"配对一次"换来比全行业更严的门禁，同时不堵死系统打印。

## 4. 真机实验（进行中）

环境：LRT-W20 平板（192.168.110.37，wlan0）+ 本机 Windows（192.168.110.40）真实 dyj 服务 v0.4.1 @6631（"dyj on 生命"，5 台打印机，含真实 EPSON K200）。

已确认：
- 平板与 PC 同网段、TCP 可达（App 内"已连接"实证；ICMP 被防火墙丢弃是表象）。
- 平板已在今晚 00:50 通过 App 与真实服务配对，`paired-devices.json` 记录 `source_ip=192.168.110.37` → 换上信任版服务端后**平板立即受信，无需重新配对**。
- 实验步骤：提权换服务二进制（旧版备份 `dyj.exe.bak-0.4.1`）→ 平板"设置 → 打印机和扫描仪 → 添加"：先自动搜索（应出现 dyj 广播的 5 台），失败再 IP添加（填 192.168.110.40；注意系统可能假设 631 端口，而服务在 6631——这正是要验证的点之一）→ 系统打印一张图 → 观察 dyj 队列。

结果：__待回填__

过程记录（时间线，北京时间 11-09）：
- 07:49 提权完成服务换血：停 v0.4.1 → 备份 `dyj.exe.bak-0.4.1` → 换新构建（哈希 `6676e462` 核对）→ 服务恢复 Running。平板无需重新配对（`source_ip=192.168.110.37` 已在配对库）。
- 07:54 用户在平板打开 设置 → 打印机和扫描仪 → 添加打印机和扫描仪 → **IP添加**（界面实测字段：名称、IP 地址、打印机驱动[自动]、协议[ipp]），填 `192.168.110.40`，点添加 → 提示"打印机需和本机连接相同 WLAN…若仍无法连接"（当时 631 无监听，6631 不在系统假设端口上，未建连）。
- 07:55 在 PC 上以 `dyj.exe serve -port 631 -spool C:\ProgramData\dyj -no-mdns` 起第二个实例引流 631（借用 dyj.exe 程序级防火墙白名单 + 共享配对库；`-no-mdns` 避免与服务抢 mDNS）。此后 **631/6631 双端口就绪**。
- 08:1x 尝试远程代点（uitest）失败两次：一次撞上用户操作，一次因平板自动锁屏（截图全黑）。教训：**dumpLayout 默认合并窗口，背景窗口的控件也会出现在树里**，点击必须先确认前台（截屏校验）。
- 待用户解锁平板重试：IP添加 `192.168.110.40`（现走 631 引流）→ 添加 → 图库系统打印。
- （同日，服务端加固落地，取代手动引流）主实例新增 **631 别名监听**：主端口仍 6631+，631 空闲即自动以同一 IPP 路由监听（仅数据面，无 /admin、/pair），被占则跳过——平板 IP添加 直接填 PC IP 即可，不再需要第二个 dyj 进程；mDNS 新增 **TTL/2（默认 60s）周期重宣告**，防火墙拦 UDP 入站时平板"添加打印机"页面随时能被动捕获。验证清单不变：IP添加 → 图库系统打印 → 记录系统发来的 document-format（pwg-raster 还是 pdf）→ 未配对设备直连仍 401。

补充取证（实验前置侦查时发现）：
- 本机（共享端 PC）防火墙对 `dyj.exe`：TCP 入站**允许**、UDP 入站**阻止**（公用配置文件，netsh 实测）→ 系统面板 **IP添加（TCP 6631）可用**；mDNS 自动搜索依赖入站 UDP 5353 查询，被拦时只能靠 dyj 启动时的无查询宣告（multicast 突发，TTL 120s）被平板 `mdnsmanager` 被动捕获——换服务后立即在平板上搜，命中窗口最大。
- 平板（LRT-W20，API 26）实测进程：`print_service`、`cupsd`、`scan_service`/`sane_service`、`mdnsmanager`；无 avahi。
- 平板 dyj App 内已有配对记录"dyj on 生命 @192.168.110.40:6631 已连接"；真实服务 `paired-devices.json` 含 `source_ip=192.168.110.37`（今晚 00:50 配对）→ 换信任版二进制后平板立即受信。
- 同机双 dyj 实例（el 机实测）：派生相同 `.local` 主机名导致后来者 mDNS 宣告被压掉，`-hostname` 显式区分可解（见 §5）。

关注点（失败分支预案）：
1. IP添加 若强制 631 端口 → 6631 够不着：**已转正**——服务端空闲时自动监听 631（`startIPPAlias`，被占则跳过），此分支的兜底不再依赖手动第二实例；Linux 下 631 需 root，届时自动跳过、走 mDNS 主端口。
2. 系统客户端若只肯发 application/pdf → 服务端未装 poppler 时干净拒绝 0x040B：需评估 Windows 版 poppler（pkg/render/pdfimg）。
3. 系统若要求 TLS（ipps）→ 服务端已支持 `-tls-port` 自签，但系统面板是否接受自签待验。

## 5. 顺带发现（独立于实验成败）

- **同机双 dyj 实例的 mDNS 冲突**：同一台主机跑两个 dyj，两者派生相同的 `.local` 主机名，后来者的宣告被压掉（el 机实测：`-hostname` 显式区分后恢复）。将来要么文档写明，要么实例名冲突时自动加后缀。
- Windows 下非提权绑 <1024 端口：el（Linux）实测 `ip_unprivileged_port_start=1024`，631–639 全拒；Windows 无此限制但服务默认 6631。
- 平板上 EPSON 官方 App（`com.epson.printandscan`）也在——生态印证：厂商 App 走自家协议，系统面板另有入口。

## 6. 参考出处

- PrintExtensionAbility 系统接口：gitee.com/openharmony/docs `OpenHarmony-6.0-Beta1/zh-cn/application-dev/website.md`、`apis-ability-kit/js-apis-bundleManager.md`
- 权限：`security/AccessToken/permissions-for-all.md`（PRINT=normal）、`restricted-permissions.md`（PRINTER_DRIVER，PC/2in1，26.0.0）
- 华为打印指南：consumer.huawei.com/cn/support/content/zh-cn16091885（IP添加、厂商驱动 App 生态）、zh-cn16057990（排障）
- OpenHarmony 打印架构：forums.openharmony.cn/ziyuan/tid-788（打印系统简介）、gitee.com/openharmony/print_print_fwk（发现指令链 PrintSpooler → PrintSA → PrintExtension → CustomPrintExtension；README 架构说明）
- `@ohos.print` / `PrintExtensionAbility` 接口状态 2026-11 复核：developer.huawei.com js-apis-print / js-apis-app-ability-printextensionability（管理接口 systemapi 标注）；社区全链路分析 blog.csdn.net/m0_67505153/article/details/153133523

> **结语（2026-11-09，[ADR-0014](../adr/0014-open-printing-mode.md)）**：此路线已放弃——鸿蒙 App 裁撤，「配对即信任 IP」与 631 别名/mDNS 周期重宣告一并回退；免认证打印改由通用的限时「开放打印模式」承担，§4 真机实验不再回填。
