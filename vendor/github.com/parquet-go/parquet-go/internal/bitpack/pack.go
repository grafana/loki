package bitpack

import (
	"encoding/binary"
)

// PackInt32 packs values from src to dst, each value is packed into the given
// bit width regardless of how many bits are needed to represent it.
//
// The function panics if dst is too short to hold the bit packed values.
func PackInt32(dst []byte, src []int32, bitWidth uint) {
	assertPack(dst, len(src), bitWidth)
	packInt32(dst, src, bitWidth)
}

func packInt32(dst []byte, src []int32, bitWidth uint) {
	n := ByteCount(uint(len(src)) * bitWidth)
	b := dst[:n]

	for i := range b {
		b[i] = 0
	}

	bitMask := uint32(1<<bitWidth) - 1
	bitOffset := uint(0)

	for _, value := range src {
		i := bitOffset / 32
		j := bitOffset % 32

		lo := binary.LittleEndian.Uint32(dst[(i+0)*4:])
		hi := binary.LittleEndian.Uint32(dst[(i+1)*4:])

		lo |= (uint32(value) & bitMask) << j
		hi |= (uint32(value) >> (32 - j))

		binary.LittleEndian.PutUint32(dst[(i+0)*4:], lo)
		binary.LittleEndian.PutUint32(dst[(i+1)*4:], hi)

		bitOffset += bitWidth
	}
}

// PackInt64 packs values from src to dst, each value is packed into the given
// bit width regardless of how many bits are needed to represent it.
//
// The function panics if dst is too short to hold the bit packed values.
func PackInt64(dst []byte, src []int64, bitWidth uint) {
	assertPack(dst, len(src), bitWidth)
	packInt64(dst, src, bitWidth)
}

func packInt64(dst []byte, src []int64, bitWidth uint) {
	n := ByteCount(uint(len(src)) * bitWidth)
	b := dst[:n]

	for i := range b {
		b[i] = 0
	}

	bitMask := uint64(1<<bitWidth) - 1
	bitOffset := uint(0)

	for _, value := range src {
		i := bitOffset / 64
		j := bitOffset % 64

		lo := binary.LittleEndian.Uint64(dst[(i+0)*8:])
		hi := binary.LittleEndian.Uint64(dst[(i+1)*8:])

		lo |= (uint64(value) & bitMask) << j
		hi |= (uint64(value) >> (64 - j))

		binary.LittleEndian.PutUint64(dst[(i+0)*8:], lo)
		binary.LittleEndian.PutUint64(dst[(i+1)*8:], hi)

		bitOffset += bitWidth
	}
}

func assertPack(dst []byte, count int, bitWidth uint) {
	_ = dst[:ByteCount(bitWidth*uint(count))]
}
