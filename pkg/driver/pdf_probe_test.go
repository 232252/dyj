package driver

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProbePDFWellKnown pins the service-visible fallback: a pdftoppm
// dropped into the users-modify state dir must be found even when PATH
// (as in the LocalSystem service) knows nothing about it.
func TestProbePDFWellKnown(t *testing.T) {
	pd := t.TempDir()
	t.Setenv("ProgramData", pd)
	t.Setenv("USERPROFILE", t.TempDir())
	if got := probePDFWellKnown(); got != "" {
		t.Fatalf("empty locations must yield \"\", got %q", got)
	}
	dir := filepath.Join(pd, "dyj", "poppler")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "pdftoppm.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := probePDFWellKnown(); got != exe {
		t.Fatalf("probe = %q, want %q", got, exe)
	}
}