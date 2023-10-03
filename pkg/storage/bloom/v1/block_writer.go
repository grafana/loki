package v1

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/pkg/errors"
)

const (
	bloomFileName  = "bloom"
	seriesFileName = "series"
)

type BlockWriter interface {
	Index() io.WriteCloser
	Blooms() io.WriteCloser
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

func (b MemoryBlockWriter) Index() io.WriteCloser {
	return NewNoopCloser(b.index)
}
func (b MemoryBlockWriter) Blooms() io.WriteCloser {
	return NewNoopCloser(b.blooms)
}

func (b MemoryBlockWriter) Size() (int, error) {
	return b.index.Len() + b.blooms.Len(), nil
}

// Directory based impl
type DirectoryBlockBuilder struct {
	dir           string
	blooms, index *os.File

	initialized bool
}

func NewDirectoryBlockBuilder(dir string) *DirectoryBlockBuilder {
	return &DirectoryBlockBuilder{
		dir: dir,
	}
}

func (b *DirectoryBlockBuilder) Init() error {
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

func (b *DirectoryBlockBuilder) Index() io.WriteCloser {
	if !b.initialized {
		b.Init()
	}
	return b.index
}

func (b *DirectoryBlockBuilder) Blooms() io.WriteCloser {
	if !b.initialized {
		b.Init()
	}
	return b.blooms
}

func (b *DirectoryBlockBuilder) Size() (int, error) {
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
