//go:build windows

package tray

import (
	"fmt"
	"golang.org/x/sys/windows"
)

// shellOpen opens a URL in the default browser via ShellExecuteW — the
// same code path Explorer's "open" uses, so no console window is
// created and no child process of ours survives the tray.
func shellOpen(url string) error {
	ptr, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("open")
	err = windows.ShellExecute(0, verb, ptr, nil, nil, windows.SW_SHOWNORMAL)
	if err != nil {
		return fmt.Errorf("ShellExecute: %w", err)
	}
	return nil
}
