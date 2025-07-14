package index

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"os"

	"github.com/pkg/errors"
)

// interface used in tsdb creation -- originally extracted from
// interacting with temporary files on the file system.
type writer interface {
	io.WriteCloser
	io.ReaderFrom
	// WriteAt overwrites a subset of the writer, but only if it won't overflow the current position.
	// NB: will not change position.
	io.WriterAt
	Remove() error
	Pos() uint64
	WriteBufs(bufs ...[]byte) error
	AddPadding(size int) error
	Flush() error
	// Returns the underlying bytes of the writer and sets the Pos to the end
	Bytes() ([]byte, error)

	// Used at the end to return the built file. Left as a implementable method rather than being
	// done via Bytes() to allow optimizations (e.g. avoid loading whole index into memory when unused)
	Load() (io.ReadCloser, error)
}

type FileWriter struct {
	f        *os.File
	fbuf     *bufio.Writer
	position uint64
	name     string
}

func NewFileWriter(name string) (*FileWriter, error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, err
	}
	return &FileWriter{
		f:        f,
		fbuf:     bufio.NewWriterSize(f, 1<<22),
		position: 0,
		name:     name,
	}, nil
}

func (fw *FileWriter) Pos() uint64 {
	return fw.position
}

func (fw *FileWriter) ReadFrom(r io.Reader) (int64, error) {
	n, err := fw.fbuf.ReadFrom(r)
	fw.position += uint64(n)
	return n, err
}

func (fw *FileWriter) Write(p []byte) (n int, err error) {
	n, err = fw.fbuf.Write(p)
	fw.position += uint64(n)
	if err != nil {
		return n, err
	}

	// For now the index file must not grow beyond 64GiB. Some of the fixed-sized
	// offset references in v1 are only 4 bytes large.
	// Once we move to compressed/varint representations in those areas, this limitation
	// can be lifted.
	if fw.position > 16*math.MaxUint32 {
		return n, errors.Errorf("%q exceeding max size of 64GiB", fw.name)
	}
	return n, nil
}

func (fw *FileWriter) WriteBufs(bufs ...[]byte) error {
	for _, b := range bufs {
		if _, err := fw.Write(b); err != nil {
			return err
		}
	}
	return nil
}

func (fw *FileWriter) Flush() error {
	return fw.fbuf.Flush()
}

func (fw *FileWriter) WriteAt(buf []byte, pos int64) (int, error) {
	if err := fw.Flush(); err != nil {
		return 0, err
	}
	if pos > int64(fw.Pos()) {
		return 0, errors.New("position out of range")
	}
	if pos+int64(len(buf)) > int64(fw.Pos()) {
		return 0, errors.New("write exceeds buffer size")
	}
	return fw.f.WriteAt(buf, pos)
}

// AddPadding adds zero byte padding until the file size is a multiple size.
func (fw *FileWriter) AddPadding(size int) error {
	p := fw.position % uint64(size)
	if p == 0 {
		return nil
	}
	p = uint64(size) - p

	if _, err := fw.Write(make([]byte, p)); err != nil {
		return errors.Wrap(err, "add padding")
	}
	return nil
}

func (fw *FileWriter) Close() error {
	if err := fw.Flush(); err != nil {
		return err
	}
	if err := fw.f.Sync(); err != nil {
		return err
	}
	return fw.f.Close()
}

func (fw *FileWriter) Load() (io.ReadCloser, error) {
	f, err := os.Open(fw.name)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (fw *FileWriter) Remove() error {
	return os.Remove(fw.name)
}

func (fw *FileWriter) Bytes() ([]byte, error) {
	// First, ensure all is flushed
	if err := fw.Flush(); err != nil {
		return nil, err
	}

	if _, err := fw.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	return io.ReadAll(fw.f)
}

type MemWriter struct {
	buf *bytes.Buffer
}

// NewBufferWriter returns a new BufferWriter.
// todo: pooling memory
func NewBufferWriter() *MemWriter {
	return &MemWriter{
		buf: bytes.NewBuffer(nil),
	}
}

func (bw *MemWriter) Write(p []byte) (n int, err error) {
	n, err = bw.buf.Write(p)
	return n, err
}

func (bw *MemWriter) Pos() uint64 {
	return uint64(bw.buf.Len())
}

func (bw *MemWriter) WriteBufs(bufs ...[]byte) error {
	for _, b := range bufs {
		if _, err := bw.Write(b); err != nil {
			return err
		}
	}
	return nil
}

func (bw *MemWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(bw.buf, r)
}

func (bw *MemWriter) WriteAt(buf []byte, pos int64) (int, error) {
	if pos+int64(len(buf)) > int64(bw.buf.Len()) {
		return 0, errors.New("write exceeds buffer size")
	}

	// Get current bytes
	bytes := bw.buf.Bytes()

	// Copy buf into correct position
	copy(bytes[pos:], buf)

	return len(buf), nil
}

// AddPadding adds zero byte padding until the file size is a multiple of size.
func (bw *MemWriter) AddPadding(size int) error {
	if size <= 0 {
		return nil
	}

	p := bw.buf.Len() % size
	if p == 0 {
		return nil
	}

	p = size - p
	padding := make([]byte, p)
	n, err := bw.Write(padding)
	if err != nil {
		return err
	}
	if n != len(padding) {
		return errors.New("failed to write padding")
	}
	return nil
}

func (bw *MemWriter) Bytes() ([]byte, error) {
	return bw.buf.Bytes(), nil
}

func (bw *MemWriter) Close() error { return nil }

func (bw *MemWriter) Flush() error { return nil }

func (bw *MemWriter) Remove() error { return nil }

func (bw *MemWriter) Load() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(bw.buf.Bytes())), nil
}
