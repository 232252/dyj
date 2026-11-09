package doctor

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestMain 把防火墙探测换成快速桩：真探测每次要拉起 PowerShell（秒级），
// 不关心防火墙的用例不该为之买单；防火墙用例各自用 withFakeRules 注入。
func TestMain(m *testing.M) {
	old := probeFirewallRulesFn
	probeFirewallRulesFn = func() ([]firewallRule, error) { return nil, errors.New("stub") }
	code := m.Run()
	probeFirewallRulesFn = old
	os.Exit(code)
}

func TestRunBasicChecks(t *testing.T) {
	checks := Run(Params{
		Port:     0, // port 0: kernel picks a free port; we check the names only
		MDNS:     true,
		StateDir: t.TempDir(),
		Backend:  "file",
	})
	seen := map[string]bool{}
	for _, c := range checks {
		if seen[c.Name] {
			t.Errorf("duplicate check name %q", c.Name)
		}
		seen[c.Name] = true
		switch c.Level {
		case LevelOK, LevelWarn, LevelFail, LevelInfo:
		default:
			t.Errorf("check %q has unknown level %q", c.Name, c.Level)
		}
		if c.Detail == "" {
			t.Errorf("check %q has empty detail", c.Name)
		}
	}
	for _, want := range []string{"state-dir", "port", "mdns-5353", "tls", "pdf", "firewall", "backend"} {
		if !seen[want] {
			t.Errorf("missing check %q (got %v)", want, seen)
		}
	}
}

func TestRunStateDirFail(t *testing.T) {
	checks := Run(Params{StateDir: "Z:\\\\definitely-not-a-dir\\\\dyj", Port: 6631})
	var found bool
	for _, c := range checks {
		if c.Name == "state-dir" {
			found = true
			if c.Level != LevelFail {
				t.Errorf("unwritable state dir level = %q", c.Level)
			}
			if c.Fix == "" {
				t.Error("fail-level check should carry a fix hint")
			}
		}
	}
	if !found {
		t.Fatal("state-dir check missing")
	}
}

func TestInProcPortReportsListening(t *testing.T) {
	checks := Run(Params{Port: 6631, InProc: true})
	for _, c := range checks {
		if c.Name == "port" {
			if c.Level != LevelOK || !strings.Contains(c.Detail, "6631") {
				t.Errorf("in-process port check = %+v", c)
			}
			return
		}
	}
	t.Fatal("port check missing")
}

func TestFileBackendWarns(t *testing.T) {
	checks := Run(Params{Backend: "file", StateDir: t.TempDir()})
	for _, c := range checks {
		if c.Name == "backend" && c.Level != LevelWarn {
			t.Errorf("file backend should warn (got %q): %s", c.Level, c.Detail)
		}
	}
}

// withFakeRules 注入假规则表。分级用例直接调 checkFirewallWindows 纯函数
// 而非 Run()：CI 在 Linux 上跑测试，Run 的 GOOS 分支到不了 windows 逻辑。
func withFakeRules(t *testing.T, rules []firewallRule, err error) {
	t.Helper()
	old := probeFirewallRulesFn
	probeFirewallRulesFn = func() ([]firewallRule, error) { return rules, err }
	t.Cleanup(func() { probeFirewallRulesFn = old })
}

func withFakeExe(t *testing.T, path string) {
	t.Helper()
	old := osExecutableFn
	osExecutableFn = func() (string, error) { return path, nil }
	t.Cleanup(func() { osExecutableFn = old })
}

func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q missing", name)
	return Check{}
}

func TestFirewallProbeOk(t *testing.T) {
	withFakeExe(t, `C:\x\dyj.exe`)
	withFakeRules(t, []firewallRule{
		{Name: "dyj IPP", Enabled: true, Direction: "in", Protocol: "tcp", LocalPort: "6631", Program: `C:\x\DYJ.EXE`},
		{Name: "dyj mDNS", Enabled: true, Direction: "in", Protocol: "udp", LocalPort: "5353", Program: `C:\x\dyj.exe`},
	}, nil)
	c := checkFirewallWindows(Params{Port: 6631})
	if c.Level != LevelOK {
		t.Errorf("covered rules should grade ok, got %+v", c)
	}
	if c.CanFix {
		t.Error("ok check must not be fixable")
	}
}

