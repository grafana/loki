package builder

import "unsafe"

// ntFlushLine copies one staged swwc line to its destination with
// non-temporal stores: 16 key words from sk to dk and 8 doc words from sd to
// dd. The stores bypass the cache and are weakly ordered; call sfence before
// another core reads the destination.
//
//go:noescape
func ntFlushLine(dk, sk, dd, sd unsafe.Pointer)

// sfence orders all preceding non-temporal stores before later stores.
func sfence()
