// This files's processing codes are inspired by https://github.com/segmentio/encoding.
// The license notation is as follows.
//
// # MIT License
//
// Copyright (c) 2019 Segment.io, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
package encoder

import (
	"unsafe"

	"github.com/goccy/go-json/internal/intfmt"
)

func numMask(numBitSize uint8) uint64 {
	return 1<<numBitSize - 1
}

func AppendInt(_ *RuntimeContext, out []byte, p unsafe.Pointer, code *Opcode) []byte {
	var u64 uint64
	switch code.NumBitSize {
	case 8:
		u64 = uint64(*(*uint8)(p))
	case 16:
		u64 = uint64(*(*uint16)(p))
	case 32:
		u64 = uint64(*(*uint32)(p))
	case 64:
		u64 = *(*uint64)(p)
	}
	mask := numMask(code.NumBitSize)
	n := u64 & mask
	negative := (u64>>(code.NumBitSize-1))&1 == 1
	if negative {
		n = -n & mask
	} else if n < 10 {
		return append(out, byte(n+'0'))
	} else if n < 100 {
		u := intfmt.DigitPairs[n]
		return append(out, byte(u), byte(u>>8))
	} else if n < 10000 {
		return intfmt.AppendThreeOrFourDigits(out, n)
	}
	return intfmt.AppendDecimal(out, n, negative)
}

func AppendUint(_ *RuntimeContext, out []byte, p unsafe.Pointer, code *Opcode) []byte {
	var u64 uint64
	switch code.NumBitSize {
	case 8:
		u64 = uint64(*(*uint8)(p))
	case 16:
		u64 = uint64(*(*uint16)(p))
	case 32:
		u64 = uint64(*(*uint32)(p))
	case 64:
		u64 = *(*uint64)(p)
	}
	n := u64 & numMask(code.NumBitSize)
	if n < 10 {
		return append(out, byte(n+'0'))
	} else if n < 100 {
		u := intfmt.DigitPairs[n]
		return append(out, byte(u), byte(u>>8))
	} else if n < 10000 {
		return intfmt.AppendThreeOrFourDigits(out, n)
	}
	return intfmt.AppendDecimal(out, n, false)
}
