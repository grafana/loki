//go:build (amd64 || arm64) && !appengine && gc && !noasm && !nounsafe && !purego
// +build amd64 arm64
// +build !appengine
// +build gc
// +build !noasm
// +build !nounsafe
// +build !purego

package lz4block

import "unsafe"

// srcReader loads little endian words from a byte slice without bounds
// checks, on little endian platforms with fast unaligned loads. The noasm,
// nounsafe and purego tags select the portable version instead. Callers must
// only load within the slice, and say why their positions are in bounds.
type srcReader struct{ p unsafe.Pointer }

// newSrcReader returns a reader for b, which must not be empty.
func newSrcReader(b []byte) srcReader { return srcReader{unsafe.Pointer(&b[0])} }

func (r srcReader) load16(_ []byte, i int) uint16 { return *(*uint16)(unsafe.Add(r.p, i)) }
func (r srcReader) load32(_ []byte, i int) uint32 { return *(*uint32)(unsafe.Add(r.p, i)) }
func (r srcReader) load64(_ []byte, i int) uint64 { return *(*uint64)(unsafe.Add(r.p, i)) }
