//go:build go1.21

// Package cmp is a thin shim over the standard library cmp package
// (same rationale as internal/slices: the Win7 legacy build compiles
// this tree with the Go 1.20.14 toolchain, which predates stdlib cmp).
package cmp

import "cmp"

// Compare returns -1, 0 or +1 as a < b, a == b or a > b
// (stdlib cmp.Compare).
func Compare[T cmp.Ordered](a, b T) int { return cmp.Compare(a, b) }
