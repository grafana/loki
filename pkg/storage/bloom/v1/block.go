package v1

import (
	"bytes"
	"io"

	"github.com/pkg/errors"
)

type BlockReader interface {
	Index() io.ReadSeeker
	// Blooms() io.ReedSeeker
}

type Block struct {
	// schema, series index
	index BlockIndex
	// synthetic header for the entire block
	// built from all the pages in the index
	header SeriesHeader

	reader BlockReader // should this be decoupled from the struct (accepted as method arg instead)?

}

func NewBlock(reader BlockReader) *Block {
	return &Block{
		reader: reader,
	}
}

func (b *Block) LoadIndex() ([]byte, error) {
	data, err := io.ReadAll(b.reader.Index())
	if err != nil {
		return nil, errors.Wrap(err, "reading index")
	}
	return data, nil
}

func (b *Block) LoadHeaders() error {
	data, err := b.LoadIndex()
	if err != nil {
		return err
	}

	if err := b.index.Decode(data); err != nil {
		return errors.Wrap(err, "decoding index")
	}

	return nil
}

func (b *Block) Series() *LazySeriesIter {
	return NewLazySeriesIter(b)
}

type ByteReader struct {
	data []byte
}

func NewByteReader(b []byte) *ByteReader {
	return &ByteReader{data: b}
}

func (r *ByteReader) Index() io.ReadSeeker {
	return bytes.NewReader(r.data)
}
