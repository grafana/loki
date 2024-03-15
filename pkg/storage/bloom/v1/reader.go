package v1

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

type BlockReader interface {
	Index() (io.ReadSeeker, error)
	Blooms() (io.ReadSeeker, error)
	TarEntries() (Iterator[TarEntry], error)
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

func (r *ByteReader) TarEntries() (Iterator[TarEntry], error) {
	indexLn := r.index.Len()
	index, err := r.Index()
	if err != nil {
		return nil, err
	}
	bloomLn := r.blooms.Len()
	blooms, err := r.Blooms()
	if err != nil {
		return nil, err
	}
	entries := []TarEntry{
		{
			Name: SeriesFileName,
			Size: int64(indexLn),
			Body: index,
		},
		{
			Name: BloomFileName,
			Size: int64(bloomLn),
			Body: blooms,
		},
	}

	return NewSliceIter[TarEntry](entries), err
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
		r.index, err = os.Open(filepath.Join(r.dir, SeriesFileName))
		if err != nil {
			return errors.Wrap(err, "opening series file")
		}

		r.blooms, err = os.Open(filepath.Join(r.dir, BloomFileName))
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

func (r *DirectoryBlockReader) TarEntries() (Iterator[TarEntry], error) {
	if !r.initialized {
		if err := r.Init(); err != nil {
			return nil, err
		}
	}

	idxInfo, err := r.index.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "error stat'ing series file")
	}

	bloomInfo, err := r.blooms.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "error stat'ing bloom file")
	}

	entries := []TarEntry{
		{
			Name: SeriesFileName,
			Size: idxInfo.Size(),
			Body: r.index,
		},
		{
			Name: BloomFileName,
			Size: bloomInfo.Size(),
			Body: r.blooms,
		},
	}

	return NewSliceIter[TarEntry](entries), nil
}