func TestFirewallProbeMissingWarnsAndFixable(t *testing.T) {
	withFakeExe(t, `C:\x\dyj.exe`)
	withFakeRules(t, nil, nil)
	c := checkFirewallWindows(Params{Port: 6631})
	if c.Level != LevelWarn || !c.CanFix {
		t.Fatalf("missing rules should warn+can_fix, got %+v", c)
	}
	for _, want := range []string{"TCP 6631", "UDP 5353", `localport=6631`, `localport=5353`} {
		if !strings.Contains(c.Detail+c.Fix, want) {
			t.Errorf("firewall check missing %q in %+v", want, c)
		}
	}
}

func TestFirewallProbeRangeAndAnyProgramCover(t *testing.T) {
	withFakeExe(t, `C:\x\dyj.exe`)
	// 值为探测层归一化后的小写（探测负责大小写归一）。
	withFakeRules(t, []firewallRule{
		{Name: "dyj IPP", Enabled: true, Direction: "in", Protocol: "tcp", LocalPort: "6600-6700", Program: "Any"},
		{Name: "dyj mDNS", Enabled: true, Direction: "in", Protocol: "udp", LocalPort: "Any"},
	}, nil)
	c := checkFirewallWindows(Params{Port: 6631})
	if c.Level != LevelOK {
		t.Errorf("range/any-program rules should cover, got %+v", c)
	}
}

func TestFirewallProbeDisabledRuleDoesNotCover(t *testing.T) {
	withFakeRules(t, []firewallRule{
		{Name: "dyj IPP", Enabled: false, Direction: "in", Protocol: "tcp", LocalPort: "6631"},
		{Name: "dyj mDNS", Enabled: true, Direction: "in", Protocol: "udp", LocalPort: "5353"},
	}, nil)
	c := checkFirewallWindows(Params{Port: 6631})
	if c.Level != LevelWarn || !c.CanFix {
		t.Errorf("disabled rule must not count as covered, got %+v", c)
	}
}

func TestFirewallProbeErrorStaysAdvisory(t *testing.T) {
	withFakeRules(t, nil, errors.New("no powershell"))
	c := checkFirewallWindows(Params{Port: 6631})
	if c.Level != LevelInfo || c.CanFix {
		t.Errorf("unprobeable firewall must stay info without can_fix, got %+v", c)
	}
}

func TestFirewallTLSPortNeedsOwnRule(t *testing.T) {
	withFakeRules(t, []firewallRule{
		{Name: "dyj IPP", Enabled: true, Direction: "in", Protocol: "tcp", LocalPort: "6631"},
		{Name: "dyj mDNS", Enabled: true, Direction: "in", Protocol: "udp", LocalPort: "5353"},
	}, nil)
	c := checkFirewallWindows(Params{Port: 6631, TLSPort: 6632})
	if c.Level != LevelWarn || !strings.Contains(c.Detail, "6632") {
		t.Errorf("TLS port without its own rule should warn mentioning it, got %+v", c)
	}
}

func TestCanFixFlags(t *testing.T) {
	checks := Run(Params{
		Port:     6631,
		StateDir: "Z:\\definitely-not-a-dir\\dyj", // state-dir fail
		TLSPort:  6632,                                // no cert in empty state dir → tls warn
		Backend:  "file",
	})
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if c := byName["state-dir"]; c.Level != LevelFail || !c.CanFix {
		t.Errorf("state-dir fail should be fixable, got %+v", c)
	}
	if c := byName["tls"]; c.Level != LevelWarn || !c.CanFix {
		t.Errorf("tls warn should be fixable, got %+v", c)
	}
}

func TestCanFixJSONOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Check{Name: "port", Level: LevelOK, Detail: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "can_fix") {
		t.Errorf("can_fix should be omitempty, got %s", b)
	}
	b, _ = json.Marshal(Check{Name: "firewall", Level: LevelWarn, Detail: "x", CanFix: true})
	if !strings.Contains(string(b), `"can_fix":true`) {
		t.Errorf("can_fix=true should serialize, got %s", b)
	}
}
