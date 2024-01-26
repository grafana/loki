// This file contains modifications from the original source code found in: https://github.com/jhump/protoreflect

package codec

import (
	"fmt"
	"io"
)

// Buffer is a reader and a writer that wraps a slice of bytes and also
// provides API for decoding and encoding the protobuf binary format.
//
// Its operation is similar to that of a bytes.Buffer: writing pushes
// data to the end of the buffer while reading pops data from the head
// of the buffer. So the same buffer can be used to both read and write.
type Buffer struct {
	buf   []byte
	index int
	len   int
}

// NewBuffer creates a new buffer with the given slice of bytes as the
// buffer's initial contents.
func NewBuffer(buf []byte) *Buffer {
	return &Buffer{buf: buf, index: 0, len: len(buf)}
}

// Reset resets this buffer back to empty. Any subsequent writes/encodes
// to the buffer will allocate a new backing slice of bytes.
func (cb *Buffer) Reset(buf []byte) {
	cb.buf = buf
	cb.index = 0
	cb.len = len(buf)
}

// Bytes returns the slice of bytes remaining in the buffer. Note that
// this does not perform a copy: if the contents of the returned slice
// are modified, the modifications will be visible to subsequent reads
// via the buffer.
func (cb *Buffer) Bytes() []byte {
	return cb.buf[cb.index:]
}

// EOF returns true if there are no more bytes remaining to read.
func (cb *Buffer) EOF() bool {
	return cb.index >= cb.len
}

// Skip attempts to skip the given number of bytes in the input. If
// the input has fewer bytes than the given count, false is returned
// and the buffer is unchanged. Otherwise, the given number of bytes
// are skipped and true is returned.
func (cb *Buffer) Skip(count int) error {
	if count < 0 {
		return fmt.Errorf("proto: bad byte length %d", count)
	}
	newIndex := cb.index + count
	if newIndex < cb.index || newIndex > cb.len {
		return io.ErrUnexpectedEOF
	}
	cb.index = newIndex
	return nil
}

// Len returns the remaining number of bytes in the buffer.
func (cb *Buffer) Len() int {
	return cb.len - cb.index
}

// Read implements the io.Reader interface. If there are no bytes
// remaining in the buffer, it will return 0, io.EOF. Otherwise,
// it reads max(len(dest), cb.Len()) bytes from input and copies
// them into dest. It returns the number of bytes copied and a nil
// error in this case.
func (cb *Buffer) Read(dest []byte) (int, error) {
	if cb.index == cb.len {
		return 0, io.EOF
	}
	copied := copy(dest, cb.buf[cb.index:])
	cb.index += copied
	return copied, nil
}

var _ io.Reader = (*Buffer)(nil)
