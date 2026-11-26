package decoder

import (
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

//go:noescape
func scanBlockAVX2(p unsafe.Pointer, m *scanMasks)

// scanBlock computes the masks of the block at p: by AVX2 when the CPU has it, else by words.
func scanBlock(p unsafe.Pointer, m *scanMasks) {
	if runtime.HasAVX2 {
		scanBlockAVX2(p, m)
		return
	}
	scanBlockWords(p, m)
}
