// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package encoding

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
)

// Encoding functions append bytes to the provided *[]byte, allowing avoiding
// allocations if the slice initially has a large enough capacity.
// Decoding functions also take *[]byte as input, and when they do not return an
// error, advance the slice so that it starts at the immediate byte after the
// decoded part (or so that it is empty if there is no such byte).

const (
	MaxVarLen64      = 9
	varfloat64Rotate = 6
)

var uvarint64Sizes = initUvarint64Sizes()
var varfloat64Sizes = initVarfloat64Sizes()

// EncodeUvarint64 serializes 64-bit unsigned integers 7 bits at a time,
// starting with the least significant bits. The most significant bit in each
// output byte is the continuation bit and indicates whether there are
// additional non-zero bits encoded in following bytes. There are at most 9
// output bytes and the last one does not have a continuation bit, allowing for
// it to encode 8 bits (8*7+8 = 64).
func EncodeUvarint64(b *[]byte, v uint64) {
	for i := 0; i < MaxVarLen64-1; i++ {
		if v < 0x80 {
			break
		}
		*b = append(*b, byte(v)|byte(0x80))
		v >>= 7
	}
	*b = append(*b, byte(v))
}

// DecodeUvarint64 deserializes 64-bit unsigned integers that have been encoded
// using EncodeUvarint64.
func DecodeUvarint64(b *[]byte) (uint64, error) {
	x := uint64(0)
	s := uint(0)
	for i := 0; ; i++ {
		if len(*b) <= i {
			return 0, io.EOF
		}
		n := (*b)[i]
		if n < 0x80 || i == MaxVarLen64-1 {
			*b = (*b)[i+1:]
			return x | uint64(n)<<s, nil
		}
		x |= uint64(n&0x7F) << s
		s += 7
	}
}

// Uvarint64Size returns the number of bytes that EncodeUvarint64 encodes a
// 64-bit unsigned integer into.
func Uvarint64Size(v uint64) int {
	return uvarint64Sizes[bits.LeadingZeros64(v)]
}

func initUvarint64Sizes() [65]int {
	var sizes [65]int
	b := []byte{}
	for i := 0; i <= 64; i++ {
		b = b[:0]
		EncodeUvarint64(&b, ^uint64(0)>>i)
		sizes[i] = len(b)
	}
	return sizes
}

// EncodeVarint64 serializes 64-bit signed integers using zig-zag encoding,
// which ensures small-scale integers are turned into unsigned integers that
// have leading zeros, whether they are positive or negative, hence allows for
// space-efficient varuint encoding of those values.
func EncodeVarint64(b *[]byte, v int64) {
	EncodeUvarint64(b, uint64(v>>(64-1)^(v<<1)))
}

// DecodeVarint64 deserializes 64-bit signed integers that have been encoded
// using EncodeVarint32.
func DecodeVarint64(b *[]byte) (int64, error) {
	v, err := DecodeUvarint64(b)
	return int64((v >> 1) ^ -(v & 1)), err
}

// Varint64Size returns the number of bytes that EncodeVarint64 encodes a 64-bit
// signed integer into.
func Varint64Size(v int64) int {
	return Uvarint64Size(uint64(v>>(64-1) ^ (v << 1)))
}

var errVarint32Overflow = errors.New("varint overflows a 32-bit integer")

// DecodeVarint32 deserializes 32-bit signed integers that have been encoded
// using EncodeVarint64.
func DecodeVarint32(b *[]byte) (int32, error) {
	v, err := DecodeVarint64(b)
	if err != nil {
		return 0, err
	}
	if v > math.MaxInt32 || v < math.MinInt32 {
		return 0, errVarint32Overflow
	}
	return int32(v), nil
}

// EncodeFloat64LE serializes 64-bit floating-point values, starting with the
// least significant bytes.
func EncodeFloat64LE(b *[]byte, v float64) {
	*b = append(*b, make([]byte, 8)...)
	binary.LittleEndian.PutUint64((*b)[len(*b)-8:], math.Float64bits(v))
}

// DecodeFloat64LE deserializes 64-bit floating-point values that have been
// encoded with EncodeFloat64LE.
func DecodeFloat64LE(b *[]byte) (float64, error) {
	if len(*b) < 8 {
		return 0, io.EOF
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(*b))
	*b = (*b)[8:]
	return v, nil
}

// EncodeVarfloat64 serializes 64-bit floating-point values using a method that
// is similar to the varuint encoding and that is space-efficient for
// non-negative integer values. The output takes at most 9 bytes.
// Input values are first shifted as floating-point values (+1), then transmuted
// to integer values, then shifted again as integer values (-Float64bits(1)).
// That is in order to minimize the number of non-zero bits when dealing with
// non-negative integer values.
// After that transformation, any input integer value no greater than 2^53 (the
// largest integer value that can be encoded exactly as a 64-bit floating-point
// value) will have at least 6 leading zero bits. By rotating bits to the left,
// those bits end up at the right of the binary representation.
// The resulting bits are then encoded similarly to the varuint method, but
// starting with the most significant bits.
func EncodeVarfloat64(b *[]byte, v float64) {
	x := bits.RotateLeft64(math.Float64bits(v+1)-math.Float64bits(1), varfloat64Rotate)
	for i := 0; i < MaxVarLen64-1; i++ {
		n := byte(x >> (8*8 - 7))
		x <<= 7
		if x == 0 {
			*b = append(*b, n)
			return
		}
		*b = append(*b, n|byte(0x80))
	}
	n := byte(x >> (8 * 7))
	*b = append(*b, n)
}

// DecodeVarfloat64 deserializes 64-bit floating-point values that have been
// encoded with EncodeVarfloat64.
func DecodeVarfloat64(b *[]byte) (float64, error) {
	x := uint64(0)
	i := int(0)
	s := uint(8*8 - 7)
	for {
		if len(*b) <= i {
			return 0, io.EOF
		}
		n := (*b)[i]
		if i == MaxVarLen64-1 {
			x |= uint64(n)
			break
		}
		if n < 0x80 {
			x |= uint64(n) << s
			break
		}
		x |= uint64(n&0x7F) << s
		i++
		s -= 7
	}
	*b = (*b)[i+1:]
	return math.Float64frombits(bits.RotateLeft64(x, -varfloat64Rotate)+math.Float64bits(1)) - 1, nil
}

// Varfloat64Size returns the number of bytes that EncodeVarfloat64 encodes a
// 64-bit floating-point value into.
func Varfloat64Size(v float64) int {
	x := bits.RotateLeft64(math.Float64bits(v+1)-math.Float64bits(1), varfloat64Rotate)
	return varfloat64Sizes[bits.TrailingZeros64(x)]
}

func initVarfloat64Sizes() [65]int {
	var sizes [65]int
	b := []byte{}
	for i := 0; i <= 64; i++ {
		b = b[:0]
		EncodeVarfloat64(&b, math.Float64frombits(bits.RotateLeft64(^uint64(0)<<i, -varfloat64Rotate)+math.Float64bits(1))-1)
		sizes[i] = len(b)
	}
	return sizes
}
