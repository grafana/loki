// Package kbin contains Kafka primitive reading and writing functions.
package kbin

import (
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
	"reflect"
	"unsafe"
)

// This file contains primitive type encoding and decoding.
//
// The Reader helper can be used even when content runs out
// or an error is hit; all other number requests will return
// zero so a decode will basically no-op.

// ErrNotEnoughData is returned when a type could not fully decode
// from a slice because the slice did not have enough data.
var ErrNotEnoughData = errors.New("response did not contain enough data to be valid")

// AppendBool appends 1 for true or 0 for false to dst.
func AppendBool(dst []byte, v bool) []byte {
	if v {
		return append(dst, 1)
	}
	return append(dst, 0)
}

// AppendInt8 appends an int8 to dst.
func AppendInt8(dst []byte, i int8) []byte {
	return append(dst, byte(i))
}

// AppendInt16 appends a big endian int16 to dst.
func AppendInt16(dst []byte, i int16) []byte {
	return AppendUint16(dst, uint16(i))
}

// AppendUint16 appends a big endian uint16 to dst.
func AppendUint16(dst []byte, u uint16) []byte {
	return append(dst, byte(u>>8), byte(u))
}

// AppendInt32 appends a big endian int32 to dst.
func AppendInt32(dst []byte, i int32) []byte {
	return AppendUint32(dst, uint32(i))
}

// AppendInt64 appends a big endian int64 to dst.
func AppendInt64(dst []byte, i int64) []byte {
	return appendUint64(dst, uint64(i))
}

// AppendFloat64 appends a big endian float64 to dst.
func AppendFloat64(dst []byte, f float64) []byte {
	return appendUint64(dst, math.Float64bits(f))
}

// AppendUuid appends the 16 uuid bytes to dst.
func AppendUuid(dst []byte, uuid [16]byte) []byte {
	return append(dst, uuid[:]...)
}

