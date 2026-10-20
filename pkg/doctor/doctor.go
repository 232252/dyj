// Package doctor answers "why can't they print from over there?" as a
// list of named checks with human text and, where one exists, the fix
// command. It backs `dyj doctor` (out-of-process, full checks) and the
// admin UI's diagnostics endpoint (in-process: bind checks that would
// collide with the running listeners are replaced by their in-process
// truth).
package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/232252/dyj/pkg/discovery"
	"github.com/232252/dyj/pkg/driver"
)

// Check is one diagnostic result. Name is a stable id (the UI keys
// localized text off it); Detail/Fix are human strings.
type Check struct {
	Name   string `json:"name"`
	Level  string `json:"level"` // ok | warn | fail | info
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Params configures a doctor run.
type Params struct {
	Port     int
	TLSPort  int
	MDNS     bool
	StateDir string
	Backend  string // backend name ("" = skip backend probe)
	Printer  string // queue name for wingdi/cups
	InProc   bool   // true when called from inside the running server

	// MDNSInstances names the _ipp._tcp instances this node advertises;
	// non-empty adds the egress self-test (browse and find ourselves).
	MDNSInstances []string
}

// Levels.
const (
	LevelOK   = "ok"
	LevelWarn = "warn"
	LevelFail = "fail"
	LevelInfo = "info"
)

// Run executes every check. It never returns a Go error: failures are
// data (a doctor that errors helps nobody).
func Run(p Params) []Check {
	var out []Check
	out = append(out, checkStateDir(p))
	out = append(out, checkPort(p))
	if p.MDNS {
		out = append(out, checkMDNSPort(p))
	}
	out = append(out, checkTLS(p))
	out = append(out, checkPDF())
	out = append(out, checkFirewall())
	out = append(out, checkBackend(p))
	if p.MDNS && len(p.MDNSInstances) > 0 {
		out = append(out, checkMDNSEgress(p))
	}
	return out
}

// checkMDNSEgress browses for our own instances: if the machine cannot
// hear its own announcements, multi-homed egress is broken and LAN peers
// are deaf too (the exact failure fixed in the acceptance round,
// docs/research/12 §2 — this check keeps it from regressing silently).
func checkMDNSEgress(p Params) Check {
	ifaces, err := discovery.SelectInterfaces(nil)
	if err != nil || len(ifaces) == 0 {
		return Check{Name: "mdns-egress", Level: LevelWarn,
			Detail: "没有可用组播网卡，无法自测 mDNS 出口",
			Fix:    "运行 dyj list-interfaces 检查；必要时用 -iface 指定网卡"}
	}
	found, err := discovery.Browse(ifaces, 3*time.Second)
	if err != nil {
		return Check{Name: "mdns-egress", Level: LevelWarn,
			Detail: "mDNS 查询失败: " + err.Error()}
	}
	for _, f := range found {
		for _, want := range p.MDNSInstances {
			if strings.Contains(strings.ToLower(f.Instance), strings.ToLower(want)) {
				return Check{Name: "mdns-egress", Level: LevelOK,
					Detail: fmt.Sprintf("本机能在网络上看到自己的广播（%s）", want)}
			}
		}
	}
	if runtime.GOOS == "windows" {
		// Measured on the acceptance host: announcements egress fine
		// (a Debian peer's avahi sees them) while the same machine's own
		// browse cannot hear its own multicast loop. Windows receive of
		// self-sent multicast is unreliable — keep this informational.
		return Check{Name: "mdns-egress", Level: LevelInfo,
			Detail: "Windows 上本机自测听不到自己的广播（平台限制）；跨机验证可用：对端 avahi-browse -rt _ipp._tcp"}
	}
	return Check{Name: "mdns-egress", Level: LevelFail,
		Detail: "本机广播了自己，但网络上查询不到（多网卡出口异常）",
		Fix:    "运行 dyj list-interfaces，用 -iface 指定局域网网卡；检查防火墙是否放行 UDP 5353"}
}

func checkStateDir(p Params) Check {
	if p.StateDir == "" {
		return Check{Name: "state-dir", Level: LevelInfo, Detail: "未指定状态目录（使用运行时默认）"}
	}
	probe := filepath.Join(p.StateDir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return Check{Name: "state-dir", Level: LevelFail,
			Detail: "状态目录不可写: " + p.StateDir,
			Fix:    "检查目录权限；Windows 服务账户需要 %ProgramData%\\dyj 的写权限"}
	}
	_ = os.Remove(probe)
	return Check{Name: "state-dir", Level: LevelOK, Detail: "状态目录可写: " + p.StateDir}
}

func checkPort(p Params) Check {
	if p.Port <= 0 {
		return Check{Name: "port", Level: LevelInfo, Detail: "未配置端口"}
	}
	if p.InProc {
		return Check{Name: "port", Level: LevelOK,
			Detail: fmt.Sprintf("端口 %d 正由本进程监听", p.Port)}
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p.Port))
	if err != nil {
		return Check{Name: "port", Level: LevelWarn,
			Detail: fmt.Sprintf("端口 %d 已被占用（可能是 dyj 服务已在运行）", p.Port),
			Fix:    "若 dyj 服务已运行则无需处理；否则换端口或停掉占用进程",
		}
	}
	_ = ln.Close()
	return Check{Name: "port", Level: LevelOK, Detail: fmt.Sprintf("端口 %d 空闲", p.Port)}
}

