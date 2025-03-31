package slicegrow

import "slices"

func Grow[Slice ~[]E, E any](s Slice, n int) Slice {
	return slices.Grow(s, max(0, n-len(s)))
}
