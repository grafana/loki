package v1

import (
	"github.com/pkg/errors"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

/*
Each binary format (version) has it's own builder. This provides type-safe way to build the binary format
while allowing reuse of underlying logic. As an example, the V3Builder will prevent encoding v1 and v2 series
as it only provides methods that are v3 compatible. The opposite is also true.

Builders provide the following methods:
- [Convenience method] BuildFrom: builds the binary format from an iterator of the relevant type.
		Primarily used in testing since the MergeBuilder will be used in production and uses the lower level APIs below.

- AddBloom: adds a bloom filter to the binary format and returns the offset at which it was added.
- AddSeries: adds a series to the binary format and returns a boolean indicating if the series was added or not.
- Close: closes the builder and returns the number of bytes written.
*/

// Convenience constructor targeting the most current version.
func NewBlockBuilder(opts BlockOptions, writer BlockWriter) (*BlockBuilder, error) {
	return NewBlockBuilderV3(opts, writer)
}

// Convenience alias for the most current version.
type BlockBuilder = V3Builder

type V3Builder struct {
	opts BlockOptions

	writer BlockWriter
	index  *IndexBuilder
	blooms *BloomBlockBuilder
}

type SeriesWithBlooms struct {
	Series *SeriesWithMeta
	Blooms iter.SizedIterator[*Bloom]
}

func NewBlockBuilderV3(opts BlockOptions, writer BlockWriter) (*V3Builder, error) {
	if opts.Schema.version != V3 {
		return nil, errors.Errorf("schema mismatch creating builder, expected v3, got %v", opts.Schema.version)
	}

	index, err := writer.Index()
	if err != nil {
		return nil, errors.Wrap(err, "initializing index writer")
	}
	blooms, err := writer.Blooms()
	if err != nil {
		return nil, errors.Wrap(err, "initializing blooms writer")
	}

	return &V3Builder{
		opts:   opts,
		writer: writer,
		index:  NewIndexBuilder(opts, index),
		blooms: NewBloomBlockBuilder(opts, blooms),
	}, nil
}

// BuildFrom is only used in tests as helper function to create blocks
// It does not take indexed fields into account.
func (b *V3Builder) BuildFrom(itr iter.Iterator[SeriesWithBlooms]) (uint32, error) {
	for itr.Next() {
		at := itr.At()
		var offsets []BloomOffset
		for at.Blooms.Next() {
			offset, err := b.AddBloom(at.Blooms.At())
			if err != nil {
				return 0, errors.Wrap(err, "writing bloom")
			}
			offsets = append(offsets, offset)
		}

		if err := at.Blooms.Err(); err != nil {
			return 0, errors.Wrap(err, "iterating blooms")
		}

		blockFull, err := b.AddSeries(at.Series.Series, offsets, at.Series.Fields)
		if err != nil {
			return 0, errors.Wrapf(err, "writing series")
		}
		if blockFull {
			break
		}
	}

	if err := itr.Err(); err != nil {
		return 0, errors.Wrap(err, "iterating series with blooms")
	}

	return b.Close()
}

func (b *V3Builder) Close() (uint32, error) {
	bloomChecksum, err := b.blooms.Close()
	if err != nil {
		return 0, errors.Wrap(err, "closing bloom file")
	}
	indexCheckSum, err := b.index.Close()
	if err != nil {
		return 0, errors.Wrap(err, "closing series file")
	}
	return combineChecksums(indexCheckSum, bloomChecksum), nil
}

func (b *V3Builder) AddBloom(bloom *Bloom) (BloomOffset, error) {
	return b.blooms.Append(bloom)
}

// AddSeries adds a series to the block. It returns true after adding the series, the block is full.
func (b *V3Builder) AddSeries(series Series, offsets []BloomOffset, fields Set[Field]) (bool, error) {
	if err := b.index.Append(SeriesWithMeta{
		Series: series,
		Meta: Meta{
			Offsets: offsets,
			Fields:  fields,
		},
	}); err != nil {
		return false, errors.Wrapf(err, "writing index for series %v", series.Fingerprint)
	}

	full, err := b.full()
	if err != nil {
		return false, errors.Wrap(err, "checking if block is full")
	}

	return full, nil
}

func (b *V3Builder) full() (bool, error) {
	if b.opts.BlockSize == 0 {
		// Unlimited block size
		return false, nil
	}

	full, writtenSize, err := b.writer.Full(b.opts.BlockSize)
	if err != nil {
		return false, errors.Wrap(err, "checking if block writer is full")
	}
	if full {
		return true, nil
	}

	// Even if the block writer is not full, we may have unflushed data in the bloom builders.
	// Check if by flushing these, we would exceed the block size.
	unflushedIndexSize := b.index.UnflushedSize()
	unflushedBloomSize := b.blooms.UnflushedSize()
	if uint64(writtenSize+unflushedIndexSize+unflushedBloomSize) > b.opts.BlockSize {
		return true, nil
	}

	return false, nil
}
