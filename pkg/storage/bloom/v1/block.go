package v1

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

type BlockMetadata struct {
	Options  BlockOptions
	Series   SeriesHeader
	Checksum uint32
}

type Block struct {
	metrics *Metrics
	// covers series pages
	index BlockIndex
	// covers bloom pages
	blooms BloomBlock

	metadata BlockMetadata

	reader BlockReader // should this be decoupled from the struct (accepted as method arg instead)?

	initialized bool
}

func NewBlock(reader BlockReader, metrics *Metrics) *Block {
	return &Block{
		reader:  reader,
		metrics: metrics,
	}
}

func (b *Block) Reader() BlockReader {
	return b.reader
}

func (b *Block) LoadHeaders() error {
	// TODO(owen-d): better control over when to decode
	if !b.initialized {
		idx, err := b.reader.Index()
		if err != nil {
			return errors.Wrap(err, "getting index reader")
		}

		indexChecksum, err := b.index.DecodeHeaders(idx)
		if err != nil {
			return errors.Wrap(err, "decoding index")
		}

		b.metadata.Options = b.index.opts

		// TODO(owen-d): better pattern
		xs := make([]SeriesHeader, 0, len(b.index.pageHeaders))
		for _, h := range b.index.pageHeaders {
			xs = append(xs, h.SeriesHeader)
		}
		b.metadata.Series = aggregateHeaders(xs)

		blooms, err := b.reader.Blooms()
		if err != nil {
			return errors.Wrap(err, "getting blooms reader")
		}
		bloomChecksum, err := b.blooms.DecodeHeaders(blooms)
		if err != nil {
			return errors.Wrap(err, "decoding blooms")
		}
		b.initialized = true

		if !b.metadata.Options.Schema.Compatible(b.blooms.schema) {
			return fmt.Errorf(
				"schema mismatch: index (%v) vs blooms (%v)",
				b.metadata.Options.Schema, b.blooms.schema,
			)
		}

		b.metadata.Checksum = combineChecksums(indexChecksum, bloomChecksum)
	}
	return nil

}

// XOR checksums as a simple checksum combiner with the benefit that
// each part can be recomputed by XORing the result against the other
func combineChecksums(index, blooms uint32) uint32 {
	return index ^ blooms
}

func (b *Block) Series() *LazySeriesIter {
	return NewLazySeriesIter(b)
}

func (b *Block) Blooms() *LazyBloomIter {
	return NewLazyBloomIter(b)
}

func (b *Block) Metadata() (BlockMetadata, error) {
	if err := b.LoadHeaders(); err != nil {
		return BlockMetadata{}, err
	}
	return b.metadata, nil
}

func (b *Block) Schema() (Schema, error) {
	if err := b.LoadHeaders(); err != nil {
		return Schema{}, err
	}
	return b.metadata.Options.Schema, nil
}

type BlockQuerier struct {
	series *LazySeriesIter
	blooms *LazyBloomIter

	block *Block // ref to underlying block

	cur *SeriesWithBloom
}

func NewBlockQuerier(b *Block) *BlockQuerier {
	return &BlockQuerier{
		block:  b,
		series: NewLazySeriesIter(b),
		blooms: NewLazyBloomIter(b),
	}
}

func (bq *BlockQuerier) Metadata() (BlockMetadata, error) {
	return bq.block.Metadata()
}

func (bq *BlockQuerier) Schema() (Schema, error) {
	return bq.block.Schema()
}

func (bq *BlockQuerier) Reset() error {
	return bq.series.Seek(0)
}

func (bq *BlockQuerier) Seek(fp model.Fingerprint) error {
	return bq.series.Seek(fp)
}

func (bq *BlockQuerier) Next() bool {
	for bq.series.Next() {
		series := bq.series.At()
		bq.blooms.Seek(series.Offset)
		if !bq.blooms.Next() {
			// skip blocks that are too large
			if errors.Is(bq.blooms.Err(), ErrPageTooLarge) {
				// fmt.Printf("skipping bloom page: %s (%d)\n", series.Fingerprint, series.Chunks.Len())
				bq.blooms.err = nil
				continue
			}
			return false
		}
		bloom := bq.blooms.At()
		bq.cur = &SeriesWithBloom{
			Series: &series.Series,
			Bloom:  bloom,
		}
		return true
	}
	return false
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
// passed as the `chks` argument. Chunks will be removed from the result set if they are indexed in the bloom
// and fail to pass all the searches.
func (bq *BlockQuerier) CheckChunksForSeries(fp model.Fingerprint, chks ChunkRefs, searches [][]byte) (ChunkRefs, error) {
	schema, err := bq.Schema()
	if err != nil {
		return chks, fmt.Errorf("getting schema: %w", err)
	}

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

	// TODO(salvacorts): pool tokenBuf
	var tokenBuf []byte
	var prefixLen int

	// Check chunks individually now
	mustCheck, inBlooms := chks.Compare(series.Chunks, true)

outer:
	for _, chk := range inBlooms {
		// Get buf to concatenate the chunk and search token
		tokenBuf, prefixLen = prefixedToken(schema.NGramLen(), chk, tokenBuf)
		for _, search := range searches {
			tokenBuf = append(tokenBuf[:prefixLen], search...)

			if !bloom.Test(tokenBuf) {
				// chunk didn't pass the search, continue to the next chunk
				continue outer
			}
		}
		// chunk passed all searches, add to the list of chunks to download
		mustCheck = append(mustCheck, chk)

	}
	return mustCheck, nil
}
