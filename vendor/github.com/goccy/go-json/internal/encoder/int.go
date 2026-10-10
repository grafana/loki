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
	"math/bits"
	"slices"
	"unsafe"
)

var endianness int

// init sets endianness by the first byte of 0xABCD in memory: 0xCD on a little-endian machine (0) and 0xAB on a
// big-endian one (1), the only two orders of the bytes of a word which Go runs on.
func init() {
	var b [2]byte
	*(*uint16)(unsafe.Pointer(&b)) = uint16(0xABCD)
	if b[0] == 0xAB {
		endianness = 1
	}
}

// "00010203...96979899" cast to []uint16
var intLELookup = [100]uint16{
	0x3030, 0x3130, 0x3230, 0x3330, 0x3430, 0x3530, 0x3630, 0x3730, 0x3830, 0x3930,
	0x3031, 0x3131, 0x3231, 0x3331, 0x3431, 0x3531, 0x3631, 0x3731, 0x3831, 0x3931,
	0x3032, 0x3132, 0x3232, 0x3332, 0x3432, 0x3532, 0x3632, 0x3732, 0x3832, 0x3932,
	0x3033, 0x3133, 0x3233, 0x3333, 0x3433, 0x3533, 0x3633, 0x3733, 0x3833, 0x3933,
	0x3034, 0x3134, 0x3234, 0x3334, 0x3434, 0x3534, 0x3634, 0x3734, 0x3834, 0x3934,
	0x3035, 0x3135, 0x3235, 0x3335, 0x3435, 0x3535, 0x3635, 0x3735, 0x3835, 0x3935,
	0x3036, 0x3136, 0x3236, 0x3336, 0x3436, 0x3536, 0x3636, 0x3736, 0x3836, 0x3936,
	0x3037, 0x3137, 0x3237, 0x3337, 0x3437, 0x3537, 0x3637, 0x3737, 0x3837, 0x3937,
	0x3038, 0x3138, 0x3238, 0x3338, 0x3438, 0x3538, 0x3638, 0x3738, 0x3838, 0x3938,
	0x3039, 0x3139, 0x3239, 0x3339, 0x3439, 0x3539, 0x3639, 0x3739, 0x3839, 0x3939,
}

var intBELookup = [100]uint16{
	0x3030, 0x3031, 0x3032, 0x3033, 0x3034, 0x3035, 0x3036, 0x3037, 0x3038, 0x3039,
	0x3130, 0x3131, 0x3132, 0x3133, 0x3134, 0x3135, 0x3136, 0x3137, 0x3138, 0x3139,
	0x3230, 0x3231, 0x3232, 0x3233, 0x3234, 0x3235, 0x3236, 0x3237, 0x3238, 0x3239,
	0x3330, 0x3331, 0x3332, 0x3333, 0x3334, 0x3335, 0x3336, 0x3337, 0x3338, 0x3339,
	0x3430, 0x3431, 0x3432, 0x3433, 0x3434, 0x3435, 0x3436, 0x3437, 0x3438, 0x3439,
	0x3530, 0x3531, 0x3532, 0x3533, 0x3534, 0x3535, 0x3536, 0x3537, 0x3538, 0x3539,
	0x3630, 0x3631, 0x3632, 0x3633, 0x3634, 0x3635, 0x3636, 0x3637, 0x3638, 0x3639,
	0x3730, 0x3731, 0x3732, 0x3733, 0x3734, 0x3735, 0x3736, 0x3737, 0x3738, 0x3739,
	0x3830, 0x3831, 0x3832, 0x3833, 0x3834, 0x3835, 0x3836, 0x3837, 0x3838, 0x3839,
	0x3930, 0x3931, 0x3932, 0x3933, 0x3934, 0x3935, 0x3936, 0x3937, 0x3938, 0x3939,
}

var intLookup = [2]*[100]uint16{&intLELookup, &intBELookup}

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
		u := intLELookup[n]
		return append(out, byte(u), byte(u>>8))
	} else if n < 10000 {
		return appendThreeOrFourDigits(out, n)
	}
	return appendDecimal(out, n, negative)
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
		u := intLELookup[n]
		return append(out, byte(u), byte(u>>8))
	} else if n < 10000 {
		return appendThreeOrFourDigits(out, n)
	}
	return appendDecimal(out, n, false)
}

// appendThreeOrFourDigits appends n, of three or four digits, by the lookups of its digits without a loop: most of
// the integers of JSON are small.
func appendThreeOrFourDigits(out []byte, n uint64) []byte {
	hi, lo := n/100, intLELookup[n%100]
	if hi < 10 {
		return append(out, byte(hi+'0'), byte(lo), byte(lo>>8))
	}
	u := intLELookup[hi]
	return append(out, byte(u), byte(u>>8), byte(lo), byte(lo>>8))
}

