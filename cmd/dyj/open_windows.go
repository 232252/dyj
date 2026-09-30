//go:build windows

package main

import (
	"golang.org/x/sys/windows"

	"github.com/sq199/dyj/pkg/sysqueue"
)

// openBrowser hands the URL to the shell (default browser, no console
// flash).
func openBrowser(url string) {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(url)
	_ = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// runSetupHelper installs the queue named in the spec file and writes the
// result file; see sysqueue.InstallElevated for the parent side.
func runSetupHelper(specPath, resPath string) error {
	return sysqueue.RunInstallHelper(specPath, resPath)
}
