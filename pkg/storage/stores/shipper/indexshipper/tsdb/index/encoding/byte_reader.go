// SPDX-License-Identifier: AGPL-3.0-only

package encoding

// ByteSliceReader implements BufReader over an in-memory byte slice.
// It is used during the migration away from mmap-based reading, allowing
// existing ByteSlice-backed code to use the streaming DecbufFactory API.
type ByteSliceReader struct {
	b   []byte
	off int
}

func NewByteSliceReader(b []byte) *ByteSliceReader {
	return &ByteSliceReader{b: b}
}

func (r *ByteSliceReader) Reset() error {
	r.off = 0
	return nil
}

func (r *ByteSliceReader) ResetAt(off int) error {
	if off > len(r.b) {
		r.off = len(r.b)
		return ErrInvalidSize
	}
	r.off = off
	return nil
}

func (r *ByteSliceReader) Skip(n int) error {
	if r.off+n > len(r.b) {
		r.off = len(r.b)
		return ErrInvalidSize
	}
	r.off += n
	return nil
}

// Peek returns up to n bytes without advancing the position. Unlike FileReader.Peek,
// this never errors: if fewer than n bytes remain it returns what is available.
func (r *ByteSliceReader) Peek(n int) ([]byte, error) {
	end := r.off + n
	if end > len(r.b) {
		end = len(r.b)
	}
	return r.b[r.off:end], nil
}

func (r *ByteSliceReader) Read(n int) ([]byte, error) {
	if r.off+n > len(r.b) {
		r.off = len(r.b)
		return nil, ErrInvalidSize
	}
	b := r.b[r.off : r.off+n]
	r.off += n
	return b, nil
}

func (r *ByteSliceReader) ReadInto(b []byte) error {
	n := len(b)
	if r.off+n > len(r.b) {
		r.off = len(r.b)
		return ErrInvalidSize
	}
	copy(b, r.b[r.off:])
	r.off += n
	return nil
}

func (r *ByteSliceReader) Buffered() int { return r.Len() }
func (r *ByteSliceReader) Size() int     { return len(r.b) }
func (r *ByteSliceReader) Len() int      { return len(r.b) - r.off }
func (r *ByteSliceReader) Offset() int   { return r.off }
func (r *ByteSliceReader) Close() error  { return nil }