func appendUint64(dst []byte, u uint64) []byte {
	return append(dst, byte(u>>56), byte(u>>48), byte(u>>40), byte(u>>32),
		byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
}

// AppendUint32 appends a big endian uint32 to dst.
func AppendUint32(dst []byte, u uint32) []byte {
	return append(dst, byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
}

// uvarintLens could only be length 65, but using 256 allows bounds check
// elimination on lookup.
const uvarintLens = "\x01\x01\x01\x01\x01\x01\x01\x01\x02\x02\x02\x02\x02\x02\x02\x03\x03\x03\x03\x03\x03\x03\x04\x04\x04\x04\x04\x04\x04\x05\x05\x05\x05\x05\x05\x05\x06\x06\x06\x06\x06\x06\x06\x07\x07\x07\x07\x07\x07\x07\x08\x08\x08\x08\x08\x08\x08\x09\x09\x09\x09\x09\x09\x09\x0a\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"

// VarintLen returns how long i would be if it were varint encoded.
func VarintLen(i int32) int {
	u := uint32(i)<<1 ^ uint32(i>>31)
	return UvarintLen(u)
}

// UvarintLen returns how long u would be if it were uvarint encoded.
func UvarintLen(u uint32) int {
	return int(uvarintLens[byte(bits.Len32(u))])
}

func uvarlongLen(u uint64) int {
	return int(uvarintLens[byte(bits.Len64(u))])
}

// Varint is a loop unrolled 32 bit varint decoder. The return semantics
// are the same as binary.Varint, with the added benefit that overflows
// in 5 byte encodings are handled rather than left to the user.
func Varint(in []byte) (int32, int) {
	x, n := Uvarint(in)
	return int32((x >> 1) ^ -(x & 1)), n
}

// Uvarint is a loop unrolled 32 bit uvarint decoder. The return semantics
// are the same as binary.Uvarint, with the added benefit that overflows
// in 5 byte encodings are handled rather than left to the user.
func Uvarint(in []byte) (uint32, int) {
	var x uint32
	var overflow int

	if len(in) < 1 {
		goto fail
	}

	x = uint32(in[0] & 0x7f)
	if in[0]&0x80 == 0 {
		return x, 1
	} else if len(in) < 2 {
		goto fail
	}

	x |= uint32(in[1]&0x7f) << 7
	if in[1]&0x80 == 0 {
		return x, 2
	} else if len(in) < 3 {
		goto fail
	}

	x |= uint32(in[2]&0x7f) << 14
	if in[2]&0x80 == 0 {
		return x, 3
	} else if len(in) < 4 {
		goto fail
	}

	x |= uint32(in[3]&0x7f) << 21
	if in[3]&0x80 == 0 {
		return x, 4
	} else if len(in) < 5 {
		goto fail
	}

	x |= uint32(in[4]) << 28
	if in[4] <= 0x0f {
		return x, 5
	}

	overflow = -5

fail:
	return 0, overflow
}

// Varlong is a loop unrolled 64 bit varint decoder. The return semantics
// are the same as binary.Varint, with the added benefit that overflows
// in 10 byte encodings are handled rather than left to the user.
func Varlong(in []byte) (int64, int) {
	x, n := uvarlong(in)
	return int64((x >> 1) ^ -(x & 1)), n
}

func uvarlong(in []byte) (uint64, int) {
	var x uint64
	var overflow int

	if len(in) < 1 {
		goto fail
	}

	x = uint64(in[0] & 0x7f)
	if in[0]&0x80 == 0 {
		return x, 1
	} else if len(in) < 2 {
		goto fail
	}

	x |= uint64(in[1]&0x7f) << 7
	if in[1]&0x80 == 0 {
		return x, 2
	} else if len(in) < 3 {
		goto fail
	}

	x |= uint64(in[2]&0x7f) << 14
	if in[2]&0x80 == 0 {
		return x, 3
	} else if len(in) < 4 {
		goto fail
	}

	x |= uint64(in[3]&0x7f) << 21
	if in[3]&0x80 == 0 {
		return x, 4
	} else if len(in) < 5 {
		goto fail
	}

	x |= uint64(in[4]&0x7f) << 28
	if in[4]&0x80 == 0 {
		return x, 5
	} else if len(in) < 6 {
		goto fail
	}

	x |= uint64(in[5]&0x7f) << 35
	if in[5]&0x80 == 0 {
		return x, 6
	} else if len(in) < 7 {
		goto fail
	}

	x |= uint64(in[6]&0x7f) << 42
	if in[6]&0x80 == 0 {
		return x, 7
	} else if len(in) < 8 {
		goto fail
	}

	x |= uint64(in[7]&0x7f) << 49
	if in[7]&0x80 == 0 {
		return x, 8
	} else if len(in) < 9 {
		goto fail
	}

	x |= uint64(in[8]&0x7f) << 56
	if in[8]&0x80 == 0 {
		return x, 9
	} else if len(in) < 10 {
		goto fail
	}

	x |= uint64(in[9]) << 63
	if in[9] <= 0x01 {
		return x, 10
	}

	overflow = -10

fail:
	return 0, overflow
}

// AppendVarint appends a varint encoded i to dst.
func AppendVarint(dst []byte, i int32) []byte {
	return AppendUvarint(dst, uint32(i)<<1^uint32(i>>31))
}

// AppendUvarint appends a uvarint encoded u to dst.
func AppendUvarint(dst []byte, u uint32) []byte {
	switch UvarintLen(u) {
	case 5:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte(u>>28))
	case 4:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte(u>>21))
	case 3:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte(u>>14))
	case 2:
		return append(dst,
			byte(u&0x7f|0x80),
			byte(u>>7))
	case 1:
		return append(dst, byte(u))
	}
	return dst
}

// AppendVarlong appends a varint encoded i to dst.
func AppendVarlong(dst []byte, i int64) []byte {
	return appendUvarlong(dst, uint64(i)<<1^uint64(i>>63))
}

