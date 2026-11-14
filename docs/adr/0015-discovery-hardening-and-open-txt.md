# ADR-0015：发现链路加固、开放模式 TXT 契约与跨网段深度扫描

- **状态**：已接受
- **日期**：2026-11-13
- **关联**：[ADR-0010](0010-role-wizard-pairing.md)、[ADR-0014](0014-open-printing-mode.md)、[research/15](../research/15-discovery-cross-subnet-mesh.md)

## 背景

v0.5.0 实际使用暴露三类问题：①「无认证模式用不了」——开放打印的数据面门禁
本身正确，但发现链路从不反映开放状态；②「不能正确显示有哪些打印机」——枚举
失败被吞、共享键撞键、扫描合并吞行、已装列表慢且闪空；③「局域网查找太弱」——
单窗口无重试、查询不入组播组、通告仅一次、冲突探测超时单位错误、mesh 网卡被漏。

## 决策

### 1. 开放模式 TXT 契约（修「用不了」）

- mDNS TXT 的 `pair` 标记按 `openActive()` 实时生成：开放=`0`，否则=`1`。
  接收端 `NeedPIN = (pair=="1")`，`0`/缺失 → 免配对安装分支。
- `Advertiser.SetTXT` 锁内换 TXT 并立即重宣告（cache-flush）；节点
  `syncOpenTXT` 在窗口开启/常开切换/关闭时调用，窗口到期另挂定时器回落。
  惰性过期仍是访问判定的唯一事实源，定时器只负责发现链路及时刷新。
- `applyConfig` 对入站 JSON 缺 `open_print` 键保留现值（常开有专属 DELETE
  关闭通道，不会锁死）；前端开放操作后重拉配置快照，双保险。
- 免配对安装前先 `POST /admin/api/probe` 探一次共享端，401 → 回落配对码框，
  避免 TXT 陈旧时装出打不了的队列。

### 2. 列表正确性

- Windows 枚举区分「成功 0 台」与「调用失败」，失败返回错误而非空列表。
- 共享键唯一化（已分配集合 + 后缀递增），修复第三台纯中文名撞键；
  `applyConfig` 按 queue/name 继承既有 Key，存量配对 URI 不断。
- 扫描合并键 UUID 优先，无 UUID 退 label + host-base + port（host-base 剥离
  `${IFACE}` 的 `-<iface>` 后缀），同名异机不再合并。
- 已装状态：`sysqueue.Names` 一次批查全部队列；IPP 探测 sem=4 并行 + 10s
  缓存，总预算 2.5s；前端刷新失败保留旧列表。

### 3. 发现核心加固

- 查询 0/400ms/1.2s 多轮退避；有 PTR 缺 SRV/TXT 即定向补查。
- 查询套接字加入 224.0.0.251（临时端口，不占 5353，与 Chrome 共存），
  收得到纯组播应答。
- 结果校验并行（sem=4）；可达探测并行取最快并记真实 RTT。
- 修复 `timeAfter(220)` → `220*time.Millisecond`（原为 220 纳秒，冲突探测
  形同虚设）。
- 恢复 RFC 6762 §8.3 的 TTL/2 周期重宣告（独立于 ADR-0014 回退的鸿蒙配套，
  这是标准行为，Close 停止）。
- `BrowseInterfaces` 放宽：Up + IPv4 非回环，不要求 Multicast 标志（mesh
  网卡可扫）；通告侧仍用严格的 `MulticastCapableInterfaces`。
- 离线治理：lastSeen 缓存，短暂消失的行保留并标不可达，超 90s 剔除。
- 扫描服务端缓存 10s + 单飞；客户端页可见态 20s 自动重扫，手动重扫穿透。

### 4. 跨网段深度扫描（research/15 P3）

- `pkg/discovery/subnetscan.go`：各选中网卡 /24 + 用户 CIDR（≤1024 地址）；
  6631 全量，显式 CIDR 补 631（兼收第三方 IPP）；sem=64，TCP 600ms 快筛后
  **必须 IPP Get-Printer-Attributes 确认**（research/12 §4：mesh 假握手不能
  当打印机）；总预算 ≤8s。
- `POST/GET /admin/api/deep-scan` 后台任务 + 单飞，**仅手动触发**；客户端页
  「深度扫描」按钮 + 可选 CIDR 输入，2s 轮询，结果并入列表。

## 后果

- 正面：开放模式端到端可用；列表如实反映本机与局域网；发现在丢包/晚启动/
  mesh/跨网段下都能找到打印机。
- 取舍：IPv6 本期不做（全 IPv4 链路，双栈涉三平台 socket/zone 语义，稳定
  优先，记为后续）；深扫不随每次扫描自动跑（流量不可控）；addedStore 不自动
  删（离线保留 + 手动移除，用户数据）。
- 兼容：IPP wire 未动，黄金字节测试原样护航；全部新代码 go1.20 可编译
  （legacy 通道，internal 垫片）；存量配对 URI 与已装队列不受影响。