// pow10 are the powers of 10 which an uint64 has, from 10^0.
var pow10 = [20]uint64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// decimalDigits returns the number of the decimal digits of n: log10 of n by the one of 2, which bits.Len64
// gives, corrected by the power of 10 it may be short of ( 1233/4096 is log10(2) ).
func decimalDigits(n uint64) int {
	n |= 1
	t := bits.Len64(n) * 1233 >> 12
	if n < pow10[t] {
		return t
	}
	return t + 1
}

// appendUpToEightDigits appends n, a positive number of up to eight digits, in decimal.
func appendUpToEightDigits(out []byte, n uint64) []byte {
	if n < 10000 {
		if n < 10 {
			return append(out, byte(n+'0'))
		} else if n < 100 {
			u := intLELookup[n]
			return append(out, byte(u), byte(u>>8))
		}
		return appendThreeOrFourDigits(out, n)
	}
	hi, lo := n/10000, n%10000
	switch {
	case hi < 10:
		out = append(out, byte(hi+'0'))
	case hi < 100:
		u := intLELookup[hi]
		out = append(out, byte(u), byte(u>>8))
	default:
		out = appendThreeOrFourDigits(out, hi)
	}
	a, b := intLELookup[lo/100], intLELookup[lo%100]
	return append(out, byte(a), byte(a>>8), byte(b), byte(b>>8))
}

// appendDecimal appends n in decimal, after a minus sign if negative: a positive number of five digits or more,
// or any negative one. A number of up to sixteen digits is written by the lookups of its digits, the one of up to
// eight digits as appendUpToEightDigits does, without its call; the digits of a larger one are written where they
// go in out, from the last, so that nothing is copied: the number of the digits is known first.
func appendDecimal(out []byte, n uint64, negative bool) []byte {
	if n < 100000000 {
		if negative {
			out = append(out, '-')
		}
		if n < 10000 {
			// a negative number of up to four digits: the positive ones are written by their callers.
			if n < 10 {
				return append(out, byte(n+'0'))
			} else if n < 100 {
				u := intLELookup[n]
				return append(out, byte(u), byte(u>>8))
			}
			return appendThreeOrFourDigits(out, n)
		}
		// up to eight digits, as appendUpToEightDigits writes them: the first ones and the last four, of a
		// division by 10000, by their lookups.
		hi, lo := n/10000, n%10000
		switch {
		case hi < 10:
			out = append(out, byte(hi+'0'))
		case hi < 100:
			u := intLELookup[hi]
			out = append(out, byte(u), byte(u>>8))
		default:
			out = appendThreeOrFourDigits(out, hi)
		}
		a, b := intLELookup[lo/100], intLELookup[lo%100]
		return append(out, byte(a), byte(a>>8), byte(b), byte(b>>8))
	}
	if n < 10000000000000000 {
		// up to sixteen digits: the first ones of a division by 100000000 as a number of up to eight digits,
		// then the last eight by their lookups.
		hi, lo := n/100000000, n%100000000
		if negative {
			out = append(out, '-')
		}
		out = appendUpToEightDigits(out, hi)
		c, d := lo/10000, lo%10000
		a, b, e, f := intLELookup[c/100], intLELookup[c%100], intLELookup[d/100], intLELookup[d%100]
		return append(out, byte(a), byte(a>>8), byte(b), byte(b>>8), byte(e), byte(e>>8), byte(f), byte(f>>8))
	}
	d := decimalDigits(n)
	if negative {
		d++
	}
	l := len(out)
	if cap(out)-l < d {
		out = slices.Grow(out, d)
	}
	out = out[:l+d]
	base := unsafe.Pointer(unsafe.SliceData(out))
	lookup := intLookup[endianness]
	i := l + d
	// four digits at a time: a division by 10000 costs what one by 100 does, so there are half as many.
	for n >= 10000 {
		q := n / 10000
		r := n - q*10000
		n = q
		i -= 4
		*(*uint16)(unsafe.Add(base, i)) = lookup[r/100]
		*(*uint16)(unsafe.Add(base, i+2)) = lookup[r%100]
	}
	if n >= 100 {
		j := n % 100
		n /= 100
		i -= 2
		*(*uint16)(unsafe.Add(base, i)) = lookup[j]
	}
	if n >= 10 {
		i -= 2
		*(*uint16)(unsafe.Add(base, i)) = lookup[n]
	} else {
		i--
		*(*byte)(unsafe.Add(base, i)) = byte(n + '0')
	}
	if negative {
		*(*byte)(unsafe.Add(base, l)) = '-'
	}
	return out
}
