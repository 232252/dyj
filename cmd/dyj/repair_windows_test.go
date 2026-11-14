//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubRunNetsh records every netsh argv instead of touching the real
// firewall, and short-circuits the UAC relaunch so tests never pop a
// prompt. Returns the call log.
func stubRunNetsh(t *testing.T, fn func(argv []string) (string, error)) *[]string {
	t.Helper()
	calls := []string{}
	old := runNetsh
	runNetsh = func(argv []string) (string, error) {
		calls = append(calls, strings.Join(argv, " "))
		return fn(argv)
	}
	t.Cleanup(func() { runNetsh = old })
	oldEl := elevateFirewall
	elevateFirewall = func(rc repairCtx) error { return errors.New("elevation-stub") }
	t.Cleanup(func() { elevateFirewall = oldEl })
	return &calls
}

func TestFixFirewallDirectSuccess(t *testing.T) {
	calls := stubRunNetsh(t, func(argv []string) (string, error) { return "Ok.", nil })
	rc := repairCtx{cfg: serveOptions{Port: 6631}}
	if err := fixFirewall(rc); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "delete") {
		t.Errorf("delete-before-add missing:\n%s", joined)
	}
	for _, want := range []string{"name=dyj IPP", "protocol=TCP", "localport=6631", "name=dyj mDNS", "protocol=UDP", "localport=5353", "delete"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestFixFirewallTLSPortRule(t *testing.T) {
	calls := stubRunNetsh(t, func(argv []string) (string, error) { return "Ok.", nil })
	rc := repairCtx{cfg: serveOptions{Port: 6631, TLSPort: 6632}}
	if err := fixFirewall(rc); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(*calls, "\n"); !strings.Contains(joined, "name=dyj IPPS") || !strings.Contains(joined, "localport=6632") {
		t.Errorf("TLS rule missing:\n%s", joined)
	}
}

// 631 别名（ADR-0017）：修复防火墙时必须一并放行别名端口，且删除旧规则
// 时同样按程序路径清理，避免残留。
func TestFixFirewallAddsAliasRule(t *testing.T) {
	calls := stubRunNetsh(t, func(argv []string) (string, error) { return "Ok.", nil })
	rc := repairCtx{cfg: serveOptions{Role: roleShare, Port: 6631}}
	if err := fixFirewall(rc); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "name=dyj IPP Alias") || !strings.Contains(joined, "localport=631") {
		t.Errorf("alias firewall rule missing:\n%s", joined)
	}
	if !strings.Contains(joined, "delete rule name=dyj IPP Alias") {
		t.Errorf("alias rule must be deleted before re-add:\n%s", joined)
	}
}

func TestFixFirewallElevationFallback(t *testing.T) {
	stubRunNetsh(t, func(argv []string) (string, error) {
		return "请求的操作需要提升 (以管理员身份运行)。", errors.New("exit status 1")
	})
	elevated := false
	oldEl := elevateFirewall
	elevateFirewall = func(rc repairCtx) error { elevated = true; return nil }
	t.Cleanup(func() { elevateFirewall = oldEl })
	if err := fixFirewall(repairCtx{cfg: serveOptions{Port: 6631}}); err != nil {
		t.Fatal(err)
	}
	if !elevated {
		t.Error("elevation denial must fall back to the UAC relaunch")
	}
}

func TestNeedsElevationWording(t *testing.T) {
	cases := map[string]bool{
		"The requested operation requires elevation (Run as administrator).": true,
		"请求的操作需要提升 (以管理员身份运行)。":                                           true,
		"拒绝访问。":                                                              true,
		"Access is denied.":                                                  true,
		"Ok.":                                                                false,
	}
	for in, want := range cases {
		if got := needsElevation(in); got != want {
			t.Errorf("needsElevation(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRunFirewallHelperWritesResult(t *testing.T) {
	calls := stubRunNetsh(t, func(argv []string) (string, error) { return "Ok.", nil })
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	resPath := filepath.Join(dir, "res.json")
	if err := os.WriteFile(specPath, []byte(`{"port":17631,"tls_port":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runFirewallHelper(specPath, resPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(resPath)
	if err != nil {
		t.Fatal(err)
	}
	var hr fwResult
	if err := json.Unmarshal(data, &hr); err != nil {
		t.Fatal(err)
	}
	if !hr.OK {
		t.Fatalf("helper not ok: %s", data)
	}
	if joined := strings.Join(*calls, "\n"); !strings.Contains(joined, "17631") {
		t.Errorf("helper did not use the spec port:\n%s", joined)
	}
}

func TestRunFirewallHelperBadSpec(t *testing.T) {
	stubRunNetsh(t, func(argv []string) (string, error) { return "", nil })
	resPath := filepath.Join(t.TempDir(), "res.json")
	if err := runFirewallHelper(filepath.Join(t.TempDir(), "missing.json"), resPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(resPath)
	var hr fwResult
	if err := json.Unmarshal(data, &hr); err != nil {
		t.Fatal(err)
	}
	if hr.OK || hr.Error == "" {
		t.Errorf("unreadable spec must produce ok=false with error, got %+v", hr)
	}
}
