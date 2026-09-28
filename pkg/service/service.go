// Package service runs dyj as a first-class OS service:
//
//	Windows  — a real SCM service (golang.org/x/sys/windows/svc)
//	Linux    — a systemd unit (systemctl integration)
//	macOS    — a launchd LaunchDaemon (launchctl integration)
//
// The contract is deliberately small: Install persists the serve options
// via persistOptions, registers the service pointing at
// `<binary> service run`, and Start/Stop/Uninstall manipulate it. `run`
// loads the persisted options and calls the serve function — one code
// path for foreground and service execution, so the E2E tests exercise
// exactly what production runs.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ServeFunc runs the server with the given options and MUST return
// promptly when ctx is canceled (the SCM/systemd/launchd stop signal).
// Injected by cmd/dyj so this package never imports the assembly.
type ServeFunc func(ctx context.Context, rawJSONOptions string) error

// OptionsJSON is the persisted serve configuration (the serveOptions
// struct serialized by cmd/dyj). Stored as an opaque JSON string so this
// package does not import the cmd package.
const optionsFile = "service.json"

// stateDir returns where the service configuration lives.
func stateDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		p := os.Getenv("ProgramData")
		if p == "" {
			return "", errors.New("service: %ProgramData% is not set")
		}
		return filepath.Join(p, "dyj"), nil
	case "darwin":
		return "/Library/Application Support/dyj", nil
	default:
		return "/var/lib/dyj", nil
	}
}

// SaveOptions persists the serve options JSON.
func SaveOptions(raw string) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Sanity: must be valid JSON with a "name" or at least an object.
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return fmt.Errorf("service: options are not valid JSON: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, optionsFile), []byte(raw), 0o644)
}

// LoadOptions reads the persisted serve options JSON.
func LoadOptions() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, optionsFile))
	if err != nil {
		return "", fmt.Errorf("service: no persisted options (run `dyj service install` first): %w", err)
	}
	return string(data), nil
}

// Run is the service entry point: load persisted options and serve
// until ctx is canceled. Platform files decide whether ctx comes from
// the SCM (Windows) or is a plain background context bridged to signals.
func Run(ctx context.Context, serve ServeFunc) error {
	raw, err := LoadOptions()
	if err != nil {
		return err
	}
	return serve(ctx, raw)
}
