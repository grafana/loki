package decoder

import "unsafe"

//go:noescape
func scanBlockNEON(p unsafe.Pointer, m *scanMasks)

// scanBlock computes the masks of the block at p, by NEON.
func scanBlock(p unsafe.Pointer, m *scanMasks) {
	scanBlockNEON(p, m)
}
