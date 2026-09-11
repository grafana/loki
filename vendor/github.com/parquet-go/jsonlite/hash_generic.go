//go:build !amd64 && !386 && !arm64 && !ppc64 && !ppc64le && !loong64 && !s390x && !wasm

package jsonlite

import "hash/maphash"

// hashseed randomizes the tag bytes across processes; see the note on the
// amd64 build's seed for what that does and does not protect against.
var hashseed = maphash.MakeSeed()

// hashKey falls back to maphash on the architectures that the compiler does
// not mark unalignedOK, where the raw unaligned loads in hash_fast.go would
// fault rather than merely run slowly.
//
// The tag is only a filter, so the two implementations need not agree: nothing
// outside a single process ever compares them.
func hashKey(k string) byte { return byte(maphash.String(hashseed, k)) }
