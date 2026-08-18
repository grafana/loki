// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_reader.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package streamenc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

// ReaderBufferSize is the size of the buffer used for reading index-header files. This
// value is arbitrary and will likely change in the future based on profiling results.
const ReaderBufferSize = 4096

// FileReader reads a file through a fixed-size read-ahead window.
// The window is filled with positioned reads (pread, via os.File.ReadAt).
// Every method maintains an invariant:
// - buf[0:n] holds the bytes [off-r, off-r+n) of the underlying file
type FileReader struct {
	file   *os.File
	closer filepool.FilePoolCloser
	buf    []byte  // read-ahead window
	bufRef *[]byte // pointer to buf so putting it back in the pool doesn't allocate a slice header
	r      int     // read cursor within buf
	n      int     // number of valid bytes in buf
	base   int
	length int
	off    int
}

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, ReaderBufferSize)
		return &b
	},
}

// NewFileReader creates a new FileReader for the segment of file beginning at base bytes,
// extending length bytes, and closing the handle with closer.
func NewFileReader(file *os.File, base, length int, closer filepool.FilePoolCloser) (*FileReader, error) {
	bufRef := bufferPool.Get().(*[]byte)
	f := &FileReader{
		file:   file,
		closer: closer,
		bufRef: bufRef,
		buf:    *bufRef,
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

// ResetAt moves the cursor to off, relative to the segment base.
//
// It never reads and never moves the file handle's offset. A target inside the
// read-ahead window is a cursor move, in either direction, since the window
// spans bytes on both sides of the cursor. Anything else drops the window, so
// the next access preads at the right place.
func (f *FileReader) ResetAt(off int) error {
	if off > f.length {
		return ErrInvalidSize
	}

	if windowStart := f.off - f.r; off >= windowStart && off <= windowStart+f.n {
		f.r, f.off = off-windowStart, off
		return nil
	}

	f.dropWindow()
	f.off = off

	return nil
}

// dropWindow marks the read-ahead window empty, so that the next fill preads at
// the current cursor. It must be called by anything that moves off without
// moving r by the same amount.
func (f *FileReader) dropWindow() {
	f.r, f.n = 0, 0
}

// fill ensures at least need bytes are in the window, or that the end of the
// file has been reached, in which case it returns io.EOF. It always asks for as
// much as the window can hold, so a single pread serves many small reads.
func (f *FileReader) fill(need int) error {
	if f.n-f.r >= need {
		return nil
	}

	// Slide what is left to the front to make room for the read-ahead.
	if f.r > 0 {
		f.n = copy(f.buf, f.buf[f.r:f.n])
		f.r = 0
	}

	if f.n < len(f.buf) {
		m, err := f.file.ReadAt(f.buf[f.n:], int64(f.base+f.off+(f.n-f.r)))
		f.n += m
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}

	if f.n-f.r < need {
		return io.EOF
	}

	return nil
}

func (f *FileReader) Skip(l int) error {
	if l > f.Len() {
		return ErrInvalidSize
	}

	if l <= f.n-f.r {
		f.r += l
	} else {
		f.dropWindow()
	}

	f.off += l

	return nil
}

func (f *FileReader) Peek(n int) ([]byte, error) {
	if n > len(f.buf) {
		// Callers are expected to check Size() first and use Read for anything
		// larger; this mirrors bufio.Reader.Peek refusing to peek past its own
		// buffer.
		return nil, fmt.Errorf("%w peeking %d bytes: window holds %d", ErrInvalidSize, n, len(f.buf))
	}

	err := f.fill(n)
	// Still return a partial result when Peeking beyond the end of the file;
	// this mirrors bufio.Reader.Peek.
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	avail := min(f.n-f.r, n)
	if avail > 0 {
		return f.buf[f.r : f.r+avail], nil
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
	read := 0

	for read < len(b) {
		// Serve from the window first, so a run of small reads shares one pread.
		if f.n > f.r {
			c := copy(b[read:], f.buf[f.r:f.n])
			f.r += c
			f.off += c
			read += c
			continue
		}

		// A read at least as large as the window would spend more time being
		// copied than it saves, so it goes straight into the destination.
		if len(b)-read >= len(f.buf) {
			f.dropWindow()
			m, err := f.file.ReadAt(b[read:], int64(f.base+f.off))
			f.off += m
			read += m
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			break
		}

		// Otherwise refill the window and go again
		if err := f.fill(1); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}

	if read < len(b) {
		cause := io.ErrUnexpectedEOF
		if read == 0 {
			cause = io.EOF
		}
		return fmt.Errorf("%w reading %d bytes: %s", ErrInvalidSize, len(b), cause)
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
	return len(f.buf)
}

func (f *FileReader) Buffered() int {
	return f.n - f.r
}

// Close cleans up the underlying resources used by this FileReader.
func (f *FileReader) Close() error {
	// Note that we don't do anything to clean up the buffer's contents before
	// returning it to the pool here: the window is marked empty on acquisition
	// instead, so stale bytes are never readable.
	if f.bufRef != nil {
		bufferPool.Put(f.bufRef)
		f.bufRef, f.buf = nil, nil
	}
	// File handles are pooled, so we don't actually close the handle here, just return it.
	return f.closer.Put(f.file)
}
