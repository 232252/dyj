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

func installSystemd(exe string) error {
	unit := fmt.Sprintf(`[Unit]
Description=dyj LAN printer sharing (IPP Everywhere + mDNS)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart="%s" service run
Restart=on-failure
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
`, exe)
	path := "/etc/systemd/system/" + ServiceName + ".service"
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
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

func installLaunchd(exe string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>org.dyj.printer</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>service</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/var/log/dyj.log</string>
</dict>
</plist>
`, xmlEscape(exe))
	path := "/Library/LaunchDaemons/org.dyj.printer.plist"
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("service: writing %s requires root: %w", path, err)
	}
	return nil
}

// Uninstall removes the service registration.
func Uninstall() error {
	if runtime.GOOS == "darwin" {
		_ = launchctl("unload", "/Library/LaunchDaemons/org.dyj.printer.plist")
		return os.Remove("/Library/LaunchDaemons/org.dyj.printer.plist")
	}
	_ = systemctl("stop", ServiceName)
	_ = systemctl("disable", ServiceName)
	return os.Remove("/etc/systemd/system/" + ServiceName + ".service")
}

// Start starts the service.
func Start() error {
	if runtime.GOOS == "darwin" {
		return launchctl("load", "/Library/LaunchDaemons/org.dyj.printer.plist")
	}
	return systemctl("start", ServiceName)
}

// Stop stops the service.
func Stop() error {
	if runtime.GOOS == "darwin" {
		return launchctl("unload", "/Library/LaunchDaemons/org.dyj.printer.plist")
	}
	return systemctl("stop", ServiceName)
}
