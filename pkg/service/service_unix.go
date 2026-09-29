//go:build !windows

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// ServiceName is the systemd unit / launchd label stem.
const ServiceName = "dyj"

// launchdLabel is the reverse-DNS job label (launchd convention).
const launchdLabel = "org.dyj.printer"

func RunForeground(serve ServeFunc) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, serve)
}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

// Install registers the service: a systemd unit on Linux, a launchd
// LaunchDaemon on macOS. The unit execs `<binary> service run`, reading
// the options persisted by SaveOptions.
//
// Process spawning policy: the two wrappers below execute exactly two
// fixed programs ("systemctl", "launchctl") with constant argument
// lists via exec.Command — the Go argument-array form with no shell in
// the path, so no command-injection surface exists. The only variable
// content is the dyj binary path, which goes into the unit/plist FILE
// (quoted, XML-escaped), never into a command line.
func Install() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return installLaunchd(exe)
	}
	return installSystemd(exe)
}

// systemctl runs systemctl with constant arguments.
func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	return cmd.Run()
}

// launchctl runs launchctl with constant arguments.
func launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	return cmd.Run()
}

// systemdUnit renders the unit file for the dyj binary at exe.
//
// Hardening follows the current systemd.exec baseline for a LAN-facing
// network daemon: dyj binds only high ports (6631/5353 — no
// CAP_NET_BIND_SERVICE needed) and writes only its StateDirectory, so
// it runs under a DynamicUser with an empty capability bounding set.
// Custom -spool/-out paths outside /var/lib/dyj need a drop-in
// override ("systemctl edit dyj" → ReadWritePaths=).
func systemdUnit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=dyj LAN printer sharing (IPP Everywhere + mDNS)
Documentation=https://github.com/sq199/dyj
After=network-online.target
Wants=network-online.target

[Service]
Type=exec
ExecStart=%q service run
Restart=on-failure
RestartSec=5
# --- hardening: high ports + StateDirectory are the only privileges ---
DynamicUser=yes
StateDirectory=dyj
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
SystemCallFilter=@system-service
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
`, exe)
}

func installSystemd(exe string) error {
	// /etc/systemd/system is the administrator's slot: this path runs
	// only when an admin invoked `dyj service install` by hand. Packages
	// must ship the unit in /usr/lib/systemd/system instead (packaging/).
	path := "/etc/systemd/system/" + ServiceName + ".service"
	if err := os.WriteFile(path, []byte(systemdUnit(exe)), 0o644); err != nil {
		return fmt.Errorf("service: writing %s requires root: %w", path, err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("service: systemctl daemon-reload: %w", err)
	}
	return systemctl("enable", ServiceName)
}

// xmlEscape keeps Windows-style "Program Files" paths and "&" out of
// trouble inside the plist.
func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
	).Replace(s)
}

// launchdPlist renders the LaunchDaemon plist for the dyj binary at exe.
func launchdPlist(exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>service</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/dyj.log</string>
  <key>StandardErrorPath</key><string>/var/log/dyj.log</string>
</dict>
</plist>
`, launchdLabel, xmlEscape(exe))
}

func launchdPlistPath() string {
	return "/Library/LaunchDaemons/" + launchdLabel + ".plist"
}

func installLaunchd(exe string) error {
	path := launchdPlistPath()
	if err := os.WriteFile(path, []byte(launchdPlist(exe)), 0o644); err != nil {
		return fmt.Errorf("service: writing %s requires root: %w", path, err)
	}
	// launchd refuses to load plists it does not consider root-owned and
	// group/other-writable ("Dubious ownership/permissions") — set both
	// explicitly instead of trusting the umask.
	if err := os.Chown(path, 0, 0); err != nil {
		return fmt.Errorf("service: chown root:wheel %s (requires root): %w", path, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("service: chmod %s: %w", path, err)
	}
	return launchdBootstrap()
}

// launchdBootstrap loads the daemon with the modern domain syntax
// (launchctl bootstrap, 10.10+), falling back to the deprecated
// `load` on systems where bootstrap is unavailable or the job is
// already loaded.
func launchdBootstrap() error {
	if err := launchctl("bootstrap", "system", launchdPlistPath()); err != nil {
		return launchctl("load", launchdPlistPath())
	}
	return nil
}

// launchdBootout unloads the daemon (bootout, 10.10+), falling back to
// the deprecated `unload`.
func launchdBootout() error {
	if err := launchctl("bootout", "system/"+launchdLabel); err != nil {
		return launchctl("unload", launchdPlistPath())
	}
	return nil
}

// Uninstall removes the service registration.
func Uninstall() error {
	if runtime.GOOS == "darwin" {
		_ = launchdBootout()
		return os.Remove(launchdPlistPath())
	}
	_ = systemctl("stop", ServiceName)
	_ = systemctl("disable", ServiceName)
	return os.Remove("/etc/systemd/system/" + ServiceName + ".service")
}

// Start starts the service.
func Start() error {
	if runtime.GOOS == "darwin" {
		// kickstart (re)launches a bootstrapped job; fall back to a
		// plain bootstrap for a job that is not loaded yet.
		if err := launchctl("kickstart", "-k", "system/"+launchdLabel); err != nil {
			return launchdBootstrap()
		}
		return nil
	}
	return systemctl("start", ServiceName)
}

// Stop stops the running service.
func Stop() error {
	if runtime.GOOS == "darwin" {
		return launchdBootout()
	}
	return systemctl("stop", ServiceName)
}
