package streamio

import (
	"encoding/binary"
	"io"
	"math/bits"
)

// [binary] does not have an implementation to write varints directly
// to a ByteWriter, and requires appending to a buffer. To allow dataobj
// encoders to stream values, we provide equivalent implementations of
// [binary.AppendUvarint] and [binary.AppendVarint] which accept a ByteWriter.

// VarintSize returns the number of bytes needed to encode x.
func VarintSize(x int64) int {
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return UvarintSize(ux)
}

// UvarintSize returns the number of bytes needed to encode x.
func UvarintSize(x uint64) int {
	if x == 0 {
		return 1
	}
	return 1 + (63-bits.LeadingZeros64(x))/7
}

// WriteVarint writes an encoded signed integer to w.
func WriteVarint(w io.ByteWriter, x int64) error {
	// Like [binary.AppendVarint], we use zig-zag encoding so small negative
	// values require fewer bytes.
	//
	// https://protobuf.dev/programming-guides/encoding/#signed-ints
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return WriteUvarint(w, ux)
}

// WriteUvarint writes an encoded unsigned integer to w.
func WriteUvarint(w io.ByteWriter, x uint64) error {
	for x >= 0x80 {
		if err := w.WriteByte(byte(x) | 0x80); err != nil {
			return err
		}
		x >>= 7
	}
	return w.WriteByte(byte(x))
}

// ReadVarint is a convenience wrapper around [binary.ReadVarint].
func ReadVarint(r io.ByteReader) (int64, error) {
	return binary.ReadVarint(r)
}

// ReadUvarint is a convenience wrapper around [binary.ReadUvarint].
func ReadUvarint(r io.ByteReader) (uint64, error) {
	return binary.ReadUvarint(r)
}
