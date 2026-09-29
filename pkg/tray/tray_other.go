//go:build !windows

package tray

import "errors"

// Run is not wired on non-Windows in v1: Linux/macOS use the web admin
// page plus the .desktop / .app entry points (docs/research/10 §1.3).
func Run(opts Options) error {
	return errors.New("tray: 系统托盘目前随 Windows 包交付；Linux/macOS 请使用管理页 http://127.0.0.1:<port>/admin")
}
