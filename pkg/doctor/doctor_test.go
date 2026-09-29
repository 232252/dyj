package doctor

import (
	"strings"
	"testing"
)

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
