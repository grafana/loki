// Package codec implements Kafka's binary wire protocol primitives.
//
// Reader decodes values from a byte slice using a sticky-error pattern:
// after the first decode failure all subsequent reads are no-ops and Err()
// returns the error.  Writer appends values into an in-memory buffer;
// bytes.Buffer.Write never fails, so no error tracking is needed there.
package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// ── Reader ────────────────────────────────────────────────────────────────────

// Reader reads Kafka protocol primitives from a byte slice.
type Reader struct {
	buf []byte
	pos int
	err error
}

// NewReader creates a Reader over the provided byte slice.  The slice is not
// copied; the caller must not modify it while the Reader is in use.
func NewReader(b []byte) *Reader { return &Reader{buf: b} }

// Err returns the first error encountered during reading.
func (r *Reader) Err() error { return r.err }

// Remaining returns the number of unread bytes.
func (r *Reader) Remaining() int {
	if r.pos >= len(r.buf) {
		return 0
	}
	return len(r.buf) - r.pos
}

// need checks that at least n bytes are available, setting r.err on failure.
func (r *Reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.pos+n > len(r.buf) {
		r.err = io.ErrUnexpectedEOF
		return false
	}
	return true
}

// ReadInt8 reads a signed 8-bit integer.
func (r *Reader) ReadInt8() int8 {
	if !r.need(1) {
		return 0
	}
	v := int8(r.buf[r.pos])
	r.pos++
	return v
}

// ReadInt16 reads a big-endian signed 16-bit integer.
func (r *Reader) ReadInt16() int16 {
	if !r.need(2) {
		return 0
	}
	v := int16(binary.BigEndian.Uint16(r.buf[r.pos:]))
	r.pos += 2
	return v
}

// ReadInt32 reads a big-endian signed 32-bit integer.
func (r *Reader) ReadInt32() int32 {
	if !r.need(4) {
		return 0
	}
	v := int32(binary.BigEndian.Uint32(r.buf[r.pos:]))
	r.pos += 4
	return v
}

// ReadInt64 reads a big-endian signed 64-bit integer.
func (r *Reader) ReadInt64() int64 {
	if !r.need(8) {
		return 0
	}
	v := int64(binary.BigEndian.Uint64(r.buf[r.pos:]))
	r.pos += 8
	return v
}

// ReadBool reads a single byte and returns false for 0, true for anything else.
func (r *Reader) ReadBool() bool { return r.ReadInt8() != 0 }

// ReadUVarInt reads an unsigned variable-length integer (unsigned LEB128).
func (r *Reader) ReadUVarInt() uint64 {
	if r.err != nil {
		return 0
	}
	var x uint64
	var s uint
	for {
		if r.pos >= len(r.buf) {
			r.err = io.ErrUnexpectedEOF
			return 0
		}
		b := r.buf[r.pos]
		r.pos++
		if b < 0x80 {
			x |= uint64(b) << s
			return x
		}
		x |= uint64(b&0x7f) << s
		s += 7
		if s >= 64 {
			r.err = fmt.Errorf("codec: uvarint overflow")
			return 0
		}
	}
}

// ReadVarInt reads a zigzag-encoded signed variable-length integer.
func (r *Reader) ReadVarInt() int64 {
	ux := r.ReadUVarInt()
	return int64((ux >> 1) ^ -(ux & 1))
}

