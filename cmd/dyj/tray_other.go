//go:build !windows

package main

import "errors"

// cmdTray is Windows-only in v1 (see docs/research/10 §1.3): on
// Linux/macOS the web admin page + .desktop entry is the interface.
func cmdTray(args []string) error {
	return errors.New("dyj: 系统托盘目前随 Windows 包交付；Linux/macOS 请使用管理页 http://127.0.0.1:<port>/admin")
}
