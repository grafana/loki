package v1

import (
	"bytes"
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

type BlockReader interface {
	Index() io.ReadSeeker
	Blooms() io.ReadSeeker
}

type Block struct {
	// covers series pages
	index BlockIndex
	// covers bloom pages
	blooms BloomBlock

	// TODO(owen-d): implement
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
	if err := b.index.DecodeHeaders(b.reader.Index()); err != nil {
		return errors.Wrap(err, "decoding index")
	}

	if err := b.blooms.DecodeHeaders(b.reader.Blooms()); err != nil {
		return errors.Wrap(err, "decoding blooms")
	}
	return nil
}

func (b *Block) Series() *LazySeriesIter {
	return NewLazySeriesIter(b)
}

func (b *Block) Blooms() *LazyBloomIter {
	return NewLazyBloomIter(b)
}

type ByteReader struct {
	index  []byte
	blooms []byte
}

func NewByteReader(index, blooms []byte) *ByteReader {
	return &ByteReader{index: index, blooms: blooms}
}

func (r *ByteReader) Index() io.ReadSeeker {
	return bytes.NewReader(r.index)
}

func (r *ByteReader) Blooms() io.ReadSeeker {
	return bytes.NewReader(r.blooms)
}

type BlockQuerier struct {
	series *LazySeriesIter
	blooms *LazyBloomIter

	cur *SeriesWithBloom
}

func NewBlockQuerier(b *Block) *BlockQuerier {
	return &BlockQuerier{
		series: NewLazySeriesIter(b),
		blooms: NewLazyBloomIter(b),
	}
}

func (bq *BlockQuerier) Seek(fp model.Fingerprint) {
	bq.series.Seek(fp)
}

func (bq *BlockQuerier) Next() bool {
	if !bq.series.Next() {
		return false
	}

	series := bq.series.At()

	bq.blooms.Seek(series.Offset)
	if !bq.blooms.Next() {
		return false
	}

	bloom := bq.blooms.At()

	bq.cur = &SeriesWithBloom{
		Series: &series.Series,
		Bloom:  bloom,
	}
	return true

}

func (bq *BlockQuerier) At() *SeriesWithBloom {
	return bq.cur
}

func (bq *BlockQuerier) Err() error {
	if err := bq.series.Err(); err != nil {
		return err
	}

	return bq.blooms.Err()
}
