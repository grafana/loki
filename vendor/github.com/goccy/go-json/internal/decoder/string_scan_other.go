//go:build !amd64

package decoder

import "unsafe"

// hasStringSIMD is false: this architecture has no SIMD scan of a string.
const hasStringSIMD = false

// indexStringSpecial scans nothing: this architecture has no SIMD scan of a string ( see string_scan_amd64.go ).
func indexStringSpecial(p unsafe.Pointer, n int) (int, uint64, bool) {
	return 0, 0, false
}
