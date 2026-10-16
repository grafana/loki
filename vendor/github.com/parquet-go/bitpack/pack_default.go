package bitpack

import "encoding/binary"

func packInt32Default(dst []byte, src []int32, bitWidth uint) {
	if bitWidth == 0 {
		return
	}

	bitMask := uint32(1<<bitWidth) - 1
	var buffer uint64
	var bufferedBits uint
	byteIndex := 0

	for _, value := range src {
		buffer |= uint64(uint32(value)&bitMask) << bufferedBits
		bufferedBits += bitWidth

		for bufferedBits >= 32 {
			binary.LittleEndian.PutUint32(dst[byteIndex:], uint32(buffer))
			buffer >>= 32
			bufferedBits -= 32
			byteIndex += 4
		}
	}

	if bufferedBits > 0 {
		remainingBytes := (bufferedBits + 7) / 8
		for i := uint(0); i < remainingBytes; i++ {
			dst[byteIndex] = byte(buffer)
			buffer >>= 8
			byteIndex++
		}
	}
}

func packInt64Default(dst []byte, src []int64, bitWidth uint) {
	if bitWidth == 0 {
		return
	}
	if bitWidth == 64 {
		for i, value := range src {
			binary.LittleEndian.PutUint64(dst[i*8:], uint64(value))
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
			bufferLo |= maskedValue << bufferedBits
			bufferedBits += bitWidth
		} else {
			bitsInLo := 64 - bufferedBits
			bufferLo |= maskedValue << bufferedBits
			bufferHi = maskedValue >> bitsInLo
			bufferedBits += bitWidth
		}

		for bufferedBits >= 64 {
			binary.LittleEndian.PutUint64(dst[byteIndex:], bufferLo)
			bufferLo = bufferHi
			bufferHi = 0
			bufferedBits -= 64
			byteIndex += 8
		}
	}

	if bufferedBits > 0 {
		remainingBytes := (bufferedBits + 7) / 8
		for i := uint(0); i < remainingBytes; i++ {
			dst[byteIndex] = byte(bufferLo)
			bufferLo >>= 8
			byteIndex++
		}
	}
}
