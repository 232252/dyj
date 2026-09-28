# 附录 C：本机环境实测（真实目标场景）

> 由 Team Lead 在项目发起人的 Windows 11 机器上直接执行 PowerShell 命令采集。
> **这些不是假设，是真实的目标环境数据**，用于约束架构决策。

## C.1 主机与工具链

| 项 | 实测值 |
| --- | --- |
| 操作系统 | Microsoft Windows 11 专业版 |
| git | 2.49.0.windows.1（`C:\Program Files\Git\cmd\git.exe`） |
| Python | 3.13（scoop） |
| Node / pnpm | 有（scoop / npm 全局） |
| **Go** | **有**（`C:\Users\sq199\scoop\shims\go.exe`）← Go 选型可直接开工 |
| Rust | 有（`cargo`） |
| gcc / cmake | 有（MinGW） |
| Docker | **未安装** |

**结论：Go 工具链已就绪，无需额外安装即可验证 Go 选型。**

## C.2 网络接口（多网段，对 mDNS 是重要约束）

| IP | 接口 | 前缀 |
| --- | --- | --- |
| 192.168.110.40 | 以太网 | /24 |
| 192.168.101.4 | WLAN | /24 |
| 10.2.3.27 | optun | /24 |
| 10.126.126.4 | et_4_rsks | /24 |
| 169.254.x.x ×3 | 蓝牙网络连接 / 本地连接* | /16（APIPA，无 DHCP） |

**架构含义**：本机有 **4 个以上真实网段** + 3 个 APIPA 虚拟接口。
mDNS 通告必须**显式选择接口**，否则会出现：
- 往 APIPA/蓝牙接口发组播，客户端永远发现不了；
- 多网段下广播到错误子网。

**设计要求**：`discovery/` 模块必须支持 **接口白名单/黑名单配置**，并为每个启用接口分别做通告
（对应 `ipp-usb` 处理 Avahi `interface = all` 的同款问题，但我们可以做得更干净）。

## C.3 打印机现状（关键发现：目标设备是**网络**打印机，不是 USB）

本机可见的打印机队列：

| 队列名 | 驱动 | 端口 |
| --- | --- | --- |
| EPSONC42E48 (K200) | **Epson ESC/P-R V4 Class Driver** | `WSD-33252543-...`（WSD 端口） |
| EPSONC42E48 (K200) (已重定向 1) | Remote Desktop Easy Print | TS002 |
| 100.113.141.45 上的 Kyocera FS-1020MFP GX | Remote Desktop Easy Print | TS001 |
| Microsoft Print to PDF | Microsoft Print To PDF | PORTPROMPT: |
| 导出为WPS PDF | Kingsoft Virtual Printer Driver | — |

**PnP 层实测**：Epson 设备的 `FriendlyName = EPSONC42E48 (K200)`，其
- `Class = WSDPrintDevice` / `Image` / `PrintQueue` / `SoftwareDevice`
- `DEVPKEY_Device_LocationInfo = http://192.168.110.174:80/WSD/DEVICE`
- `DEVPKEY_DeviceContainer_IsNetworkDevice = True`

**结论：这是一台通过 WSD/网络接入的 Epson（L3250 系列量级）一体机，不是 USB 打印机。**

**这条实测数据推翻了"顺手做 USB 直通"的默认假设，并强烈支持简报的架构建议：**

> **不要碰 USB（`usbprint.sys` / WinUSB 解绑），而是把 IPP 服务端架在"已有打印队列"之上。**

即简报 C.3 第 3 点推荐的 `driver/winspl` 路线：
把 IPP 作业**转发给 Windows spooler 里已存在的队列**（此处即 `EPSONC42E48 (K200)`）。
这样：
- 完全绕开 `usbprint.sys` 独占问题；
- 复用系统已装好的厂商驱动做光栅化，**我们不需要实现驱动栈**（避开 cups-filters 那 246 MB）；
- 在 Windows 上可立即做出可用版本。

## C.4 端口连通性实测：打印机当前**离线**

对 `192.168.110.174` 探测：

| 端口 | 结果 |
| --- | --- |
| 631 (IPP) | closed |
| 515 (LPD) | closed |
| 9100 (AppSocket) | closed |
| 80 / 443 | closed |
| 5353 (mDNS) | closed |

对 `100.113.141.45`（Kyocera）探测：631/515/9100/80/443 **全部 closed**。

**结论**：两台打印机当前均不可达（典型原因是打印机休眠/Epson 的 WSD 按需唤醒）。
**这不阻塞调研**，但意味着：

- **P0 验收（真机打印）需要打印机在线时才能做**；
- Epson 这类设备**只在被唤醒时监听端口**（WSD 唤醒机制），
  因此**不能用"端口扫描"作为设备可用性判断**，必须走 WSD 或 spooler 队列。

