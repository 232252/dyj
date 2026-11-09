//go:build windows

package doctor

import (
	"os/exec"
	"strings"
)

// probeFirewallRules reads the inbound allow rules named for dyj through
// PowerShell's NetSecurity module, emitted as pipe-delimited records so
// parsing does not depend on the OS display language. Requires PS3+ /
// Win8+; on legacy systems the error path keeps the firewall check
// advisory (doctor.go keeps the static fix hint in that case).
//
// The display-name filter is a wildcard on purpose: an exact-name list
// makes Get-NetFirewallRule error-exit when one of the names is absent
// (e.g. dyj IPPS on a machine where TLS was never enabled).
func probeFirewallRules() ([]firewallRule, error) {
	const script = `$ErrorActionPreference='SilentlyContinue'
Get-NetFirewallRule -DisplayName 'dyj*' -ErrorAction SilentlyContinue | ForEach-Object {
  $pf = $_ | Get-NetFirewallPortFilter
  $af = $_ | Get-NetFirewallApplicationFilter
  '{0}|{1}|{2}|{3}|{4}|{5}' -f $_.DisplayName,$_.Enabled,$_.Direction,$pf.Protocol,($pf.LocalPort -join ','),$af.Program
}`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var rules []firewallRule
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		f := strings.Split(line, "|")
		if len(f) != 6 || f[0] == "" {
			continue
		}
		rules = append(rules, firewallRule{
			Name:      f[0],
			Enabled:   strings.EqualFold(f[1], "true"),
			Direction: normalizeDirection(f[2]),
			Protocol:  strings.ToLower(strings.TrimSpace(f[3])),
			LocalPort: strings.TrimSpace(f[4]),
			Program:   strings.TrimSpace(f[5]),
		})
	}
	return rules, nil
}

// normalizeDirection folds PowerShell's Inbound/Outbound (and netsh's
// In/Out) onto the compact in/out the grader compares.
func normalizeDirection(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "in", "inbound":
		return "in"
	case "out", "outbound":
		return "out"
	}
	return strings.ToLower(strings.TrimSpace(s))
}
