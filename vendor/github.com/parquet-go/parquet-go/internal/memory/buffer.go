package memory

import (
	"fmt"
	"io"
)

// Buffer is a buffer that stores bytes in fixed-size chunks and implements
// io.ReadWriteSeeker.
//
// It uses ChunkBuffer[byte] internally for chunk management.
type Buffer struct {
	data ChunkBuffer[byte]
	seek int64 // absolute offset for read/write operations
}

func NewBuffer(chunkSize int) *Buffer {
	return &Buffer{data: ChunkBufferFor[byte](chunkSize)}
}

func (b *Buffer) Reset() {
	b.data.Reset()
	b.seek = 0
}

func (b *Buffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if b.seek >= int64(b.data.Len()) {
		return 0, io.EOF
	}

	chunkSize := b.data.chunkSize
	i := int(b.seek) / chunkSize
	offset := int(b.seek) % chunkSize

	chunk := b.data.Chunk(i)
	n = copy(p, chunk[offset:])
	b.seek += int64(n)

	return n, nil
}

func (b *Buffer) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return 0, nil
	}

	chunkSize := b.data.chunkSize
	for len(p) > 0 {
		i := int(b.seek) / chunkSize
		offset := int(b.seek) % chunkSize

		if i == b.data.NumChunks() {
			// Append dummy byte to allocate chunk, we'll overwrite it
			b.data.Append(0)
		}

		chunk := b.data.Chunk(i)
		chunk = chunk[:cap(chunk)]
		written := copy(chunk[offset:], p)

		newSeek := b.seek + int64(written)
		if int(newSeek) > b.data.length {
			b.data.length = int(newSeek)
		}

		b.seek = newSeek
		p = p[written:]
	}

	return n, nil
}

func (b *Buffer) WriteTo(w io.Writer) (int64, error) {
	var written int64
	var err error
	chunkSize := b.data.chunkSize

	for err == nil && b.seek < int64(b.data.Len()) {
		i := int(b.seek) / chunkSize
		offset := int(b.seek) % chunkSize

		chunk := b.data.Chunk(i)
		n, e := w.Write(chunk[offset:])
		written += int64(n)
		b.seek += int64(n)
		err = e
	}
	return written, err
}

func (b *Buffer) Seek(offset int64, whence int) (int64, error) {
	var position int64

	switch whence {
	case io.SeekStart:
		position = offset
	case io.SeekCurrent:
		position = b.seek + offset
	case io.SeekEnd:
		position = int64(b.data.Len()) + offset
	default:
		return 0, fmt.Errorf("seek: invalid whence: %d", whence)
	}

	if position < 0 {
		return 0, fmt.Errorf("seek: negative offset: %d<0", position)
	}

	end := int64(b.data.Len())
	if position > end {
		position = end
	}

	b.seek = position
	return position, nil
}

var (
	_ io.Writer   = (*Buffer)(nil)
	_ io.Reader   = (*Buffer)(nil)
	_ io.Seeker   = (*Buffer)(nil)
	_ io.WriterTo = (*Buffer)(nil)
)