func checkMDNSPort(p Params) Check {
	pc, err := net.ListenPacket("udp4", ":5353")
	if err != nil {
		// Expected wherever avahi/mDNSResponder/Windows mDNS runs: dyj
		// coexists via SO_REUSEADDR. A failure here is only a problem
		// when peers cannot discover the printer.
		return Check{Name: "mdns-5353", Level: LevelInfo,
			Detail: "UDP 5353 已被系统 mDNS 服务占用（正常现象，dyj 通过端口共享共存）",
			Fix:    "若局域网设备发现不了打印机，见 docs/research/06 与 README 疑难解答",
		}
	}
	_ = pc.Close()
	return Check{Name: "mdns-5353", Level: LevelOK, Detail: "UDP 5353 空闲（无系统 mDNS 占用）"}
}

func checkTLS(p Params) Check {
	if p.TLSPort <= 0 {
		return Check{Name: "tls", Level: LevelInfo, Detail: "ipps 未启用（-tls-port 可开启）"}
	}
	crt := filepath.Join(p.StateDir, "tls.crt")
	if _, err := os.Stat(crt); err != nil {
		return Check{Name: "tls", Level: LevelWarn, Detail: "TLS 证书缺失，ipps 客户端将握手失败",
			Fix: "重启服务自动生成自签证书，或删除损坏的 tls.crt"}
	}
	return Check{Name: "tls", Level: LevelOK, Detail: "TLS 证书就绪"}
}

func checkPDF() Check {
	if driver.PDFAvailable() {
		return Check{Name: "pdf", Level: LevelOK, Detail: "PDF 支持已启用（pdftoppm）"}
	}
	return Check{Name: "pdf", Level: LevelInfo,
		Detail: "PDF 未启用（未找到 pdftoppm）",
		Fix:    "安装 poppler-utils（Linux）/ poppler（brew, choco）后重启服务即可声明 application/pdf"}
}

func checkFirewall() Check {
	switch runtime.GOOS {
	case "windows":
		return Check{Name: "firewall", Level: LevelInfo,
			Detail: "Windows 防火墙需放行 TCP 6631 与 UDP 5353（三个网络配置文件都要）",
			Fix:    `netsh advfirewall firewall add rule name="dyj IPP" dir=in action=allow program="<dyj.exe 路径>" protocol=TCP localport=6631 profile=domain,private,public`,
		}
	case "darwin":
		return Check{Name: "firewall", Level: LevelInfo,
			Detail: "macOS 应用防火墙默认关闭；若手动开启，需允许 dyj 接收入站连接",
			Fix:    "/usr/libexec/ApplicationFirewall/socketfilterfw --add <dyj 路径> --unblockapp <dyj 路径>"}
	default:
		return Check{Name: "firewall", Level: LevelInfo,
			Detail: "多数发行版默认不拦入站；firewalld（Fedora/RHEL）需放行",
			Fix:    "firewall-cmd --permanent --add-port=6631/tcp && firewall-cmd --reload"}
	}
}

func checkBackend(p Params) Check {
	switch p.Backend {
	case "", "null":
		return Check{Name: "backend", Level: LevelInfo, Detail: "未指定打印后端（file/null 不会输出到纸）"}
	case "wingdi":
		if p.Printer == "" {
			return Check{Name: "backend", Level: LevelWarn, Detail: "wingdi 需要 -printer <Windows 打印队列名>"}
		}
		return Check{Name: "backend", Level: LevelInfo,
			Detail: "wingdi 后端，队列: " + p.Printer + "（真实验证以实际打印为准）"}
	case "cups":
		return Check{Name: "backend", Level: LevelInfo,
			Detail: "cups 后端" + queueSuffix(p.Printer) + "；队列状态以 lpstat 为准"}
	case "file":
		return Check{Name: "backend", Level: LevelWarn,
			Detail: "file 后端只落盘不打印（调试用途）"}
	default:
		return Check{Name: "backend", Level: LevelWarn, Detail: "未知后端 " + p.Backend}
	}
}

func queueSuffix(q string) string {
	if q == "" {
		return "（自动选择）"
	}
	return "，队列: " + q
}
