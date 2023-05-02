package util

import (
	"golang.org/x/exp/constraints"
)

// Max returns the maximum of two arguments.
func Max[T constraints.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Min returns the minimum of two arguments.
func Min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}
