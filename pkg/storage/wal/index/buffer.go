package index

import (
	"bytes"
	"fmt"
	"io"

	"github.com/pkg/errors"
)

type BufferWriter struct {
	buf *bytes.Buffer
	pos uint64
}

// NewBufferWriter returns a new BufferWriter.
// todo: pooling memory
func NewBufferWriter() *BufferWriter {
	return &BufferWriter{
		buf: bytes.NewBuffer(nil),
		pos: 0,
	}
}

func (fw *BufferWriter) Pos() uint64 {
	return fw.pos
}

func (fw *BufferWriter) Write(bufs ...[]byte) error {
	for _, buf := range bufs {
		n, err := fw.buf.Write(buf)
		if err != nil {
			return err
		}
		fw.pos += uint64(n)
	}
	return nil
}

func (fw *BufferWriter) Flush() error {
	return nil
}

func (fw *BufferWriter) WriteAt(buf []byte, pos uint64) error {
	if pos > fw.pos {
		return fmt.Errorf("position out of range")
	}
	if pos+uint64(len(buf)) > fw.pos {
		return fmt.Errorf("write exceeds buffer size")
	}
	copy(fw.buf.Bytes()[pos:], buf)
	return nil
}

func (fw *BufferWriter) Read(buf []byte) (int, error) {
	return fw.buf.Read(buf)
}

func (fw *BufferWriter) ReadFrom(r io.Reader) (int64, error) {
	n, err := fw.buf.ReadFrom(r)
	if err != nil {
		return n, err
	}
	fw.pos += uint64(n)
	return n, err
}

func (fw *BufferWriter) AddPadding(size int) error {
	p := fw.pos % uint64(size)
	if p == 0 {
		return nil
	}
	p = uint64(size) - p

	if err := fw.Write(make([]byte, p)); err != nil {
		return errors.Wrap(err, "add padding")
	}
	return nil
}

func (fw *BufferWriter) Buffer() ([]byte, io.Closer, error) {
	return fw.buf.Bytes(), io.NopCloser(nil), nil
}

func (fw *BufferWriter) Close() error {
	return nil
}

func (fw *BufferWriter) Reset() {
	fw.pos = 0
	fw.buf.Reset()
}

func (fw *BufferWriter) Remove() error {
	return nil
}
