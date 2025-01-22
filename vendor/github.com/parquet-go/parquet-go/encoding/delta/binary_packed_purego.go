//go:build purego || !amd64

package delta

import (
	"encoding/binary"
)

func encodeMiniBlockInt32(dst []byte, src *[miniBlockSize]int32, bitWidth uint) {
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

func encodeMiniBlockInt64(dst []byte, src *[miniBlockSize]int64, bitWidth uint) {
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

func decodeBlockInt32(block []int32, minDelta, lastValue int32) int32 {
	for i := range block {
		block[i] += minDelta
		block[i] += lastValue
		lastValue = block[i]
	}
	return lastValue
}

func decodeBlockInt64(block []int64, minDelta, lastValue int64) int64 {
	for i := range block {
		block[i] += minDelta
		block[i] += lastValue
		lastValue = block[i]
	}
	return lastValue
}

func decodeMiniBlockInt32(dst []int32, src []uint32, bitWidth uint) {
	bitMask := uint32(1<<bitWidth) - 1
	bitOffset := uint(0)

	for n := range dst {
		i := bitOffset / 32
		j := bitOffset % 32
		d := (src[i] & (bitMask << j)) >> j
		if j+bitWidth > 32 {
			k := 32 - j
			d |= (src[i+1] & (bitMask >> k)) << k
		}
		dst[n] = int32(d)
		bitOffset += bitWidth
	}
}

func decodeMiniBlockInt64(dst []int64, src []uint32, bitWidth uint) {
	bitMask := uint64(1<<bitWidth) - 1
	bitOffset := uint(0)

	for n := range dst {
		i := bitOffset / 32
		j := bitOffset % 32
		d := (uint64(src[i]) & (bitMask << j)) >> j
		if j+bitWidth > 32 {
			k := 32 - j
			d |= (uint64(src[i+1]) & (bitMask >> k)) << k
			if j+bitWidth > 64 {
				k := 64 - j
				d |= (uint64(src[i+2]) & (bitMask >> k)) << k
			}
		}
		dst[n] = int64(d)
		bitOffset += bitWidth
	}
}