func appendUvarlong(dst []byte, u uint64) []byte {
	switch uvarlongLen(u) {
	case 10:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte((u>>28)&0x7f|0x80),
			byte((u>>35)&0x7f|0x80),
			byte((u>>42)&0x7f|0x80),
			byte((u>>49)&0x7f|0x80),
			byte((u>>56)&0x7f|0x80),
			byte(u>>63))
	case 9:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte((u>>28)&0x7f|0x80),
			byte((u>>35)&0x7f|0x80),
			byte((u>>42)&0x7f|0x80),
			byte((u>>49)&0x7f|0x80),
			byte(u>>56))
	case 8:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte((u>>28)&0x7f|0x80),
			byte((u>>35)&0x7f|0x80),
			byte((u>>42)&0x7f|0x80),
			byte(u>>49))
	case 7:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte((u>>28)&0x7f|0x80),
			byte((u>>35)&0x7f|0x80),
			byte(u>>42))
	case 6:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte((u>>28)&0x7f|0x80),
			byte(u>>35))
	case 5:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte((u>>21)&0x7f|0x80),
			byte(u>>28))
	case 4:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte((u>>14)&0x7f|0x80),
			byte(u>>21))
	case 3:
		return append(dst,
			byte(u&0x7f|0x80),
			byte((u>>7)&0x7f|0x80),
			byte(u>>14))
	case 2:
		return append(dst,
			byte(u&0x7f|0x80),
			byte(u>>7))
	case 1:
		return append(dst, byte(u))
	}
	return dst
}

// AppendString appends a string to dst prefixed with its int16 length.
func AppendString(dst []byte, s string) []byte {
	dst = AppendInt16(dst, int16(len(s)))
	return append(dst, s...)
}

// AppendCompactString appends a string to dst prefixed with its uvarint length
// starting at 1; 0 is reserved for null, which compact strings are not
// (nullable compact ones are!). Thus, the length is the decoded uvarint - 1.
//
// For KIP-482.
func AppendCompactString(dst []byte, s string) []byte {
	dst = AppendUvarint(dst, 1+uint32(len(s)))
	return append(dst, s...)
}

// AppendNullableString appends potentially nil string to dst prefixed with its
// int16 length or int16(-1) if nil.
func AppendNullableString(dst []byte, s *string) []byte {
	if s == nil {
		return AppendInt16(dst, -1)
	}
	return AppendString(dst, *s)
}

// AppendCompactNullableString appends a potentially nil string to dst with its
// uvarint length starting at 1, with 0 indicating null. Thus, the length is
// the decoded uvarint - 1.
//
// For KIP-482.
func AppendCompactNullableString(dst []byte, s *string) []byte {
	if s == nil {
		return AppendUvarint(dst, 0)
	}
	return AppendCompactString(dst, *s)
}

// AppendBytes appends bytes to dst prefixed with its int32 length.
func AppendBytes(dst, b []byte) []byte {
	dst = AppendInt32(dst, int32(len(b)))
	return append(dst, b...)
}

// AppendCompactBytes appends bytes to dst prefixed with a its uvarint length
// starting at 1; 0 is reserved for null, which compact bytes are not (nullable
// compact ones are!). Thus, the length is the decoded uvarint - 1.
//
// For KIP-482.
func AppendCompactBytes(dst, b []byte) []byte {
	dst = AppendUvarint(dst, 1+uint32(len(b)))
	return append(dst, b...)
}

// AppendNullableBytes appends a potentially nil slice to dst prefixed with its
// int32 length or int32(-1) if nil.
func AppendNullableBytes(dst, b []byte) []byte {
	if b == nil {
		return AppendInt32(dst, -1)
	}
	return AppendBytes(dst, b)
}

// AppendCompactNullableBytes appends a potentially nil slice to dst with its
// uvarint length starting at 1, with 0 indicating null. Thus, the length is
// the decoded uvarint - 1.
//
// For KIP-482.
func AppendCompactNullableBytes(dst, b []byte) []byte {
	if b == nil {
		return AppendUvarint(dst, 0)
	}
	return AppendCompactBytes(dst, b)
}

