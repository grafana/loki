//go:build !((amd64 || arm64) && !appengine && gc && !noasm && !nounsafe && !purego)
// +build !amd64,!arm64 appengine !gc noasm nounsafe purego

package lz4block

import "encoding/binary"

// srcReader loads little endian words from a byte slice, with bounds checks.
type srcReader struct{}

func newSrcReader([]byte) srcReader { return srcReader{} }

// Slicing to the exact length takes one bounds check, where b[i:] takes two.
func (srcReader) load16(b []byte, i int) uint16 { return binary.LittleEndian.Uint16(b[i : i+2]) }
func (srcReader) load32(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[i : i+4]) }
func (srcReader) load64(b []byte, i int) uint64 { return binary.LittleEndian.Uint64(b[i : i+8]) }
