//go:build !go1.21

package slices

import "sort"

// ordered mirrors stdlib cmp.Ordered; declared locally so the Go 1.20
// legacy toolchain needs no golang.org/x/exp dependency.
type ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 | ~string
}

// Sort sorts x into ascending order.
func Sort[S ~[]E, E ordered](x S) {
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
}

// SortFunc sorts x with the given ordering function; not stable, matching
// the stdlib slices.SortFunc this mirrors.
func SortFunc[S ~[]E, E any](x S, f func(a, b E) int) {
	sort.Slice(x, func(i, j int) bool { return f(x[i], x[j]) < 0 })
}

// SortStableFunc sorts x stably with the given ordering function.
func SortStableFunc[S ~[]E, E any](x S, f func(a, b E) int) {
	sort.SliceStable(x, func(i, j int) bool { return f(x[i], x[j]) < 0 })
}

// Contains reports whether v is present in s.
func Contains[S ~[]E, E comparable](s S, v E) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

// Reverse reverses the elements of x in place.
func Reverse[S ~[]E, E any](x S) {
	for i, j := 0, len(x)-1; i < j; i, j = i+1, j-1 {
		x[i], x[j] = x[j], x[i]
	}
}

// DeleteFunc removes in place every element e with del(e) == true,
// zeroing the obsolete tail so held pointers can be collected — the
// stdlib slices.DeleteFunc contract the spooler prune relies on.
func DeleteFunc[S ~[]E, E any](s S, del func(E) bool) S {
	out := s[:0]
	for _, e := range s {
		if !del(e) {
			out = append(out, e)
		}
	}
	var zero E
	for i := len(out); i < len(s); i++ {
		s[i] = zero
	}
	return out
}
