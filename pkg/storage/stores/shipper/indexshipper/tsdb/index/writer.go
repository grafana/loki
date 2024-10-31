package index

import (
	"bufio"
	"io"
	"math"
	"os"

	"github.com/pkg/errors"
)

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

func (fw *FileWriter) WriteAt(buf []byte, pos uint64) error {
	if err := fw.Flush(); err != nil {
		return err
	}
	_, err := fw.f.WriteAt(buf, int64(pos))
	return err
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

func (fw *FileWriter) Remove() error {
	return os.Remove(fw.name)
}
