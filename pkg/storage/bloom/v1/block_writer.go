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
	FileMode       = 0644
	BloomFileName  = "bloom"
	SeriesFileName = "series"
)

type BlockWriter interface {
	Index() (io.WriteCloser, error)
	Blooms() (io.WriteCloser, error)
	Size() (int, error) // byte size of accumualted index & blooms
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

		b.index, err = os.Create(filepath.Join(b.dir, SeriesFileName))
		if err != nil {
			return errors.Wrap(err, "creating series file")
		}

		b.blooms, err = os.Create(filepath.Join(b.dir, BloomFileName))
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
