package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withScratchStateDir points the package at a scratch state dir for one
// test and restores the real resolver afterwards.
func withScratchStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := stateDirFunc
	stateDirFunc = func() (string, error) { return dir, nil }
	t.Cleanup(func() { stateDirFunc = orig })
	return dir
}

// The deb/rpm/launchd packages install a unit that runs `dyj service
// run` before any `dyj service install` has persisted options — a fresh
// install must come up in bootstrap mode (same as the bare `dyj` launch
// path), not crash-loop in systemd.
func TestRunServesBootstrapDefaultsWhenOptionsMissing(t *testing.T) {
	withScratchStateDir(t) // empty dir: no service.json

	var got string
	err := Run(context.Background(), func(_ context.Context, raw string) error {
		got = raw
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "{}" {
		t.Fatalf("served options = %q, want bootstrap default \"{}\"", got)
	}
}

// Saved options must pass through Run unchanged.
func TestRunServesSavedOptions(t *testing.T) {
	withScratchStateDir(t)
	const saved = `{"role":"both","name":"h","port":6631}`
	if err := SaveOptions(saved); err != nil {
		t.Fatalf("SaveOptions: %v", err)
	}

	var got string
	err := Run(context.Background(), func(_ context.Context, raw string) error {
		got = raw
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != saved {
		t.Fatalf("served options = %q, want %q", got, saved)
	}
}

// A state dir that cannot be read for reasons other than "not there"
// (here: a directory where the file should be) must stay fatal instead
// of silently bootstrapping over a broken machine state.
func TestRunFatalOnUnreadableOptions(t *testing.T) {
	dir := withScratchStateDir(t)
	if err := os.MkdirAll(filepath.Join(dir, optionsFile), 0o755); err != nil {
		t.Fatal(err)
	}

	serveCalled := false
	err := Run(context.Background(), func(_ context.Context, raw string) error {
		serveCalled = true
		return nil
	})
	if err == nil {
		t.Fatal("Run must fail when the options file cannot be read")
	}
	if serveCalled {
		t.Error("serve must not run when options are unreadable")
	}
	if !strings.Contains(err.Error(), "no persisted options") {
		t.Errorf("error should keep the operator hint, got: %v", err)
	}
}