## C.5 mDNS 环境：**5353 已被占用**

| 监听 | 进程 |
| --- | --- |
| UDP `0.0.0.0:5353` 和 `[::]:5353` | **PID 7776 = `chrome.exe`** |

**重要发现：Chrome 正在占用 UDP 5353**（Chrome 内置 mDNS 用于 Cast/私有网络访问）。

**架构含义（必须在实现前解决）**：
1. 我们的 mDNS 通告进程**不能**独占 5353，否则与 Chrome 冲突；
2. 必须使用 **`SO_REUSEADDR` + `SO_REUSEPORT`** 并加入组播组 `224.0.0.251`，
   以"多监听者"方式共存 —— Go 标准 `net.ListenMulticastUDP` 的默认行为需实测确认；
3. 或改用**单播 DNS-SD 响应**（QU 位）+ 依赖系统响应器，但会牺牲可靠性。

**建议：把"与 Chrome 等其他 5353 监听者共存"列为动工前必须实测的 P0 项。**
这是简报未覆盖、但本机实测暴露的真实风险。

## C.6 Windows 侧可用性（决定"零驱动"是否成立）

实测 `Get-PrinterDriver`：

| 驱动 | 厂商 | 意义 |
| --- | --- | --- |
| **Microsoft IPP Class Driver** | Microsoft | ✅ **Windows 内置 IPP 客户端驱动已存在** |
| Microsoft enhanced Point and Print compatibility driver | Microsoft | Point-and-Print 兼容驱动 |
| Universal Print Class Driver | Microsoft | 云打印 |
| Epson ESC/P-R V4 Class Driver | EPSON | 厂商 v4 类驱动 |

**结论**：`Microsoft IPP Class Driver` 已在本机存在，证明
**"客户端免装驱动、直接添加 IPP 打印机"这条路在 Windows 11 上是真实可行的**，
与附录 B 的微软官方时间表互相印证。

⚠️ **未实测**：`Get-WindowsOptionalFeature` 需要管理员权限，本次**未能**枚举
LPR Port Monitor / Internet Printing Client 等可选功能状态（返回"请求的操作需要提升"）。
因此 **LPR 是否可用、Internet Printing Client 是否已启用，仍是未知数** —— 不要写进文档当事实。

## C.7 防火墙与 SMB 现状

- `LanmanServer`（SMB 服务）：**Running / Automatic**；445 与 139 正在监听。
- 说明本机 SMB 共享能力是现成的，但**本项目不应依赖它**（见附录 B 第 3 条）。

**实现时必须处理**：放行 **TCP 631**（IPP）与 **UDP 5353**（DNS-SD）。
竞品 `ArcticLampyrid/ipp-sharing` 的 README 也明确要求这两条。安装包需自动或引导用户配置。

## 对架构决策的净影响（汇总）

| 实测发现 | 影响的模块 | 决策 |
| --- | --- | --- |
| 打印机是**网络**(WSD)设备，非 USB | `driver/` | ✅ **先做 `winspl` 后端（转发给已有队列），不做 USB 解绑** |
| Go 工具链已就绪 | 全局 | ✅ 可直接验证 Go 选型 |
| 4+ 网段 + APIPA 接口 | `discovery/` | ⚠️ 必须支持接口白/黑名单 |
| **Chrome 占用 5353** | `discovery/` | ⚠️ **P0：必须实测多监听者共存**（简报未覆盖） |
| 打印机休眠、端口全关 | `driver/`, 验收 | ⚠️ 不可用端口扫描判断可用性；真机验收需设备在线 |
| 内置 IPP Class Driver 存在 | 验收 | ✅ "零驱动"目标可行性已确认 |
| 可选功能状态未知（需提权） | 验收 | ⚠️ 待提权后核实 LPR / IPP 客户端状态 |

## 采集命令（可复现）

```powershell
# 环境与工具链
git --version; foreach($t in 'go','node','cargo','python','docker'){ "$t -> $((Get-Command $t -EA SilentlyContinue).Source)" }

# 网络
Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike '127.*' } |
    Select-Object IPAddress,InterfaceAlias,PrefixLength

# 打印机与 PnP 定位
Get-Printer | Select-Object Name,DriverName,PortName
Get-PnpDevice -FriendlyName '*EPSON*' | Select-Object Status,Class,FriendlyName,InstanceId
Get-PnpDeviceProperty -InstanceId '<InstanceId>' | Where-Object { $_.KeyName -match 'Network|Location' }

# 驱动
Get-PrinterDriver | Select-Object Name,Manufacturer

# mDNS 占用
Get-NetUDPEndpoint -LocalPort 5353 | Select-Object LocalAddress,LocalPort,OwningProcess

# 端口连通性
foreach($p in 631,515,9100,80,443,5353){ Test-NetConnection -ComputerName <printer-ip> -Port $p -InformationLevel Quiet }
```