// ReadString reads a non-compact nullable string (int16 length prefix).
// Returns "" for null strings (length == -1).
func (r *Reader) ReadString() string {
	n := r.ReadInt16()
	if r.err != nil {
		return ""
	}
	if n < 0 {
		return ""
	}
	if !r.need(int(n)) {
		return ""
	}
	s := string(r.buf[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s
}

// ReadNullableString reads a non-compact nullable string (int16 length prefix).
// Returns nil for null strings (length == -1).
func (r *Reader) ReadNullableString() *string {
	n := r.ReadInt16()
	if r.err != nil {
		return nil
	}
	if n < 0 {
		return nil
	}
	if !r.need(int(n)) {
		return nil
	}
	s := string(r.buf[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return &s
}

// ReadCompactString reads a compact string (uvarint N+1 length prefix).
// N+1 == 0 is the null sentinel; it returns "" for null strings.
func (r *Reader) ReadCompactString() string {
	n := r.ReadUVarInt()
	if r.err != nil {
		return ""
	}
	if n == 0 {
		return "" // null
	}
	n-- // actual byte length
	if !r.need(int(n)) {
		return ""
	}
	s := string(r.buf[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s
}

// ReadCompactNullableString reads a compact nullable string (uvarint N+1 prefix).
// Returns nil when the encoded length is 0 (null).
func (r *Reader) ReadCompactNullableString() *string {
	n := r.ReadUVarInt()
	if r.err != nil {
		return nil
	}
	if n == 0 {
		return nil
	}
	n--
	if !r.need(int(n)) {
		return nil
	}
	s := string(r.buf[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return &s
}

// ReadBytes reads a byte slice with an int32 length prefix.
// Returns nil for null (length == -1).
func (r *Reader) ReadBytes() []byte {
	n := r.ReadInt32()
	if r.err != nil {
		return nil
	}
	if n < 0 {
		return nil
	}
	if !r.need(int(n)) {
		return nil
	}
	b := make([]byte, n)
	copy(b, r.buf[r.pos:r.pos+int(n)])
	r.pos += int(n)
	return b
}

// ReadArrayLen reads an int32 array length.  Returns -1 for null arrays.
func (r *Reader) ReadArrayLen() int32 { return r.ReadInt32() }

// ReadCompactArrayLen reads a compact array length (uvarint N+1).
// Returns -1 for null arrays (uvarint == 0).
func (r *Reader) ReadCompactArrayLen() int32 {
	n := r.ReadUVarInt()
	if r.err != nil {
		return 0
	}
	if n == 0 {
		return -1 // null array
	}
	return int32(n - 1)
}

// ReadTaggedFields reads and discards a tagged-fields section (KIP-482).
// The encoding is: uvarint count, then for each tag:
//
//	uvarint tag | uvarint data-size | data bytes
func (r *Reader) ReadTaggedFields() {
	n := r.ReadUVarInt()
	if r.err != nil {
		return
	}
	for i := uint64(0); i < n; i++ {
		r.ReadUVarInt() // tag
		size := r.ReadUVarInt()
		if r.err != nil {
			return
		}
		if !r.need(int(size)) {
			return
		}
		r.pos += int(size)
	}
}

// ── Writer ────────────────────────────────────────────────────────────────────

// Writer appends Kafka protocol primitives into an in-memory buffer.
type Writer struct {
	buf bytes.Buffer
}

// NewWriter creates a Writer with an empty buffer.
func NewWriter() *Writer { return &Writer{} }

// WriteInt8 appends a signed 8-bit integer.
func (w *Writer) WriteInt8(v int8) { w.buf.WriteByte(byte(v)) }

// WriteInt16 appends a big-endian signed 16-bit integer.
func (w *Writer) WriteInt16(v int16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(v))
	w.buf.Write(b[:])
}

// WriteInt32 appends a big-endian signed 32-bit integer.
func (w *Writer) WriteInt32(v int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	w.buf.Write(b[:])
}

// WriteInt64 appends a big-endian signed 64-bit integer.
func (w *Writer) WriteInt64(v int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	w.buf.Write(b[:])
}

// WriteBool appends 0x00 for false, 0x01 for true.
func (w *Writer) WriteBool(v bool) {
	if v {
		w.WriteInt8(1)
	} else {
		w.WriteInt8(0)
	}
}

// WriteUVarInt appends an unsigned variable-length integer (unsigned LEB128).
func (w *Writer) WriteUVarInt(v uint64) {
	var buf [10]byte
	n := 0
	for v >= 0x80 {
		buf[n] = byte(v) | 0x80
		v >>= 7
		n++
	}
	buf[n] = byte(v)
	w.buf.Write(buf[:n+1])
}

// WriteString appends a non-compact string with an int16 length prefix.
func (w *Writer) WriteString(s string) {
	w.WriteInt16(int16(len(s)))
	w.buf.WriteString(s)
}

// WriteNullableString appends a nullable string with an int16 length prefix.
// A nil pointer appends -1 (null sentinel).
func (w *Writer) WriteNullableString(s *string) {
	if s == nil {
		w.WriteInt16(-1)
		return
	}
	w.WriteString(*s)
}

// WriteCompactString appends a compact string with a uvarint(len+1) prefix.
func (w *Writer) WriteCompactString(s string) {
	w.WriteUVarInt(uint64(len(s)) + 1)
	w.buf.WriteString(s)
}

// WriteCompactNullableString appends a compact nullable string.
// A nil pointer appends uvarint(0) (null sentinel).
func (w *Writer) WriteCompactNullableString(s *string) {
	if s == nil {
		w.WriteUVarInt(0)
		return
	}
	w.WriteCompactString(*s)
}

// WriteBytes appends a byte slice with an int32 length prefix.
// A nil slice appends -1 (null sentinel).
func (w *Writer) WriteBytes(b []byte) {
	if b == nil {
		w.WriteInt32(-1)
		return
	}
	w.WriteInt32(int32(len(b)))
	w.buf.Write(b)
}

// WriteCompactBytes appends a compact byte slice with a uvarint(len+1) prefix.
// A nil slice appends uvarint(0) (null sentinel).
func (w *Writer) WriteCompactBytes(b []byte) {
	if b == nil {
		w.WriteUVarInt(0)
		return
	}
	w.WriteUVarInt(uint64(len(b)) + 1)
	w.buf.Write(b)
}

// WriteArrayLen appends an int32 array length.
func (w *Writer) WriteArrayLen(n int32) { w.WriteInt32(n) }

// WriteCompactArrayLen appends a compact array length as uvarint(n+1).
// Pass -1 to write a null array (uvarint 0).
func (w *Writer) WriteCompactArrayLen(n int32) {
	if n < 0 {
		w.WriteUVarInt(0)
		return
	}
	w.WriteUVarInt(uint64(n) + 1)
}

// WriteTaggedFields appends an empty tagged-fields section (single 0x00 byte).
func (w *Writer) WriteTaggedFields() { w.WriteUVarInt(0) }

// WriteVarInt appends a zigzag-encoded signed 32-bit variable-length integer.
// This is the Record-level varint encoding used inside RecordBatch bodies.
func (w *Writer) WriteVarInt(v int32) {
	// Compute zigzag in int32 space to avoid sign-extension when widening to
	// uint64.  Without the explicit uint32 cast, uint64(v>>31) sign-extends
	// int32(-1) to 0xFFFFFFFFFFFFFFFF instead of 0x00000000FFFFFFFF, which
	// produces a 10-byte encoding for any negative value.
	w.WriteUVarInt(uint64(uint32((v << 1) ^ (v >> 31))))
}

// WriteVarLong appends a zigzag-encoded signed 64-bit variable-length integer.
func (w *Writer) WriteVarLong(v int64) {
	w.WriteUVarInt(uint64((v << 1) ^ (v >> 63)))
}

// WriteVarintBytes appends a byte slice prefixed by its zigzag-varint length.
// A nil slice is encoded as varint(-1) (null).
func (w *Writer) WriteVarintBytes(b []byte) {
	if b == nil {
		w.WriteVarInt(-1)
		return
	}
	w.WriteVarInt(int32(len(b)))
	w.buf.Write(b)
}

// WriteVarintString appends a string prefixed by its zigzag-varint length.
func (w *Writer) WriteVarintString(s string) {
	w.WriteVarInt(int32(len(s)))
	w.buf.WriteString(s)
}

// WriteRaw appends raw bytes directly without any framing.
func (w *Writer) WriteRaw(b []byte) { w.buf.Write(b) }

// Bytes returns the accumulated bytes.  The returned slice is valid until the
// next write or Reset call.
func (w *Writer) Bytes() []byte { return w.buf.Bytes() }

// Len returns the current number of buffered bytes.
func (w *Writer) Len() int { return w.buf.Len() }

// ── Record-level varint read helpers ─────────────────────────────────────────
// These complement ReadVarInt / ReadUVarInt for parsing RecordBatch bodies.

// ReadVarintBytes reads a byte slice with a zigzag-varint length prefix.
// Returns nil when the encoded length is negative (null sentinel).
func (r *Reader) ReadVarintBytes() []byte {
	n := r.ReadVarInt() // ReadVarInt returns int64 (zigzag decoded)
	if r.err != nil {
		return nil
	}
	if n < 0 {
		return nil
	}
	if !r.need(int(n)) {
		return nil
	}
	b := make([]byte, n)
	copy(b, r.buf[r.pos:r.pos+int(n)])
	r.pos += int(n)
	return b
}

// ReadVarintString reads a string with a zigzag-varint length prefix.
func (r *Reader) ReadVarintString() string {
	n := r.ReadVarInt()
	if r.err != nil {
		return ""
	}
	if n <= 0 {
		return ""
	}
	if !r.need(int(n)) {
		return ""
	}
	s := string(r.buf[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s
}

// ReadFull reads exactly n bytes and returns them.
func (r *Reader) ReadFull(n int) []byte {
	if !r.need(n) {
		return nil
	}
	b := make([]byte, n)
	copy(b, r.buf[r.pos:r.pos+n])
	r.pos += n
	return b
}

// Reset clears the buffer so the Writer can be reused.
func (w *Writer) Reset() { w.buf.Reset() }

// ── Reader error management ────────────────────────────────────────────────

// ResetErr clears the Reader's sticky error so that subsequent reads can
// proceed.  Use sparingly — only for optional/informational fields where a
// short frame should not cause the connection to be dropped.
func (r *Reader) ResetErr() { r.err = nil }
