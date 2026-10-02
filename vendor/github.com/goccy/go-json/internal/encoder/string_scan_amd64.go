package encoder

import (
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// The scan of a string for a byte to escape by AVX2, 32 bytes at a time.

//go:noescape
func scanStringAVX2(p unsafe.Pointer, n int, tables *nibbleTables) int

//go:noescape
func escapeStringAVX2(dst, src unsafe.Pointer, n int, tables *nibbleTables, seqs *[256]uint64) (consumed, written int)

// hasEscapeLoop is whether appendEscapedSIMD escapes: whether the CPU has AVX2.
var hasEscapeLoop = runtime.HasAVX2

// escapeSegmentLength is the number of the bytes of a string which appendEscapedSIMD escapes by one call of the
// loop, which has room for 6 bytes of every byte: it bounds the growth of the buffer for a long string.
const escapeSegmentLength = 1024

// appendEscapedSIMD appends the string escaped, 32 bytes at a time, as the appendString functions do by the
// tables of their options, and returns the number of the bytes of the string it appended. It stops at a byte
// which the caller escapes ( a byte which is not ASCII, if UTF-8 is normalized ) or when fewer than 32 bytes
// remain. The string has 32 bytes or more, and the CPU has AVX2.
func appendEscapedSIMD(buf []byte, s string, tables *nibbleTables) ([]byte, int) {
	consumed := 0
	for len(s)-consumed >= 32 {
		n := min(len(s)-consumed, escapeSegmentLength)
		if need := 6*n + 32; cap(buf)-len(buf) < need {
			buf = growForString(buf, need)
		}
		dst := unsafe.Add(unsafe.Pointer(unsafe.SliceData(buf)), len(buf))
		c, w := escapeStringAVX2(dst, unsafe.Add(unsafe.Pointer(unsafe.StringData(s)), consumed), n, tables, &escapeSequences)
		buf = buf[:len(buf)+w]
		consumed += c
		if c+32 <= n {
			// stopped at a byte which the caller escapes
			break
		}
	}
	return buf, consumed
}

// minSIMDScanLength is the length from which a string is scanned by SIMD: the setup of the registers costs
// about as much as a few words of the scalar scan.
const minSIMDScanLength = 32

// hasEscapeSIMD is whether a byte of the string may need an escape, by SIMD. The second result is false if
// the CPU doesn't support it or the string is short, and the string is not looked at.
func (e *stringEscape) hasEscapeSIMD(src unsafe.Pointer, n int) (bool, bool) {
	return scanBytesSIMD(src, n, &e.tables)
}

// scanBytesSIMD is whether a byte of the n bytes at src is in the tables, by SIMD. The second result is false
// if the CPU doesn't support it or n is small, and the bytes are not looked at.
func scanBytesSIMD(src unsafe.Pointer, n int, tables *nibbleTables) (bool, bool) {
	if !runtime.HasAVX2 || n < minSIMDScanLength {
		return false, false
	}
	return scanStringAVX2(src, n, tables) != 0, true
}
