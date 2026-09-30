//go:build !windows

package main

import (
	"errors"
	"os/exec"
)

// openBrowser launches xdg-open with the default handler.
func openBrowser(url string) {
	path, err := exec.LookPath("xdg-open")
	if err != nil || path == "" {
		return // headless: the URL is printed by the caller anyway
	}
	cmd := &exec.Cmd{Path: path, Args: []string{path, url}}
	_ = cmd.Start() // fire and forget; the browser outlives us
}

// runSetupHelper has no role on unix (no UAC).
func runSetupHelper(specPath, resPath string) error {
	_ = specPath
	_ = resPath
	return errors.New("setup-helper is Windows-only")
}
