// Package protohelpers provides encoding helpers for reverse-write protobuf marshaling.
// Based on github.com/planetscale/vtprotobuf/protohelpers (Apache 2.0 license).
package protohelpers

import "math/bits"

// EncodeVarint writes a varint-encoded uint64 into dAtA ending at offset,
// writing backwards. Returns the new offset (before the encoded varint).
func EncodeVarint(dAtA []byte, offset int, v uint64) int {
	offset -= SizeOfVarint(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}

// SizeOfVarint returns the encoded size of a varint.
func SizeOfVarint(x uint64) int {
	return (bits.Len64(x|1) + 6) / 7
}
