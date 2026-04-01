package thrift

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/parquet-go/bitpack/unsafecast"
)

// BinaryProtocol is a Protocol implementation for the binary thrift protocol.
//
// https://github.com/apache/thrift/blob/master/doc/specs/thrift-binary-protocol.md
type BinaryProtocol struct {
	NonStrict bool
}

func (p *BinaryProtocol) NewReaderFromBytes(b []byte) Reader {
	return &binaryBytesReader{p: p, data: b}
}

func (p *BinaryProtocol) NewReader(r io.Reader) Reader {
	return &binaryReader{p: p, r: r}
}

func (p *BinaryProtocol) NewWriter(w io.Writer) Writer {
	return &binaryWriter{p: p, w: w}
}

func (p *BinaryProtocol) Features() Features {
	return 0
}

type binaryReader struct {
	p *BinaryProtocol
	r io.Reader
	b [8]byte
	n int
}

func (r *binaryReader) Protocol() Protocol {
	return r.p
}

func (r *binaryReader) Reader() io.Reader {
	return r.r
}

func (r *binaryReader) ReadBool() (bool, error) {
	v, err := r.ReadByte()
	// Thrift protocol treats both 0 and 2 as false.
	return v != 0 && v != 2, err
}

func (r *binaryReader) ReadInt8() (int8, error) {
	b, err := r.ReadByte()
	return int8(b), err
}

func (r *binaryReader) ReadInt16() (int16, error) {
	b, err := r.read(2)
	if len(b) < 2 {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(b)), nil
}

func (r *binaryReader) ReadInt32() (int32, error) {
	b, err := r.read(4)
	if len(b) < 4 {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(b)), nil
}

