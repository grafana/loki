package encoder

import (
	"unsafe"
)

// The scan of a string for a byte to escape by NEON, 16 bytes at a time.

//go:noescape
func scanStringNEON(p unsafe.Pointer, n int, tables *nibbleTables) int

// minSIMDScanLength is the length from which a string is scanned by SIMD.
const minSIMDScanLength = 16

// maxOnePassLength is the length of the longest string which AppendString looks at and copies in one pass:
// a longer one is looked at by SIMD, which then costs less, and copied after.
const maxOnePassLength = minSIMDScanLength - 1

// scanBytesSIMD is whether a byte of the n bytes at src is in the tables, by SIMD. The second result is false
// if n is small, and the bytes are not looked at. It is inlined, so that AppendString calls the scan itself.
func scanBytesSIMD(src unsafe.Pointer, n int, tables *nibbleTables) (bool, bool) {
	if n < minSIMDScanLength {
		return false, false
	}
	return scanStringNEON(src, n, tables) != 0, true
}

// hasEscapeLoop is false: the loop of the escapes by SIMD is of amd64 only ( see string_scan_amd64.go ).
const hasEscapeLoop = false

// appendEscapedSIMD appends nothing: it is never called on this architecture.
func appendEscapedSIMD(buf []byte, _ string, _ *nibbleTables, _ bool) ([]byte, int) {
	return buf, 0
}
