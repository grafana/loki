// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bitutil

import (
	"encoding/binary"
	"math"
	"math/bits"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow/memory"
)

var (
	BitMask        = [8]byte{1, 2, 4, 8, 16, 32, 64, 128}
	FlippedBitMask = [8]byte{254, 253, 251, 247, 239, 223, 191, 127}
)

// IsMultipleOf8 returns whether v is a multiple of 8.
func IsMultipleOf8(v int64) bool { return v&7 == 0 }

// IsMultipleOf64 returns whether v is a multiple of 64
func IsMultipleOf64(v int64) bool { return v&63 == 0 }

func BytesForBits(bits int64) int64 { return (bits + 7) >> 3 }

// NextPowerOf2 rounds x to the next power of two.
func NextPowerOf2(x int) int { return 1 << uint(bits.Len(uint(x))) }

// CeilByte rounds size to the next multiple of 8.
func CeilByte(size int) int { return (size + 7) &^ 7 }

// CeilByte64 rounds size to the next multiple of 8.
func CeilByte64(size int64) int64 { return (size + 7) &^ 7 }

// BitIsSet returns true if the bit at index i in buf is set (1).
func BitIsSet(buf []byte, i int) bool { return (buf[uint(i)/8] & BitMask[byte(i)%8]) != 0 }

// BitIsNotSet returns true if the bit at index i in buf is not set (0).
func BitIsNotSet(buf []byte, i int) bool { return (buf[uint(i)/8] & BitMask[byte(i)%8]) == 0 }

// SetBit sets the bit at index i in buf to 1.
func SetBit(buf []byte, i int) { buf[uint(i)/8] |= BitMask[byte(i)%8] }

// ClearBit sets the bit at index i in buf to 0.
func ClearBit(buf []byte, i int) { buf[uint(i)/8] &= FlippedBitMask[byte(i)%8] }

// SetBitSwap sets the bit at index i in buf to 1 and returns whether it was previously set.
func SetBitSwap(buf []byte, i int) bool {
	p := &buf[uint(i)/8]
	mask := BitMask[byte(i)%8]
	old := *p
	*p = old | mask
	return old&mask != 0
}

// ClearBitSwap sets the bit at index i in buf to 0 and returns whether it was previously set.
func ClearBitSwap(buf []byte, i int) bool {
	p := &buf[uint(i)/8]
	mask := BitMask[byte(i)%8]
	old := *p
	*p = old &^ mask
	return old&mask != 0
}

// SetBitTo sets the bit at index i in buf to val.
func SetBitTo(buf []byte, i int, val bool) {
	if val {
		SetBit(buf, i)
	} else {
		ClearBit(buf, i)
	}
}

// CountSetBits counts the number of 1's in buf up to n bits.
func CountSetBits(buf []byte, offset, n int) int {
	if offset > 0 {
		return countSetBitsWithOffset(buf, offset, n)
	}

	count := 0

	uint64Bytes := n / uint64SizeBits * 8
	for _, v := range bytesToUint64(buf[:uint64Bytes]) {
		count += bits.OnesCount64(v)
	}

	for _, v := range buf[uint64Bytes : n/8] {
		count += bits.OnesCount8(v)
	}

	// tail bits
	for i := n &^ 0x7; i < n; i++ {
		if BitIsSet(buf, i) {
			count++
		}
	}

	return count
}

// BitmapAllSet reports whether all bits in the requested range are set.
// For n == 0, it returns true for any buf and offset. Otherwise, buf must
// cover offset+n bits or BitmapAllSet panics, matching CountSetBits.
func BitmapAllSet(buf []byte, offset, n int) bool {
	if n == 0 {
		return true
	}
	// Validate the full range before returning early for a clear bit.
	_ = buf[(uint(offset)+uint(n)-1)/8]
	// Preserve the cheapest early exit for a null at the start.
	if !BitIsSet(buf, offset) {
		return false
	}
	if offset&7 != 0 {
		leading := min(8-(offset&7), n)
		mask := byte((1<<leading)-1) << (offset & 7)
		if buf[offset/8]&mask != mask {
			return false
		}
		offset += leading
		n -= leading
	}
	end := offset/8 + n/8
	body := buf[offset/8 : end]
	// One test per 512 bits keeps long all-valid scans branch-light.
	if len(body) >= 64 {
		bulkBytes := len(body) &^ 63
		words := bytesToUint64(body[:bulkBytes])
		for len(words) >= 8 {
			if words[0]&words[1]&words[2]&words[3]&words[4]&words[5]&words[6]&words[7] != ^uint64(0) {
				return false
			}
			words = words[8:]
		}
		body = body[bulkBytes:]
	}
	for len(body) >= 8 {
		if binary.LittleEndian.Uint64(body) != ^uint64(0) {
			return false
		}
		body = body[8:]
	}
	for _, v := range body {
		if v != 255 {
			return false
		}
	}
	if tail := n & 7; tail != 0 {
		mask := byte(1<<tail) - 1
		return buf[end]&mask == mask
	}
	return true
}

func countSetBitsWithOffset(buf []byte, offset, n int) int {
	if n == 0 {
		return 0
	}

	byteOffset := offset / 8
	bitOffset := offset & 7
	if bitOffset == 0 {
		return CountSetBits(buf[byteOffset:], 0, n)
	}

	// Count the partial first byte, then use the word-based path for the rest.
	leading := min(n, 8-bitOffset)
	mask := byte((1<<leading)-1) << bitOffset
	count := bits.OnesCount8(buf[byteOffset] & mask)
	if leading == n {
		return count
	}
	return count + CountSetBits(buf[byteOffset+1:], 0, n-leading)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const (
	uint64SizeBytes = int(unsafe.Sizeof(uint64(0)))
	uint64SizeBits  = uint64SizeBytes * 8
)

var (
	// PrecedingBitmask is a convenience set of values as bitmasks for checking
	// prefix bits of a byte
	PrecedingBitmask = [8]byte{0, 1, 3, 7, 15, 31, 63, 127}
	// TrailingBitmask is the bitwise complement version of kPrecedingBitmask
	TrailingBitmask = [8]byte{255, 254, 252, 248, 240, 224, 192, 128}
)

// SetBitsTo is a convenience function to quickly set or unset all the bits
// in a bitmap starting at startOffset for length bits.
func SetBitsTo(bits []byte, startOffset, length int64, areSet bool) {
	if length == 0 {
		return
	}

	beg := startOffset
	end := startOffset + length
	var fill uint8 = 0
	if areSet {
		fill = math.MaxUint8
	}

	byteBeg := beg / 8
	byteEnd := end/8 + 1

	// don't modify bits before the startOffset by using this mask
	firstByteMask := PrecedingBitmask[beg%8]
	// don't modify bits past the length by using this mask
	lastByteMask := TrailingBitmask[end%8]

	if byteEnd == byteBeg+1 {
		// set bits within a single byte
		onlyByteMask := firstByteMask
		if end%8 != 0 {
			onlyByteMask = firstByteMask | lastByteMask
		}

		bits[byteBeg] &= onlyByteMask
		bits[byteBeg] |= fill &^ onlyByteMask
		return
	}

	// set/clear trailing bits of first byte
	bits[byteBeg] &= firstByteMask
	bits[byteBeg] |= fill &^ firstByteMask

	if byteEnd-byteBeg > 2 {
		memory.Set(bits[byteBeg+1:byteEnd-1], fill)
	}

	if end%8 == 0 {
		return
	}

	bits[byteEnd-1] &= lastByteMask
	bits[byteEnd-1] |= fill &^ lastByteMask
}
