//go:build !amd64

package builder

import "unsafe"

// ntFlushLine copies one staged swwc line with regular stores; non-temporal
// stores are only implemented on amd64.
func ntFlushLine(dk, sk, dd, sd unsafe.Pointer) {
	*(*swwcLineK)(dk) = *(*swwcLineK)(sk)
	*(*swwcLineD)(dd) = *(*swwcLineD)(sd)
}

func sfence() {}