// AppendVarintString appends a string to dst prefixed with its length encoded
// as a varint.
func AppendVarintString(dst []byte, s string) []byte {
	dst = AppendVarint(dst, int32(len(s)))
	return append(dst, s...)
}

// AppendVarintBytes appends a slice to dst prefixed with its length encoded as
// a varint.
func AppendVarintBytes(dst, b []byte) []byte {
	if b == nil {
		return AppendVarint(dst, -1)
	}
	dst = AppendVarint(dst, int32(len(b)))
	return append(dst, b...)
}

// AppendArrayLen appends the length of an array as an int32 to dst.
func AppendArrayLen(dst []byte, l int) []byte {
	return AppendInt32(dst, int32(l))
}

// AppendCompactArrayLen appends the length of an array as a uvarint to dst
// as the length + 1.
//
// For KIP-482.
func AppendCompactArrayLen(dst []byte, l int) []byte {
	return AppendUvarint(dst, 1+uint32(l))
}

// AppendNullableArrayLen appends the length of an array as an int32 to dst,
// or -1 if isNil is true.
func AppendNullableArrayLen(dst []byte, l int, isNil bool) []byte {
	if isNil {
		return AppendInt32(dst, -1)
	}
	return AppendInt32(dst, int32(l))
}

// AppendCompactNullableArrayLen appends the length of an array as a uvarint to
// dst as the length + 1; if isNil is true, this appends 0 as a uvarint.
//
// For KIP-482.
func AppendCompactNullableArrayLen(dst []byte, l int, isNil bool) []byte {
	if isNil {
		return AppendUvarint(dst, 0)
	}
	return AppendUvarint(dst, 1+uint32(l))
}

// Reader is used to decode Kafka messages.
//
// For all functions on Reader, if the reader has been invalidated, functions
// return defaults (false, 0, nil, ""). Use Complete to detect if the reader
// was invalidated or if the reader has remaining data.
type Reader struct {
	Src []byte
	bad bool
}

// Bool returns a bool from the reader.
func (b *Reader) Bool() bool {
	if len(b.Src) < 1 {
		b.bad = true
		b.Src = nil
		return false
	}
	t := b.Src[0] != 0 // if '0', false
	b.Src = b.Src[1:]
	return t
}

// Int8 returns an int8 from the reader.
func (b *Reader) Int8() int8 {
	if len(b.Src) < 1 {
		b.bad = true
		b.Src = nil
		return 0
	}
	r := b.Src[0]
	b.Src = b.Src[1:]
	return int8(r)
}

// Int16 returns an int16 from the reader.
func (b *Reader) Int16() int16 {
	if len(b.Src) < 2 {
		b.bad = true
		b.Src = nil
		return 0
	}
	r := int16(binary.BigEndian.Uint16(b.Src))
	b.Src = b.Src[2:]
	return r
}

// Uint16 returns an uint16 from the reader.
func (b *Reader) Uint16() uint16 {
	if len(b.Src) < 2 {
		b.bad = true
		b.Src = nil
		return 0
	}
	r := binary.BigEndian.Uint16(b.Src)
	b.Src = b.Src[2:]
	return r
}

// Int32 returns an int32 from the reader.
func (b *Reader) Int32() int32 {
	if len(b.Src) < 4 {
		b.bad = true
		b.Src = nil
		return 0
	}
	r := int32(binary.BigEndian.Uint32(b.Src))
	b.Src = b.Src[4:]
	return r
}

// Int64 returns an int64 from the reader.
func (b *Reader) Int64() int64 {
	return int64(b.readUint64())
}

// Uuid returns a uuid from the reader.
func (b *Reader) Uuid() [16]byte {
	var r [16]byte
	copy(r[:], b.Span(16))
	return r
}

