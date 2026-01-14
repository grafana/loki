package bitset

import "math/bits"

func popcntSlice(s []uint64) (cnt uint64) {
	for _, x := range s {
		cnt += uint64(bits.OnesCount64(x))
	}
	return
}

func popcntMaskSlice(s, m []uint64) (cnt uint64) {
	// The next line is to help the bounds checker, it matters!
	_ = m[len(s)-1] // BCE
	for i := range s {
		cnt += uint64(bits.OnesCount64(s[i] &^ m[i]))
	}
	return
}

// popcntAndSlice computes the population count of the AND of two slices.
// It assumes that len(m) >= len(s) > 0.
func popcntAndSlice(s, m []uint64) (cnt uint64) {
	// The next line is to help the bounds checker, it matters!
	_ = m[len(s)-1] // BCE
	for i := range s {
		cnt += uint64(bits.OnesCount64(s[i] & m[i]))
	}
	return
}

// popcntOrSlice computes the population count of the OR of two slices.
// It assumes that len(m) >= len(s) > 0.
func popcntOrSlice(s, m []uint64) (cnt uint64) {
	// The next line is to help the bounds checker, it matters!
	_ = m[len(s)-1] // BCE
	for i := range s {
		cnt += uint64(bits.OnesCount64(s[i] | m[i]))
	}
	return
}

// popcntXorSlice computes the population count of the XOR of two slices.
// It assumes that len(m) >= len(s) > 0.
func popcntXorSlice(s, m []uint64) (cnt uint64) {
	// The next line is to help the bounds checker, it matters!
	_ = m[len(s)-1] // BCE
	for i := range s {
		cnt += uint64(bits.OnesCount64(s[i] ^ m[i]))
	}
	return
}
