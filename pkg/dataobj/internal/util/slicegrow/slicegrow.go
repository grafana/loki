package slicegrow

import "slices"

// GrowToCap grows the slice to at least n elements total capacity.
// It is an alternative to slices.Grow that increases the capacity of the slice instead of allowing n new appends.
// This is useful when the slice is expected to have nil values.
func GrowToCap[Slice ~[]E, E any](s Slice, n int) Slice {
	if s == nil {
		return make(Slice, 0, n)
	}
	return slices.Grow(s, max(0, n-len(s)))
}

func Copy[Slice ~[]E, E any](dst Slice, src Slice) Slice {
	dst = GrowToCap(dst, len(src))
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}

func CopyString[Slice ~[]byte](dst Slice, src string) Slice {
	dst = GrowToCap(dst, len(src))
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}
