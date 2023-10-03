package v1

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
)

const (
	bloomFileName  = "bloom"
	seriesFileName = "series"
)

type BlockWriter interface {
	Index() (io.WriteCloser, error)
	Blooms() (io.WriteCloser, error)
	Size() (int, error) // byte size of accumualted index & blooms
}

type BlockReader interface {
	Index() (io.ReadSeeker, error)
	Blooms() (io.ReadSeeker, error)
}

// in memory impl
type MemoryBlockWriter struct {
	index, blooms *bytes.Buffer
}

func NewMemoryBlockWriter(index, blooms *bytes.Buffer) MemoryBlockWriter {
	return MemoryBlockWriter{
		index:  index,
		blooms: blooms,
	}
}

func (b MemoryBlockWriter) Index() (io.WriteCloser, error) {
	return NewNoopCloser(b.index), nil
}
func (b MemoryBlockWriter) Blooms() (io.WriteCloser, error) {
	return NewNoopCloser(b.blooms), nil
}

func (b MemoryBlockWriter) Size() (int, error) {
	return b.index.Len() + b.blooms.Len(), nil
}

// Directory based impl
type DirectoryBlockWriter struct {
	dir           string
	blooms, index *os.File

	initialized bool
}

func NewDirectoryBlockWriter(dir string) *DirectoryBlockWriter {
	return &DirectoryBlockWriter{
		dir: dir,
	}
}

func (b *DirectoryBlockWriter) Init() error {
	if !b.initialized {
		err := util.EnsureDirectory(b.dir)
		if err != nil {
			return errors.Wrap(err, "creating bloom block dir")
		}

		b.index, err = os.Create(filepath.Join(b.dir, seriesFileName))
		if err != nil {
			return errors.Wrap(err, "creating series file")
		}

		b.blooms, err = os.Create(filepath.Join(b.dir, bloomFileName))
		if err != nil {
			return errors.Wrap(err, "creating bloom file")
		}

		b.initialized = true
	}
	return nil
}

func (b *DirectoryBlockWriter) Index() (io.WriteCloser, error) {
	if !b.initialized {
		if err := b.Init(); err != nil {
			return nil, err
		}
	}
	return b.index, nil
}

func (b *DirectoryBlockWriter) Blooms() (io.WriteCloser, error) {
	if !b.initialized {
		if err := b.Init(); err != nil {
			return nil, err
		}
	}
	return b.blooms, nil
}

func (b *DirectoryBlockWriter) Size() (int, error) {
	var size int
	for _, f := range []*os.File{b.blooms, b.index} {
		info, err := f.Stat()
		if err != nil {
			return 0, errors.Wrapf(err, "error stat'ing file %s", f.Name())
		}

		size += int(info.Size())
	}
	return size, nil
}

// In memory reader
type ByteReader struct {
	index, blooms *bytes.Buffer
}

func NewByteReader(index, blooms *bytes.Buffer) *ByteReader {
	return &ByteReader{index: index, blooms: blooms}
}

func (r *ByteReader) Index() (io.ReadSeeker, error) {
	return bytes.NewReader(r.index.Bytes()), nil
}

func (r *ByteReader) Blooms() (io.ReadSeeker, error) {
	return bytes.NewReader(r.blooms.Bytes()), nil
}

// File reader
type DirectoryBlockReader struct {
	dir           string
	blooms, index *os.File

	initialized bool
}

func NewDirectoryBlockReader(dir string) *DirectoryBlockReader {
	return &DirectoryBlockReader{
		dir:         dir,
		initialized: false,
	}
}

func (r *DirectoryBlockReader) Init() error {
	if !r.initialized {
		var err error
		r.index, err = os.Open(filepath.Join(r.dir, seriesFileName))
		if err != nil {
			return errors.Wrap(err, "opening series file")
		}

		r.blooms, err = os.Open(filepath.Join(r.dir, bloomFileName))
		if err != nil {
			return errors.Wrap(err, "opening bloom file")
		}

		r.initialized = true
	}
	return nil
}

func (r *DirectoryBlockReader) Index() (io.ReadSeeker, error) {
	if !r.initialized {
		if err := r.Init(); err != nil {
			return nil, err
		}
	}
	return r.index, nil
}

func (r *DirectoryBlockReader) Blooms() (io.ReadSeeker, error) {
	if !r.initialized {
		if err := r.Init(); err != nil {
			return nil, err
		}
	}
	return r.blooms, nil
}
