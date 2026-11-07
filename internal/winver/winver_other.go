//go:build !windows

package winver

// Guard is a no-op off Windows (see winver_windows.go).
func Guard() {}
