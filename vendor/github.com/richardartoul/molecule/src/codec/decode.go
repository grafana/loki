// This file contains modifications from the original source code found in: https://github.com/jhump/protoreflect

package codec

import (
	"errors"
	"fmt"
	"io"
)

// ErrOverflow is returned when an integer is too large to be represented.
var ErrOverflow = errors.New("proto: integer overflow")

// ErrBadWireType is returned when decoding a wire-type from a buffer that
// is not valid.
var ErrBadWireType = errors.New("proto: bad wiretype")

var varintTypes = map[FieldType]bool{}
var fixed32Types = map[FieldType]bool{}
var fixed64Types = map[FieldType]bool{}

func init() {
	varintTypes[FieldType_BOOL] = true
	varintTypes[FieldType_INT32] = true
	varintTypes[FieldType_INT64] = true
	varintTypes[FieldType_UINT32] = true
	varintTypes[FieldType_UINT64] = true
	varintTypes[FieldType_SINT32] = true
	varintTypes[FieldType_SINT64] = true
	varintTypes[FieldType_ENUM] = true

	fixed32Types[FieldType_FIXED32] = true
	fixed32Types[FieldType_SFIXED32] = true
	fixed32Types[FieldType_FLOAT] = true

	fixed64Types[FieldType_FIXED64] = true
	fixed64Types[FieldType_SFIXED64] = true
	fixed64Types[FieldType_DOUBLE] = true
}

