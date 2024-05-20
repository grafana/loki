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

// NewBlockQuerier returns a new BlockQuerier for the given block.
// WARNING: If noCapture is true, the underlying byte slice of the bloom page
// will be returned to the pool for efficiency. This can only safely be used
// when the underlying bloom bytes don't escape the decoder, i.e.
// when loading blooms for querying (bloom-gw) but not for writing (bloom-compactor).
func NewBlockQuerier(b *Block, noCapture bool, maxPageSize int) *BlockQuerier {
	return &BlockQuerier{
		block:  b,
		series: NewLazySeriesIter(b),
		blooms: NewLazyBloomIter(b, noCapture, maxPageSize),
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
		if skip := bq.blooms.LoadOffset(series.Offset); skip {
			// can't seek to the desired bloom, likely because the page was too large to load
			// so we skip this series and move on to the next
			continue
		}
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
