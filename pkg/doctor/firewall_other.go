//go:build !windows

package doctor

import "errors"

// probeFirewallRules has no cross-platform implementation: grading with a
// real rule table is a Windows Defender Firewall concern. The error keeps
// the firewall check advisory on other platforms.
func probeFirewallRules() ([]firewallRule, error) {
	return nil, errors.New("firewall rule probe is Windows-only")
}
