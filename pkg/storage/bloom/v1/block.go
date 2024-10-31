package v1

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/util/mempool"
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
	*LazySeriesIter
	blooms *LazyBloomIter

	block *Block // ref to underlying block
}

// NewBlockQuerier returns a new BlockQuerier for the given block.
// WARNING: You can pass an implementation of Allocator that is responsible for
// whether the underlying byte slice of the bloom page will be returned to the
// pool for efficiency or not. Returning to the pool can only safely be used
// when the underlying bloom bytes don't escape the decoder, i.e. when loading
// blooms for querying (bloom-gateway), but not for writing (bloom-builder).
// Therefore, when calling NewBlockQuerier on the write path, you should always
// pass the SimpleHeapAllocator implementation of the Allocator interface.
func NewBlockQuerier(b *Block, alloc mempool.Allocator, maxPageSize int) *BlockQuerier {
	return &BlockQuerier{
		block:          b,
		LazySeriesIter: NewLazySeriesIter(b),
		blooms:         NewLazyBloomIter(b, alloc, maxPageSize),
	}
}

func (bq *BlockQuerier) Metadata() (BlockMetadata, error) {
	return bq.block.Metadata()
}

func (bq *BlockQuerier) Schema() (Schema, error) {
	return bq.block.Schema()
}

func (bq *BlockQuerier) Reset() error {
	bq.blooms.Reset()
	return bq.Seek(0)
}

func (bq *BlockQuerier) Err() error {
	if err := bq.LazySeriesIter.Err(); err != nil {
		return err
	}

	return bq.blooms.Err()
}

type BlockQuerierIter struct {
	*BlockQuerier
}

// Iter returns a new BlockQuerierIter, which changes the iteration type to SeriesWithBlooms,
// automatically loading the blooms for each series rather than requiring the caller to
// turn the offset to a `Bloom` via `LoadOffset`
func (bq *BlockQuerier) Iter() *BlockQuerierIter {
	return &BlockQuerierIter{BlockQuerier: bq}
}

func (b *BlockQuerierIter) Next() bool {
	next := b.LazySeriesIter.Next()
	if !next {
		b.blooms.Reset()
	}
	return next
}

func (b *BlockQuerierIter) At() *SeriesWithBlooms {
	s := b.LazySeriesIter.At()
	res := &SeriesWithBlooms{
		Series: s,
		Blooms: newOffsetsIter(b.blooms, s.Offsets),
	}
	return res
}

type offsetsIter struct {
	blooms  *LazyBloomIter
	offsets []BloomOffset
	cur     int
}

func newOffsetsIter(blooms *LazyBloomIter, offsets []BloomOffset) *offsetsIter {
	return &offsetsIter{
		blooms:  blooms,
		offsets: offsets,
	}
}

func (it *offsetsIter) Next() bool {
	for it.cur < len(it.offsets) {

		if skip := it.blooms.LoadOffset(it.offsets[it.cur]); skip {
			it.cur++
			continue
		}

		it.cur++
		return it.blooms.Next()

	}
	return false
}

func (it *offsetsIter) At() *Bloom {
	return it.blooms.At()
}

func (it *offsetsIter) Err() error {
	return it.blooms.Err()
}

func (it *offsetsIter) Remaining() int {
	return len(it.offsets) - it.cur
}
