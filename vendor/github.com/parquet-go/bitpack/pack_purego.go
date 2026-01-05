//go:build purego || !arm64

package bitpack

import "encoding/binary"

func packInt32(dst []byte, src []int32, bitWidth uint) {
	if bitWidth == 0 {
		return
	}

	bitMask := uint32(1<<bitWidth) - 1
	var buffer uint64
	var bufferedBits uint
	byteIndex := 0

	for _, value := range src {
		// Add value to buffer
		buffer |= uint64(uint32(value)&bitMask) << bufferedBits
		bufferedBits += bitWidth

		// Flush complete 32-bit words
		for bufferedBits >= 32 {
			binary.LittleEndian.PutUint32(dst[byteIndex:], uint32(buffer))
			buffer >>= 32
			bufferedBits -= 32
			byteIndex += 4
		}
	}

	// Flush remaining bits
	if bufferedBits > 0 {
		// Only write the bytes we need
		remainingBytes := (bufferedBits + 7) / 8
		for i := uint(0); i < remainingBytes; i++ {
			dst[byteIndex] = byte(buffer)
			buffer >>= 8
			byteIndex++
		}
	}
}

func packInt64(dst []byte, src []int64, bitWidth uint) {
	if bitWidth == 0 {
		return
	}
	if bitWidth == 64 {
		// Special case: no packing needed, direct copy
		for i, v := range src {
			binary.LittleEndian.PutUint64(dst[i*8:], uint64(v))
		}
		return
	}

	bitMask := uint64(1<<bitWidth) - 1
	var bufferLo, bufferHi uint64
	var bufferedBits uint
	byteIndex := 0

	for _, value := range src {
		maskedValue := uint64(value) & bitMask

		if bufferedBits+bitWidth <= 64 {
			// Value fits entirely in low buffer
			bufferLo |= maskedValue << bufferedBits
			bufferedBits += bitWidth
		} else {
			// Value spans low and high buffers
			bitsInLo := 64 - bufferedBits
			bufferLo |= maskedValue << bufferedBits
			bufferHi = maskedValue >> bitsInLo
			bufferedBits += bitWidth
		}

		// Flush complete 64-bit words
		for bufferedBits >= 64 {
			binary.LittleEndian.PutUint64(dst[byteIndex:], bufferLo)
			bufferLo = bufferHi
			bufferHi = 0
			bufferedBits -= 64
			byteIndex += 8
		}
	}

	// Flush remaining bits
	if bufferedBits > 0 {
		remainingBytes := (bufferedBits + 7) / 8
		for i := uint(0); i < remainingBytes; i++ {
			dst[byteIndex] = byte(bufferLo)
			bufferLo >>= 8
			byteIndex++
		}
	}
}
