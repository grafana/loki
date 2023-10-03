package v1

import (
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

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

	initialized bool
}

func NewBlock(reader BlockReader) *Block {
	return &Block{
		reader: reader,
	}
}

func (b *Block) LoadHeaders() error {
	// TODO(owen-d): better control over when to decode
	if !b.initialized {
		idx, err := b.reader.Index()
		if err != nil {
			return errors.Wrap(err, "getting index reader")
		}

		if err := b.index.DecodeHeaders(idx); err != nil {
			return errors.Wrap(err, "decoding index")
		}

		blooms, err := b.reader.Blooms()
		if err != nil {
			return errors.Wrap(err, "getting blooms reader")
		}
		if err := b.blooms.DecodeHeaders(blooms); err != nil {
			return errors.Wrap(err, "decoding blooms")
		}
		b.initialized = true
	}
	return nil

}

func (b *Block) Series() *LazySeriesIter {
	return NewLazySeriesIter(b)
}

func (b *Block) Blooms() *LazyBloomIter {
	return NewLazyBloomIter(b)
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
