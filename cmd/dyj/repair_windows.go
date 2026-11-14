//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/232252/dyj/pkg/doctor"
	"github.com/232252/dyj/pkg/driver"
	"golang.org/x/sys/windows"
)

// repairFirewall adds the inbound allow rules. Precondition and result
// are graded by the doctor probe itself, so "fixed" always means
// "verified", not "netsh exited 0".
func repairFirewall(rc repairCtx) repairOutcome {
	before := firewallCheckNow(rc)
	if before.Level == doctor.LevelOK {
		return repairOutcome{Check: "firewall", OK: true, Detail: "防火墙已放行，无需修复", Skipped: true}
	}
	if err := fixFirewall(rc); err != nil {
		return repairOutcome{Check: "firewall", OK: false,
			Detail: err.Error() + "；也可手动执行: " + before.Fix}
	}
	doctor.InvalidateFirewallProbe() // 复查必须是新探测，不能吃 TTL 缓存
	after := firewallCheckNow(rc)
	if after.Level == doctor.LevelOK {
		return repairOutcome{Check: "firewall", OK: true, Detail: "防火墙规则已添加并复查通过"}
	}
	return repairOutcome{Check: "firewall", OK: false, Detail: "命令已执行但复查未通过: " + after.Detail}
}

// fixFirewall deletes our rules (so repeats don't pile same-name entries
// and a port change leaves no stale allow; absent rules are not an
// error), then adds the current set. Unelevated first — an admin shell
// or the LocalSystem service just succeeds; only a denial falls back to
// the UAC relaunch.
func fixFirewall(rc repairCtx) error {
	for _, argv := range firewallNetshCmds("delete", rc) {
		_, _ = runNetsh(argv)
	}
	for _, argv := range firewallNetshCmds("add", rc) {
		out, err := runNetsh(argv)
		if err == nil {
			continue
		}
		if needsElevation(out) {
			return elevateFirewall(rc)
		}
		return fmt.Errorf("netsh %s: %s", strings.Join(argv[1:], " "), firstLineOf(out))
	}
	return nil
}

// runNetsh / elevateFirewall are vars so tests can record commands and
// short-circuit the UAC relaunch instead of touching the real firewall.
var (
	runNetsh = func(argv []string) (string, error) {
		var out bytes.Buffer
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return strings.TrimSpace(out.String()), err
	}
	elevateFirewall = fixFirewallElevated
)

// firewallNetshCmds builds the netsh argvs. Rule names must stay in sync
// with pkg/doctor's probe and packaging/windows/dyj.iss. The delete is
// scoped to THIS binary's program path: a name-only delete would strip
// the rules of another dyj install (e.g. the installed service) living
// under a different path.
func firewallNetshCmds(verb string, rc repairCtx) [][]string {
	exe, err := os.Executable()
	if err != nil {
		exe = "<dyj.exe>"
	}
	var cmds [][]string
	if verb == "delete" {
		for _, name := range []string{"dyj IPP", "dyj IPPS", "dyj IPP Alias", "dyj mDNS"} {
			cmds = append(cmds, []string{"netsh", "advfirewall", "firewall", "delete", "rule",
				"name=" + name, "program=" + exe})
		}
		return cmds
	}
	add := func(name, proto string, port int) {
		cmds = append(cmds, []string{"netsh", "advfirewall", "firewall", "add", "rule",
			"name=" + name, "dir=in", "action=allow", "program=" + exe,
			"protocol=" + proto, fmt.Sprintf("localport=%d", port),
			"profile=domain,private,public"})
	}
	if rc.cfg.Port > 0 {
		add("dyj IPP", "TCP", rc.cfg.Port)
	}
	if rc.cfg.TLSPort > 0 {
		add("dyj IPPS", "TCP", rc.cfg.TLSPort)
	}
	if sharesRole(rc.cfg.Role) && rc.cfg.Port != 631 {
		add("dyj IPP Alias", "TCP", 631)
	}
	add("dyj mDNS", "UDP", 5353)
	return cmds
}