func (r *binaryReader) ReadInt64() (int64, error) {
	b, err := r.read(8)
	if len(b) < 8 {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}

func (r *binaryReader) ReadFloat64() (float64, error) {
	b, err := r.read(8)
	if len(b) < 8 {
		return 0, err
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
}

func (r *binaryReader) ReadBytes() ([]byte, error) {
	n, err := r.ReadLength()
	if err != nil {
		return nil, err
	}
	b := make([]byte, n)
	_, err = io.ReadFull(r.r, b)
	if err == nil {
		r.n += n
	}
	return b, err
}

func (r *binaryReader) ReadString() (string, error) {
	b, err := r.ReadBytes()
	return unsafecast.String(b), err
}

func (r *binaryReader) ReadLength() (int, error) {
	b, err := r.read(4)
	if len(b) < 4 {
		return 0, err
	}
	n := binary.BigEndian.Uint32(b)
	if n > math.MaxInt32 {
		return 0, fmt.Errorf("length out of range: %d", n)
	}
	return int(n), nil
}

func (r *binaryReader) ReadMessage() (Message, error) {
	m := Message{}

	b, err := r.read(4)
	if len(b) < 4 {
		return m, err
	}

	if (b[0] >> 7) == 0 { // non-strict
		n := int(binary.BigEndian.Uint32(b))
		s := make([]byte, n)
		_, err := io.ReadFull(r.r, s)
		if err != nil {
			return m, dontExpectEOF(err)
		}
		r.n += n
		m.Name = unsafecast.String(s)

		t, err := r.ReadInt8()
		if err != nil {
			return m, dontExpectEOF(err)
		}

		m.Type = MessageType(t & 0x7)
	} else {
		m.Type = MessageType(b[3] & 0x7)

		if m.Name, err = r.ReadString(); err != nil {
			return m, dontExpectEOF(err)
		}
	}

	m.SeqID, err = r.ReadInt32()
	return m, err
}

func (r *binaryReader) ReadField() (Field, error) {
	t, err := r.ReadInt8()
	if err != nil {
		return Field{}, err
	}
	i, err := r.ReadInt16()
	if err != nil {
		return Field{}, err
	}
	return Field{ID: i, Type: Type(t)}, nil
}

func (r *binaryReader) ReadList() (List, error) {
	t, err := r.ReadInt8()
	if err != nil {
		return List{}, err
	}
	n, err := r.ReadInt32()
	if err != nil {
		return List{}, dontExpectEOF(err)
	}
	return List{Size: n, Type: Type(t)}, nil
}

func (r *binaryReader) ReadSet() (Set, error) {
	l, err := r.ReadList()
	return Set(l), err
}

func (r *binaryReader) ReadMap() (Map, error) {
	k, err := r.ReadByte()
	if err != nil {
		return Map{}, err
	}
	v, err := r.ReadByte()
	if err != nil {
		return Map{}, dontExpectEOF(err)
	}
	n, err := r.ReadInt32()
	if err != nil {
		return Map{}, dontExpectEOF(err)
	}
	return Map{Size: n, Key: Type(k), Value: Type(v)}, nil
}

func (r *binaryReader) ReadByte() (byte, error) {
	var b byte
	var err error
	switch x := r.r.(type) {
	case *bytes.Buffer:
		b, err = x.ReadByte()
	case *bytes.Reader:
		b, err = x.ReadByte()
	case *bufio.Reader:
		b, err = x.ReadByte()
	case io.ByteReader:
		b, err = x.ReadByte()
	default:
		buf, err := r.read(1)
		if err != nil {
			return 0, err
		}
		return buf[0], nil
	}
	if err == nil {
		r.n++
	}
	return b, err
}

func (r *binaryReader) read(n int) ([]byte, error) {
	_, err := io.ReadFull(r.r, r.b[:n])
	if err == nil {
		r.n += n
	}
	return r.b[:n], err
}

func (r *binaryReader) BytesRead() int {
	return r.n
}

type binaryWriter struct {
	p *BinaryProtocol
	b [8]byte
	w io.Writer
}

func (w *binaryWriter) Protocol() Protocol {
	return w.p
}

func (w *binaryWriter) Writer() io.Writer {
	return w.w
}

func (w *binaryWriter) WriteBool(v bool) error {
	var b byte
	if v {
		b = 1
	}
	return w.writeByte(b)
}

func (w *binaryWriter) WriteInt8(v int8) error {
	return w.writeByte(byte(v))
}

func (w *binaryWriter) WriteInt16(v int16) error {
	binary.BigEndian.PutUint16(w.b[:2], uint16(v))
	return w.write(w.b[:2])
}

func (w *binaryWriter) WriteInt32(v int32) error {
	binary.BigEndian.PutUint32(w.b[:4], uint32(v))
	return w.write(w.b[:4])
}

func (w *binaryWriter) WriteInt64(v int64) error {
	binary.BigEndian.PutUint64(w.b[:8], uint64(v))
	return w.write(w.b[:8])
}

func (w *binaryWriter) WriteFloat64(v float64) error {
	binary.BigEndian.PutUint64(w.b[:8], math.Float64bits(v))
	return w.write(w.b[:8])
}

func (w *binaryWriter) WriteBytes(v []byte) error {
	if err := w.WriteLength(len(v)); err != nil {
		return err
	}
	return w.write(v)
}

func (w *binaryWriter) WriteString(v string) error {
	if err := w.WriteLength(len(v)); err != nil {
		return err
	}
	return w.writeString(v)
}

func (w *binaryWriter) WriteLength(n int) error {
	if n < 0 {
		return fmt.Errorf("negative length cannot be encoded in thrift: %d", n)
	}
	if n > math.MaxInt32 {
		return fmt.Errorf("length is too large to be encoded in thrift: %d", n)
	}
	return w.WriteInt32(int32(n))
}

func (w *binaryWriter) WriteMessage(m Message) error {
	if w.p.NonStrict {
		if err := w.WriteString(m.Name); err != nil {
			return err
		}
		if err := w.writeByte(byte(m.Type)); err != nil {
			return err
		}
	} else {
		w.b[0] = 1 << 7
		w.b[1] = 0
		w.b[2] = 0
		w.b[3] = byte(m.Type) & 0x7
		binary.BigEndian.PutUint32(w.b[4:], uint32(len(m.Name)))

		if err := w.write(w.b[:8]); err != nil {
			return err
		}
		if err := w.writeString(m.Name); err != nil {
			return err
		}
	}
	return w.WriteInt32(m.SeqID)
}

func (w *binaryWriter) WriteField(f Field) error {
	if err := w.writeByte(byte(f.Type)); err != nil {
		return err
	}
	return w.WriteInt16(f.ID)
}

func (w *binaryWriter) WriteList(l List) error {
	if err := w.writeByte(byte(l.Type)); err != nil {
		return err
	}
	return w.WriteInt32(l.Size)
}

func (w *binaryWriter) WriteSet(s Set) error {
	return w.WriteList(List(s))
}

func (w *binaryWriter) WriteMap(m Map) error {
	if err := w.writeByte(byte(m.Key)); err != nil {
		return err
	}
	if err := w.writeByte(byte(m.Value)); err != nil {
		return err
	}
	return w.WriteInt32(m.Size)
}

func (w *binaryWriter) write(b []byte) error {
	_, err := w.w.Write(b)
	return err
}

func (w *binaryWriter) writeString(s string) error {
	_, err := io.WriteString(w.w, s)
	return err
}

func (w *binaryWriter) writeByte(b byte) error {
	// The special cases are intended to reduce the runtime overheadof testing
	// for the io.ByteWriter interface for common types. Type assertions on a
	// concrete type is just a pointer comparison, instead of requiring a
	// complex lookup in the type metadata.
	switch x := w.w.(type) {
	case *bytes.Buffer:
		return x.WriteByte(b)
	case *bufio.Writer:
		return x.WriteByte(b)
	case io.ByteWriter:
		return x.WriteByte(b)
	default:
		w.b[0] = b
		return w.write(w.b[:1])
	}
}

// binaryBytesReader is a zero-allocation reader that reads directly from a byte slice.
// Strings and byte slices returned by this reader point into the original buffer,
// so the buffer must outlive any usage of the decoded values.
type binaryBytesReader struct {
	p      *BinaryProtocol
	data   []byte
	offset int
}

func (r *binaryBytesReader) Protocol() Protocol {
	return r.p
}

func (r *binaryBytesReader) Reader() io.Reader {
	return bytes.NewReader(r.data[r.offset:])
}

func (r *binaryBytesReader) ReadBool() (bool, error) {
	b, err := r.ReadByte()
	// Thrift protocol treats both 0 and 2 as false.
	return b != 0 && b != 2, err
}

func (r *binaryBytesReader) ReadInt8() (int8, error) {
	b, err := r.ReadByte()
	return int8(b), err
}

func (r *binaryBytesReader) ReadInt16() (int16, error) {
	if r.offset+2 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int16(binary.BigEndian.Uint16(r.data[r.offset:]))
	r.offset += 2
	return v, nil
}

func (r *binaryBytesReader) ReadInt32() (int32, error) {
	if r.offset+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int32(binary.BigEndian.Uint32(r.data[r.offset:]))
	r.offset += 4
	return v, nil
}

func (r *binaryBytesReader) ReadInt64() (int64, error) {
	if r.offset+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int64(binary.BigEndian.Uint64(r.data[r.offset:]))
	r.offset += 8
	return v, nil
}

func (r *binaryBytesReader) ReadFloat64() (float64, error) {
	if r.offset+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := math.Float64frombits(binary.BigEndian.Uint64(r.data[r.offset:]))
	r.offset += 8
	return v, nil
}

func (r *binaryBytesReader) ReadBytes() ([]byte, error) {
	n, err := r.ReadLength()
	if err != nil {
		return nil, err
	}
	if r.offset+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	b := r.data[r.offset : r.offset+n]
	r.offset += n
	return b, nil
}

func (r *binaryBytesReader) ReadString() (string, error) {
	n, err := r.ReadLength()
	if err != nil {
		return "", err
	}
	if r.offset+n > len(r.data) {
		return "", io.ErrUnexpectedEOF
	}
	s := unsafecast.String(r.data[r.offset : r.offset+n])
	r.offset += n
	return s, nil
}

func (r *binaryBytesReader) ReadLength() (int, error) {
	if r.offset+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	n := binary.BigEndian.Uint32(r.data[r.offset:])
	r.offset += 4
	if n > math.MaxInt32 {
		return 0, fmt.Errorf("length out of range: %d", n)
	}
	return int(n), nil
}

func (r *binaryBytesReader) ReadMessage() (Message, error) {
	m := Message{}

	if r.offset+4 > len(r.data) {
		return m, io.ErrUnexpectedEOF
	}
	b := r.data[r.offset : r.offset+4]
	r.offset += 4

	if (b[0] >> 7) == 0 { // non-strict
		n := int(binary.BigEndian.Uint32(b))
		if r.offset+n > len(r.data) {
			return m, io.ErrUnexpectedEOF
		}
		m.Name = unsafecast.String(r.data[r.offset : r.offset+n])
		r.offset += n

		t, err := r.ReadInt8()
		if err != nil {
			return m, dontExpectEOF(err)
		}

		m.Type = MessageType(t & 0x7)
	} else {
		m.Type = MessageType(b[3] & 0x7)

		var err error
		if m.Name, err = r.ReadString(); err != nil {
			return m, dontExpectEOF(err)
		}
	}

	var err error
	m.SeqID, err = r.ReadInt32()
	return m, err
}

func (r *binaryBytesReader) ReadField() (Field, error) {
	t, err := r.ReadInt8()
	if err != nil {
		return Field{}, err
	}
	i, err := r.ReadInt16()
	if err != nil {
		return Field{}, err
	}
	return Field{ID: i, Type: Type(t)}, nil
}

func (r *binaryBytesReader) ReadList() (List, error) {
	t, err := r.ReadInt8()
	if err != nil {
		return List{}, err
	}
	n, err := r.ReadInt32()
	if err != nil {
		return List{}, dontExpectEOF(err)
	}
	return List{Size: n, Type: Type(t)}, nil
}

func (r *binaryBytesReader) ReadSet() (Set, error) {
	l, err := r.ReadList()
	return Set(l), err
}

func (r *binaryBytesReader) ReadMap() (Map, error) {
	k, err := r.ReadByte()
	if err != nil {
		return Map{}, err
	}
	v, err := r.ReadByte()
	if err != nil {
		return Map{}, dontExpectEOF(err)
	}
	n, err := r.ReadInt32()
	if err != nil {
		return Map{}, dontExpectEOF(err)
	}
	return Map{Size: n, Key: Type(k), Value: Type(v)}, nil
}

func (r *binaryBytesReader) ReadByte() (byte, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	b := r.data[r.offset]
	r.offset++
	return b, nil
}

func (r *binaryBytesReader) BytesRead() int {
	return r.offset
}