// Float64 returns a float64 from the reader.
func (b *Reader) Float64() float64 {
	return math.Float64frombits(b.readUint64())
}

func (b *Reader) readUint64() uint64 {
	if len(b.Src) < 8 {
		b.bad = true
		b.Src = nil
		return 0
	}
	r := binary.BigEndian.Uint64(b.Src)
	b.Src = b.Src[8:]
	return r
}

// Uint32 returns a uint32 from the reader.
func (b *Reader) Uint32() uint32 {
	if len(b.Src) < 4 {
		b.bad = true
		b.Src = nil
		return 0
	}
	r := binary.BigEndian.Uint32(b.Src)
	b.Src = b.Src[4:]
	return r
}

// Varint returns a varint int32 from the reader.
func (b *Reader) Varint() int32 {
	val, n := Varint(b.Src)
	if n <= 0 {
		b.bad = true
		b.Src = nil
		return 0
	}
	b.Src = b.Src[n:]
	return val
}

// Varlong returns a varlong int64 from the reader.
func (b *Reader) Varlong() int64 {
	val, n := Varlong(b.Src)
	if n <= 0 {
		b.bad = true
		b.Src = nil
		return 0
	}
	b.Src = b.Src[n:]
	return val
}

// Uvarint returns a uvarint encoded uint32 from the reader.
func (b *Reader) Uvarint() uint32 {
	val, n := Uvarint(b.Src)
	if n <= 0 {
		b.bad = true
		b.Src = nil
		return 0
	}
	b.Src = b.Src[n:]
	return val
}

// Span returns l bytes from the reader.
func (b *Reader) Span(l int) []byte {
	if len(b.Src) < l || l < 0 {
		b.bad = true
		b.Src = nil
		return nil
	}
	r := b.Src[:l:l]
	b.Src = b.Src[l:]
	return r
}

// UnsafeString returns a Kafka string from the reader without allocating using
// the unsafe package. This must be used with care; note the string holds a
// reference to the original slice.
func (b *Reader) UnsafeString() string {
	l := b.Int16()
	return UnsafeString(b.Span(int(l)))
}

// String returns a Kafka string from the reader.
func (b *Reader) String() string {
	l := b.Int16()
	return string(b.Span(int(l)))
}

// UnsafeCompactString returns a Kafka compact string from the reader without
// allocating using the unsafe package. This must be used with care; note the
// string holds a reference to the original slice.
func (b *Reader) UnsafeCompactString() string {
	l := int(b.Uvarint()) - 1
	return UnsafeString(b.Span(l))
}

// CompactString returns a Kafka compact string from the reader.
func (b *Reader) CompactString() string {
	l := int(b.Uvarint()) - 1
	return string(b.Span(l))
}

// UnsafeNullableString returns a Kafka nullable string from the reader without
// allocating using the unsafe package. This must be used with care; note the
// string holds a reference to the original slice.
func (b *Reader) UnsafeNullableString() *string {
	l := b.Int16()
	if l < 0 {
		return nil
	}
	s := UnsafeString(b.Span(int(l)))
	return &s
}

// NullableString returns a Kafka nullable string from the reader.
func (b *Reader) NullableString() *string {
	l := b.Int16()
	if l < 0 {
		return nil
	}
	s := string(b.Span(int(l)))
	return &s
}

// UnsafeCompactNullableString returns a Kafka compact nullable string from the
// reader without allocating using the unsafe package. This must be used with
// care; note the string holds a reference to the original slice.
func (b *Reader) UnsafeCompactNullableString() *string {
	l := int(b.Uvarint()) - 1
	if l < 0 {
		return nil
	}
	s := UnsafeString(b.Span(l))
	return &s
}

// CompactNullableString returns a Kafka compact nullable string from the
// reader.
func (b *Reader) CompactNullableString() *string {
	l := int(b.Uvarint()) - 1
	if l < 0 {
		return nil
	}
	s := string(b.Span(l))
	return &s
}