// needsElevation matches the unelevated netsh denials in both the English
// and the zh-CN wording (same approach as sysqueue's access-denied probe).
func needsElevation(out string) bool {
	low := strings.ToLower(out)
	for _, marker := range []string{"elevation", "access is denied", "提升", "拒绝访问"} {
		if strings.Contains(low, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// fwSpec / fwResult are the temp-file handshake between the unelevated
// server and the elevated `dyj setup-helper --fix-firewall` relaunch
// (same contract shape as sysqueue's helperResult — the elevated child
// has no usable stdin/stdout pipes).
type fwSpec struct {
	Port    int    `json:"port"`
	TLSPort int    `json:"tls_port"`
	Role    string `json:"role,omitempty"`
}

type fwResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Output string `json:"output,omitempty"`
}

func fixFirewallElevated(rc repairCtx) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating dyj binary: %w", err)
	}
	stamp := time.Now().UnixNano()
	dir := os.TempDir()
	specPath := filepath.Join(dir, fmt.Sprintf("dyj-fw-spec-%d.json", stamp))
	resPath := filepath.Join(dir, fmt.Sprintf("dyj-fw-res-%d.json", stamp))
	defer os.Remove(specPath)
	defer os.Remove(resPath)
	spec, err := json.Marshal(fwSpec{Port: rc.cfg.Port, TLSPort: rc.cfg.TLSPort, Role: rc.cfg.Role})
	if err != nil {
		return err
	}
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString(fmt.Sprintf("setup-helper --fix-firewall --spec %q --result %q", specPath, resPath))
	cwd, _ := windows.UTF16PtrFromString(dir)
	if err := windows.ShellExecute(0, verb, file, args, cwd, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("管理员确认被取消或失败: %w", err)
	}
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		data, err := os.ReadFile(resPath)
		if err != nil {
			continue // helper still running (or UAC still on screen)
		}
		var hr fwResult
		if err := json.Unmarshal(data, &hr); err != nil {
			return fmt.Errorf("helper result unreadable: %w", err)
		}
		if !hr.OK {
			return fmt.Errorf("提权修复失败: %s", hr.Error)
		}
		return nil
	}
	return fmt.Errorf("提权修复超时（120s 内未收到结果）")
}

// runFirewallHelper is the elevated child entry (setup-helper
// --fix-firewall): run the netsh set with administrator rights and write
// the result file.
func runFirewallHelper(specPath, resPath string) error {
	write := func(hr fwResult) error {
		data, _ := json.Marshal(hr)
		return os.WriteFile(resPath, data, 0o600)
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		return write(fwResult{Error: "spec unreadable: " + err.Error()})
	}
	var spec fwSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return write(fwResult{Error: "spec invalid: " + err.Error()})
	}
	rc := repairCtx{cfg: serveOptions{Port: spec.Port, TLSPort: spec.TLSPort, Role: spec.Role}}
	var out bytes.Buffer
	for _, argv := range firewallNetshCmds("delete", rc) {
		if o, _ := runNetsh(argv); o != "" {
			out.WriteString(o + "\n")
		}
	}
	for _, argv := range firewallNetshCmds("add", rc) {
		o, err := runNetsh(argv)
		if o != "" {
			out.WriteString(o + "\n")
		}
		if err != nil {
			return write(fwResult{Error: fmt.Sprintf("netsh %s: %s", strings.Join(argv[1:], " "), firstLineOf(o)), Output: out.String()})
		}
	}
	return write(fwResult{OK: true, Output: out.String()})
}

// repairPDF installs poppler through the first package manager it finds:
// scoop (user-level) → winget → choco (wants an admin shell; expected to
// fail unelevated and the error says so). The running process still
// resolves pdftoppm from its start-time PATH, so the detail must say
// "restart to take effect".
func repairPDF(rc repairCtx) repairOutcome {
	if driver.PDFAvailable() {
		return repairOutcome{Check: "pdf", OK: true, Detail: "PDF 组件已就绪，无需修复", Skipped: true}
	}
	tool, err := installPoppler()
	if err != nil {
		return repairOutcome{Check: "pdf", OK: false,
			Detail: "自动安装失败: " + err.Error() + "；可手动安装 poppler 后重启服务"}
	}
	return repairOutcome{Check: "pdf", OK: true, Detail: "已通过 " + tool + " 安装 PDF 组件；重启服务后生效"}
}

func installPoppler() (string, error) {
	type pm struct {
		tool string
		argv []string
	}
	tries := []pm{
		{"scoop", []string{"scoop", "install", "poppler"}},
		{"winget", []string{"winget", "install", "-e", "--id", "oschwartz10612.Poppler", "--accept-source-agreements", "--accept-package-agreements", "--silent"}},
		{"choco", []string{"choco", "install", "poppler", "-y"}},
	}
	var errs []string
	for _, t := range tries {
		if _, err := exec.LookPath(t.tool); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		out, err := exec.CommandContext(ctx, t.argv[0], t.argv[1:]...).CombinedOutput()
		cancel()
		if err == nil {
			return t.tool, nil
		}
		errs = append(errs, t.tool+": "+firstLineOf(string(out)))
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("未找到可用的包管理器（scoop / winget / choco）")
	}
	return "", fmt.Errorf("%s", strings.Join(errs, "; "))
}
