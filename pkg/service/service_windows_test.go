//go:build windows

package service

import "testing"

// The SCM ImagePath must carry the subcommand: 0.4.0's first real install
// registered the bare exe, which fell into launch mode, never reported
// Running and was killed by the SCM after 30 s (event 7009).
func TestImagePath(t *testing.T) {
	got := imagePath(`C:\Program Files\dyj\dyj.exe`)
	want := `"C:\Program Files\dyj\dyj.exe" service run`
	if got != want {
		t.Fatalf("imagePath = %q, want %q", got, want)
	}
}
