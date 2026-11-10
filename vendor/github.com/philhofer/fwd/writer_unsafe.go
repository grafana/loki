//go:build !appengine && !tinygo
// +build !appengine,!tinygo

package fwd

import (
	"unsafe"
)

// unsafe cast string as []byte
func unsafestr(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
