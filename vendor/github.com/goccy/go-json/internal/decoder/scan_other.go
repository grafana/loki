//go:build !amd64 && !arm64

package decoder

import "unsafe"

// scanBlock computes the masks of the block at p, by words: this architecture has no SIMD scan.
func scanBlock(p unsafe.Pointer, m *scanMasks) {
	scanBlockWords(p, m)
}
