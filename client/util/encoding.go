package util

import (
	"encoding/binary"
	"hash/crc32"
)

// Encbuf is a simple encoding buffer for binary data.
// This is a minimal version that avoids Prometheus tsdb dependencies.
type Encbuf struct {
	B []byte
}

// PutUvarint encodes a uint64 using variable-length encoding.
func (e *Encbuf) PutUvarint(x uint64) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], x)
	e.B = append(e.B, buf[:n]...)
}

// PutString appends a string to the buffer.
func (e *Encbuf) PutString(s string) {
	e.B = append(e.B, s...)
}

// PutByte appends a byte to the buffer.
func (e *Encbuf) PutByte(b byte) {
	e.B = append(e.B, b)
}

// PutBytes appends bytes to the buffer.
func (e *Encbuf) PutBytes(b []byte) {
	e.B = append(e.B, b...)
}

// Decbuf is a simple decoding buffer for binary data.
type Decbuf struct {
	B []byte
	E error
}

// Uvarint decodes a uint64 using variable-length encoding.
func (d *Decbuf) Uvarint() uint64 {
	if d.E != nil {
		return 0
	}
	x, n := binary.Uvarint(d.B)
	if n <= 0 {
		d.E = ErrInvalidSize
		return 0
	}
	d.B = d.B[n:]
	return x
}

// Bytes reads n bytes from the buffer.
func (d *Decbuf) Bytes(n int) []byte {
	if d.E != nil {
		return nil
	}
	if len(d.B) < n {
		d.E = ErrInvalidSize
		return nil
	}
	x := d.B[:n]
	d.B = d.B[n:]
	return x
}

// String reads a string of length n from the buffer.
func (d *Decbuf) String(n int) string {
	b := d.Bytes(n)
	if b == nil {
		return ""
	}
	return string(b)
}

// CheckCrc verifies the CRC32 checksum at the end of the buffer.
func (d *Decbuf) CheckCrc(table *crc32.Table) error {
	if d.E != nil {
		return d.E
	}
	if len(d.B) < 4 {
		d.E = ErrInvalidSize
		return d.E
	}

	offset := len(d.B) - 4
	expCRC := binary.BigEndian.Uint32(d.B[offset:])
	d.B = d.B[:offset]

	if d.Crc32(table) != expCRC {
		d.E = ErrInvalidChecksum
		return d.E
	}
	return nil
}

// Crc32 calculates the CRC32 checksum of the remaining buffer.
func (d *Decbuf) Crc32(table *crc32.Table) uint32 {
	return crc32.Checksum(d.B, table)
}

// Error returns the decoding error, if any.
func (d *Decbuf) Error() error {
	return d.E
}

var (
	// ErrInvalidSize is returned when the buffer is too small.
	ErrInvalidSize = &DecodeError{Msg: "invalid size"}
	// ErrInvalidChecksum is returned when the CRC32 checksum doesn't match.
	ErrInvalidChecksum = &DecodeError{Msg: "invalid checksum"}
)

// DecodeError represents a decoding error.
type DecodeError struct {
	Msg string
}

func (e *DecodeError) Error() string {
	return e.Msg
}