// Bytes returns a Kafka byte array from the reader.
//
// This never returns nil.
func (b *Reader) Bytes() []byte {
	l := b.Int32()
	// This is not to spec, but it is not clearly documented and Microsoft
	// EventHubs fails here. -1 means null, which should throw an
	// exception. EventHubs uses -1 to mean "does not exist" on some
	// non-nullable fields.
	//
	// Until EventHubs is fixed, we return an empty byte slice for null.
	if l == -1 {
		return []byte{}
	}
	return b.Span(int(l))
}

// CompactBytes returns a Kafka compact byte array from the reader.
//
// This never returns nil.
func (b *Reader) CompactBytes() []byte {
	l := int(b.Uvarint()) - 1
	if l == -1 { // same as above: -1 should not be allowed here
		return []byte{}
	}
	return b.Span(l)
}

// NullableBytes returns a Kafka nullable byte array from the reader, returning
// nil as appropriate.
func (b *Reader) NullableBytes() []byte {
	l := b.Int32()
	if l < 0 {
		return nil
	}
	r := b.Span(int(l))
	return r
}

// CompactNullableBytes returns a Kafka compact nullable byte array from the
// reader, returning nil as appropriate.
func (b *Reader) CompactNullableBytes() []byte {
	l := int(b.Uvarint()) - 1
	if l < 0 {
		return nil
	}
	r := b.Span(l)
	return r
}

// ArrayLen returns a Kafka array length from the reader.
func (b *Reader) ArrayLen() int32 {
	r := b.Int32()
	// The min size of a Kafka type is a byte, so if we do not have
	// at least the array length of bytes left, it is bad.
	if len(b.Src) < int(r) {
		b.bad = true
		b.Src = nil
		return 0
	}
	return r
}

// VarintArrayLen returns a Kafka array length from the reader.
func (b *Reader) VarintArrayLen() int32 {
	r := b.Varint()
	// The min size of a Kafka type is a byte, so if we do not have
	// at least the array length of bytes left, it is bad.
	if len(b.Src) < int(r) {
		b.bad = true
		b.Src = nil
		return 0
	}
	return r
}

// CompactArrayLen returns a Kafka compact array length from the reader.
func (b *Reader) CompactArrayLen() int32 {
	r := int32(b.Uvarint()) - 1
	// The min size of a Kafka type is a byte, so if we do not have
	// at least the array length of bytes left, it is bad.
	if len(b.Src) < int(r) {
		b.bad = true
		b.Src = nil
		return 0
	}
	return r
}

// VarintBytes returns a Kafka encoded varint array from the reader, returning
// nil as appropriate.
func (b *Reader) VarintBytes() []byte {
	l := b.Varint()
	if l < 0 {
		return nil
	}
	return b.Span(int(l))
}

// UnsafeVarintString returns a Kafka encoded varint string from the reader
// without allocating using the unsafe package. This must be used with care;
// note the string holds a reference to the original slice.
func (b *Reader) UnsafeVarintString() string {
	return UnsafeString(b.VarintBytes())
}

// VarintString returns a Kafka encoded varint string from the reader.
func (b *Reader) VarintString() string {
	return string(b.VarintBytes())
}

// Complete returns ErrNotEnoughData if the source ran out while decoding.
func (b *Reader) Complete() error {
	if b.bad {
		return ErrNotEnoughData
	}
	return nil
}

// Ok returns true if the reader is still ok.
func (b *Reader) Ok() bool {
	return !b.bad
}

// UnsafeString returns the slice as a string using unsafe rule (6).
func UnsafeString(slice []byte) string {
	var str string
	strhdr := (*reflect.StringHeader)(unsafe.Pointer(&str))             //nolint:gosec // known way to convert slice to string
	strhdr.Data = ((*reflect.SliceHeader)(unsafe.Pointer(&slice))).Data //nolint:gosec // known way to convert slice to string
	strhdr.Len = len(slice)
	return str
}
