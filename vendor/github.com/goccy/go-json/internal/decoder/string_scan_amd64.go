package decoder

import (
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// hasStringSIMD is whether indexStringSpecial scans: whether the CPU has AVX2.
var hasStringSIMD = runtime.HasAVX2

//go:noescape
func indexStringSpecialAVX2(p unsafe.Pointer, n int) (index int, high uint64)

// minStringSIMDLength is the number of the bytes from which the rest of a string is scanned by SIMD, after its
// first words: the call and the setup of the registers cost about as much as a few words of the scalar scan.
const minStringSIMDLength = 32

// indexStringSpecial returns the index of the first byte of the n bytes at p which is a quote, a backslash or a
// control character, or n if there is none, and high, which is msb if a byte before it is not ASCII and else 0,
// as the scan by words accumulates it. The last result is false if the CPU has no SIMD scan or n is less than
// minStringSIMDLength: nothing is scanned.
func indexStringSpecial(p unsafe.Pointer, n int) (int, uint64, bool) {
	if !runtime.HasAVX2 || n < minStringSIMDLength {
		return 0, 0, false
	}
	index, high := indexStringSpecialAVX2(p, n)
	if high != 0 {
		// the scan by AVX2 has a bit for every byte which is not ASCII: the callers look at the top bits of bytes.
		high = msb
	}
	return index, high, true
}
