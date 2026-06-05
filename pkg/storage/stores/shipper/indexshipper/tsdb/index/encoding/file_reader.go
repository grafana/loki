// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_reader.go

package encoding

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/filepool"
)

// readerBufferSize is the size of the buffer used for reading index-header files. This
// value is arbitrary and will likely change in the future based on profiling results.
const readerBufferSize = 4096

type FileReader struct {
	file   *os.File
	closer filepool.FilePoolCloser
	buf    *bufio.Reader
	base   int
	length int
	off    int
}

var bufferPool = sync.Pool{
	New: func() any {
		return bufio.NewReaderSize(nil, readerBufferSize)
	},
}

// NewFileReader creates a new FileReader for the segment of file beginning at base bytes,
// extending length bytes, and closing the handle with closer.
func NewFileReader(file *os.File, base, length int, closer filepool.FilePoolCloser) (*FileReader, error) {
	f := &FileReader{
		file:   file,
		closer: closer,
		buf:    bufferPool.Get().(*bufio.Reader),
		base:   base,
		length: length,
	}

	err := f.Reset()
	if err != nil {
		return nil, err
	}

	return f, nil
}

func (f *FileReader) Reset() error {
	return f.ResetAt(0)
}

func (f *FileReader) ResetAt(off int) error {
	if off > f.length {
		return ErrInvalidSize
	}

	_, err := f.file.Seek(int64(f.base+off), io.SeekStart)
	if err != nil {
		return err
	}

	f.buf.Reset(f.file)
	f.off = off

	return nil
}

func (f *FileReader) Skip(l int) error {
	if l > f.Len() {
		return ErrInvalidSize
	}

	n, err := f.buf.Discard(l)
	if n > 0 {
		f.off += n
	}

	return err
}

func (f *FileReader) Peek(n int) ([]byte, error) {
	b, err := f.buf.Peek(n)
	// bufio.Reader still returns what it Read when it hits EOF and callers
	// expect to be able to peek past the end of a file.
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if len(b) > 0 {
		return b, nil
	}

	return nil, nil
}

func (f *FileReader) Read(n int) ([]byte, error) {
	b := make([]byte, n)

	err := f.ReadInto(b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (f *FileReader) ReadInto(b []byte) error {
	r, err := io.ReadFull(f.buf, b)
	if r > 0 {
		f.off += r
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w reading %d bytes: %s", ErrInvalidSize, len(b), err)
	} else if err != nil {
		return err
	}

	return nil
}

func (f *FileReader) Offset() int {
	return f.off
}

func (f *FileReader) Len() int {
	return f.length - f.off
}

func (f *FileReader) Size() int {
	return f.buf.Size()
}

func (f *FileReader) Buffered() int {
	return f.buf.Buffered()
}

// Close cleans up the underlying resources used by this FileReader.
func (f *FileReader) Close() error {
	// Note that we don't do anything to clean up the buffer before returning it to the pool here:
	// we reset the buffer when we retrieve it from the pool instead.
	bufferPool.Put(f.buf)
	// File handles are pooled, so we don't actually close the handle here, just return it.
	return f.closer.Put(f.file)
}
