//go:build !go1.21

package cmp

// ordered mirrors stdlib cmp.Ordered (see internal/slices/slices_go120.go).
type ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 | ~string
}

// Compare returns -1, 0 or +1 as a < b, a == b or a > b.
func Compare[T ordered](a, b T) int {
	switch {
	case a < b:
		return -1
	case b < a:
		return 1
	default:
		return 0
	}
}
