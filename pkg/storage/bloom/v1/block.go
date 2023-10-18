package v1

import (
	"fmt"

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

func (bq *BlockQuerier) Seek(fp model.Fingerprint) error {
	return bq.series.Seek(fp)
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

// CheckChunksForSeries checks if the given chunks pass a set of searches in the given bloom block.
// It returns the list of chunks which will need to be downloaded for a query based on the initial list
// passed as the `chks` argument. Chunks will be removed from the result set if they they are indexed in the bloom
// and fail to pass all the searches.
func (bq *BlockQuerier) CheckChunksForSeries(fp model.Fingerprint, chks ChunkRefs, searches [][]byte) (ChunkRefs, error) {
	if err := bq.Seek(fp); err != nil {
		return chks, errors.Wrapf(err, "seeking to series for fp: %v", fp)
	}

	if !bq.series.Next() {
		return chks, nil
	}

	series := bq.series.At()
	if series.Fingerprint != fp {
		return chks, nil
	}

	bq.blooms.Seek(series.Offset)
	if !bq.blooms.Next() {
		return chks, fmt.Errorf("seeking to bloom for fp: %v", fp)
	}

	bloom := bq.blooms.At()

	// First, see if the search passes the series level bloom before checking for chunks individually
	for _, search := range searches {
		if !bloom.Test(search) {
			// the entire series bloom didn't pass one of the searches,
			// so we can skip checking chunks individually.
			// We still return all chunks that are not included in the bloom
			// as they may still have the data
			return chks.Unless(series.Chunks), nil
		}
	}

	// TODO(owen-d): pool, memoize chunk search prefix creation

	// Check chunks individually now
	mustCheck, inBlooms := chks.Compare(series.Chunks, true)

outer:
	for _, chk := range inBlooms {
		for _, search := range searches {
			// TODO(owen-d): meld chunk + search into a single byte slice from the block schema
			var combined = search

			if !bloom.Test(combined) {
				continue outer
			}
		}
		// chunk passed all searches, add to the list of chunks to download
		mustCheck = append(mustCheck, chk)

	}
	return mustCheck, nil
}
