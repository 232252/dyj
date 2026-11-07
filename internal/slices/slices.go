//go:build go1.21

// Package slices is a thin shim over the standard library slices package.
//
// Why call sites import this instead of stdlib "slices": the Win7 legacy
// build (scripts/build-legacy-windows.sh) compiles this tree with the Go
// 1.20.14 toolchain — the last release that runs on Windows 7/8 — where
// stdlib slices/cmp do not exist. On go1.21+ toolchains every function
// below is a direct stdlib delegation (zero cost); on go1.20 the
// build-tagged fallback in slices_go120.go is compiled instead. The
// legacy worktree only lowers the go directive, never edits these files.
package slices

import (
	"cmp"
	"slices"
)

// Sort sorts x into ascending order (stdlib slices.Sort).
func Sort[S ~[]E, E cmp.Ordered](x S) { slices.Sort(x) }

// SortFunc sorts x with the given ordering function; not stable, like
// the stdlib slices.SortFunc it delegates to.
func SortFunc[S ~[]E, E any](x S, f func(a, b E) int) { slices.SortFunc(x, f) }

// SortStableFunc sorts x stably with the given ordering function
// (stdlib slices.SortStableFunc).
func SortStableFunc[S ~[]E, E any](x S, f func(a, b E) int) { slices.SortStableFunc(x, f) }

// Contains reports whether v is present in s (stdlib slices.Contains).
func Contains[S ~[]E, E comparable](s S, v E) bool { return slices.Contains(s, v) }

// Reverse reverses the elements of x in place (stdlib slices.Reverse).
func Reverse[S ~[]E, E any](x S) { slices.Reverse(x) }

// DeleteFunc removes in place every element e with del(e) == true and
// zeroes the obsolete tail — stdlib slices.DeleteFunc semantics.
func DeleteFunc[S ~[]E, E any](s S, del func(E) bool) S { return slices.DeleteFunc(s, del) }