// DecodeVarint reads a varint-encoded integer from the Buffer.
// This is the format for the
// int32, int64, uint32, uint64, bool, and enum
// protocol buffer types.
//
// This implementation is inlined from https://github.com/dennwc/varint to avoid the call-site overhead
func (cb *Buffer) DecodeVarint() (uint64, error) {
	if cb.Len() == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	const (
		step = 7
		bit  = 1 << 7
		mask = bit - 1
	)
	if cb.Len() >= 10 {
		// i == 0
		b := cb.buf[cb.index]
		if b < bit {
			cb.index++
			return uint64(b), nil
		}
		x := uint64(b & mask)
		var s uint = step

		// i == 1
		b = cb.buf[cb.index+1]
		if b < bit {
			cb.index += 2
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 2
		b = cb.buf[cb.index+2]
		if b < bit {
			cb.index += 3
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 3
		b = cb.buf[cb.index+3]
		if b < bit {
			cb.index += 4
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 4
		b = cb.buf[cb.index+4]
		if b < bit {
			cb.index += 5
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 5
		b = cb.buf[cb.index+5]
		if b < bit {
			cb.index += 6
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 6
		b = cb.buf[cb.index+6]
		if b < bit {
			cb.index += 7
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 7
		b = cb.buf[cb.index+7]
		if b < bit {
			cb.index += 8
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 8
		b = cb.buf[cb.index+8]
		if b < bit {
			cb.index += 9
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&mask) << s
		s += step

		// i == 9
		b = cb.buf[cb.index+9]
		if b < bit {
			if b > 1 {
				return 0, ErrOverflow
			}
			cb.index += 10
			return x | uint64(b)<<s, nil
		} else if cb.Len() == 10 {
			return 0, io.ErrUnexpectedEOF
		}
		for _, b := range cb.buf[cb.index+10:] {
			if b < bit {
				return 0, ErrOverflow
			}
		}
		return 0, io.ErrUnexpectedEOF
	}

	// i == 0
	b := cb.buf[cb.index]
	if b < bit {
		cb.index++
		return uint64(b), nil
	} else if cb.Len() == 1 {
		return 0, io.ErrUnexpectedEOF
	}
	x := uint64(b & mask)
	var s uint = step

	// i == 1
	b = cb.buf[cb.index+1]
	if b < bit {
		cb.index += 2
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 2 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 2
	b = cb.buf[cb.index+2]
	if b < bit {
		cb.index += 3
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 3 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 3
	b = cb.buf[cb.index+3]
	if b < bit {
		cb.index += 4
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 4 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 4
	b = cb.buf[cb.index+4]
	if b < bit {
		cb.index += 5
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 5 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 5
	b = cb.buf[cb.index+5]
	if b < bit {
		cb.index += 6
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 6 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 6
	b = cb.buf[cb.index+6]
	if b < bit {
		cb.index += 7
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 7 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 7
	b = cb.buf[cb.index+7]
	if b < bit {
		cb.index += 8
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 8 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 8
	b = cb.buf[cb.index+8]
	if b < bit {
		cb.index += 9
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 9 {
		return 0, io.ErrUnexpectedEOF
	}
	x |= uint64(b&mask) << s
	s += step

	// i == 9
	b = cb.buf[cb.index+9]
	if b < bit {
		if b > 1 {
			return 0, ErrOverflow
		}
		cb.index += 10
		return x | uint64(b)<<s, nil
	} else if cb.Len() == 10 {
		return 0, io.ErrUnexpectedEOF
	}
	for _, b := range cb.buf[cb.index+10:] {
		if b < bit {
			return 0, ErrOverflow
		}
	}
	return 0, io.ErrUnexpectedEOF
}

// DecodeFixed64 reads a 64-bit integer from the Buffer.
// This is the format for the
// fixed64, sfixed64, and double protocol buffer types.
func (cb *Buffer) DecodeFixed64() (x uint64, err error) {
	// x, err already 0
	i := cb.index + 8
	if i < 0 || i > cb.len {
		err = io.ErrUnexpectedEOF
		return
	}
	cb.index = i

	x = uint64(cb.buf[i-8])
	x |= uint64(cb.buf[i-7]) << 8
	x |= uint64(cb.buf[i-6]) << 16
	x |= uint64(cb.buf[i-5]) << 24
	x |= uint64(cb.buf[i-4]) << 32
	x |= uint64(cb.buf[i-3]) << 40
	x |= uint64(cb.buf[i-2]) << 48
	x |= uint64(cb.buf[i-1]) << 56
	return
}

// DecodeFixed32 reads a 32-bit integer from the Buffer.
// This is the format for the
// fixed32, sfixed32, and float protocol buffer types.
func (cb *Buffer) DecodeFixed32() (x uint64, err error) {
	// x, err already 0
	i := cb.index + 4
	if i < 0 || i > cb.len {
		err = io.ErrUnexpectedEOF
		return
	}
	cb.index = i

	x = uint64(cb.buf[i-4])
	x |= uint64(cb.buf[i-3]) << 8
	x |= uint64(cb.buf[i-2]) << 16
	x |= uint64(cb.buf[i-1]) << 24
	return
}

// DecodeZigZag32 decodes a signed 32-bit integer from the given
// zig-zag encoded value.
func DecodeZigZag32(v uint64) int32 {
	return int32((uint32(v) >> 1) ^ uint32((int32(v&1)<<31)>>31))
}

// DecodeZigZag64 decodes a signed 64-bit integer from the given
// zig-zag encoded value.
func DecodeZigZag64(v uint64) int64 {
	return int64((v >> 1) ^ uint64((int64(v&1)<<63)>>63))
}

// DecodeRawBytes reads a count-delimited byte buffer from the Buffer.
// This is the format used for the bytes protocol buffer
// type and for embedded messages.
func (cb *Buffer) DecodeRawBytes(alloc bool) (buf []byte, err error) {
	n, err := cb.DecodeVarint()
	if err != nil {
		return nil, err
	}

	nb := int(n)
	if nb < 0 {
		return nil, fmt.Errorf("proto: bad byte length %d", nb)
	}
	end := cb.index + nb
	if end < cb.index || end > cb.len {
		return nil, io.ErrUnexpectedEOF
	}

	if !alloc {
		// We set a cap on the returned slice equal to the length of the buffer so that it is not possible
		// to read past the end of this slice
		buf = cb.buf[cb.index:end:end]
		cb.index = end
		return
	}

	buf = make([]byte, nb)
	copy(buf, cb.buf[cb.index:])
	cb.index = end
	return
}

// ReadGroup reads the input until a "group end" tag is found
// and returns the data up to that point. Subsequent reads from
// the buffer will read data after the group end tag. If alloc
// is true, the data is copied to a new slice before being returned.
// Otherwise, the returned slice is a view into the buffer's
// underlying byte slice.
//
// This function correctly handles nested groups: if a "group start"
// tag is found, then that group's end tag will be included in the
// returned data.
func (cb *Buffer) ReadGroup(alloc bool) ([]byte, error) {
	var groupEnd, dataEnd int
	groupEnd, dataEnd, err := cb.findGroupEnd()
	if err != nil {
		return nil, err
	}
	var results []byte
	if !alloc {
		results = cb.buf[cb.index:dataEnd]
	} else {
		results = make([]byte, dataEnd-cb.index)
		copy(results, cb.buf[cb.index:])
	}
	cb.index = groupEnd
	return results, nil
}

// SkipGroup is like ReadGroup, except that it discards the
// data and just advances the buffer to point to the input
// right *after* the "group end" tag.
func (cb *Buffer) SkipGroup() error {
	groupEnd, _, err := cb.findGroupEnd()
	if err != nil {
		return err
	}
	cb.index = groupEnd
	return nil
}

func (cb *Buffer) findGroupEnd() (groupEnd int, dataEnd int, err error) {
	bs := cb.buf
	start := cb.index
	defer func() {
		cb.index = start
	}()
	for {
		fieldStart := cb.index
		// read a field tag
		var v uint64
		v, err = cb.DecodeVarint()
		if err != nil {
			return 0, 0, err
		}
		_, wireType, err := AsTagAndWireType(v)
		if err != nil {
			return 0, 0, err
		}
		// skip past the field's data
		switch wireType {
		case WireFixed32:
			if err := cb.Skip(4); err != nil {
				return 0, 0, err
			}
		case WireFixed64:
			if err := cb.Skip(8); err != nil {
				return 0, 0, err
			}
		case WireVarint:
			// skip varint by finding last byte (has high bit unset)
			i := cb.index
			limit := i + 10 // varint cannot be >10 bytes
			for {
				if i >= limit {
					return 0, 0, ErrOverflow
				}
				if i >= len(bs) {
					return 0, 0, io.ErrUnexpectedEOF
				}
				if bs[i]&0x80 == 0 {
					break
				}
				i++
			}
			// TODO: This would only overflow if buffer length was MaxInt and we
			// read the last byte. This is not a real/feasible concern on 64-bit
			// systems. Something to worry about for 32-bit systems? Do we care?
			cb.index = i + 1
		case WireBytes:
			l, err := cb.DecodeVarint()
			if err != nil {
				return 0, 0, err
			}
			if err := cb.Skip(int(l)); err != nil {
				return 0, 0, err
			}
		case WireStartGroup:
			if err := cb.SkipGroup(); err != nil {
				return 0, 0, err
			}
		case WireEndGroup:
			return cb.index, fieldStart, nil
		default:
			return 0, 0, ErrBadWireType
		}
	}
}

// AsTagAndWireType converts the given varint in to a field number and wireType
//
// As of now, this function is inlined.  Please double check that any modifications do not modify the
// inline eligibility
func AsTagAndWireType(v uint64) (tag int32, wireType WireType, err error) {
	// rest is int32 tag number
	// low 7 bits is wire type
	wireType = WireType(v & 7)
	tag = int32(v >> 3)
	if tag <= 0 {
		err = ErrBadWireType // We return a constant error here as this allows the function to be inlined
	}

	return
}
