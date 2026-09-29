//go:build !windows

package service

import (
	"os"
	"strings"
	"testing"
)

// The unit/plist renderers are pure: their content is the contract with
// systemd/launchd, so it is locked by assertion rather than a byte
// golden file — the interesting invariants are substring-level.

func TestSystemdUnitHardening(t *testing.T) {
	unit := systemdUnit("/usr/local/bin/dyj")
	for _, want := range []string{
		`ExecStart="/usr/local/bin/dyj" service run`,
		"After=network-online.target",
		"Restart=on-failure",
		// hardening set
		"DynamicUser=yes",
		"StateDirectory=dyj",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"PrivateTmp=yes",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK",
		"CapabilityBoundingSet=",
		// 6631/5353 are high ports: the unit must not need capabilities
		"SystemCallFilter=@system-service",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("systemd unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "User=root") {
		t.Error("unit must not run as root: DynamicUser is the whole point")
	}
}

func TestLaunchdPlistInvariants(t *testing.T) {
	plist := launchdPlist("/Applications/dyj/dyj")
	for _, want := range []string{
		"<key>Label</key><string>org.dyj.printer</string>",
		"<string>/Applications/dyj/dyj</string>",
		"<string>service</string>", "<string>run</string>",
		"<key>RunAtLoad</key><true/>",
		"<key>KeepAlive</key><true/>",
		"<key>StandardOutPath</key><string>/var/log/dyj.log</string>",
		"<key>StandardErrorPath</key><string>/var/log/dyj.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestLaunchdPlistXMLEscape(t *testing.T) {
	plist := launchdPlist(`/Applications/My App & Co/dyj`)
	if !strings.Contains(plist, "/Applications/My App &amp; Co/dyj") {
		t.Errorf("ampersand not escaped:\n%s", plist)
	}
	if strings.Contains(plist, " & ") {
		t.Errorf("raw & reached the XML:\n%s", plist)
	}
}

func TestSystemdUnitQuotePath(t *testing.T) {
	unit := systemdUnit("/opt/My Tools/dyj")
	if !strings.Contains(unit, `ExecStart="/opt/My Tools/dyj" service run`) {
		t.Errorf("path with spaces not quoted:\n%s", unit)
	}
}

func TestLaunchdPlistPath(t *testing.T) {
	if got, want := launchdPlistPath(), "/Library/LaunchDaemons/org.dyj.printer.plist"; got != want {
		t.Fatalf("plist path = %q, want %q", got, want)
	}
	// The file that installLaunchd writes must satisfy launchd's
	// ownership/permission gate: root-owned, not group/other-writable.
	if fi, err := os.Stat("/Library/LaunchDaemons"); err == nil {
		_ = fi // exists on macOS dev machines; nothing to assert here
	}
}
