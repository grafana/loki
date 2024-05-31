package v1

import "github.com/pkg/errors"

/*
Each binary format (version) has it's own builder. This provides type-safe way to build the binary format
while allowing reuse of underlying logic. As an example, the V2Builder will prevent encoding v1 series (only 1 bloom per series)
as it only provides methods that are v2 compatible. The opposite is also true.

Builders provide the following methods:
- [Convenience method] BuildFrom: builds the binary format from an iterator of the relevant type.
		Primarily used in testing since the MergeBuilder will be used in production and uses the lower level APIs below.

- AddBloom: adds a bloom filter to the binary format and returns the offset at which it was added.
- AddSeries: adds a series to the binary format and returns a boolean indicating if the series was added or not.
- Close: closes the builder and returns the number of bytes written.
*/

// Convenience constructor targeting the most current version.
func NewBlockBuilder(opts BlockOptions, writer BlockWriter) (*V2Builder, error) {
	return NewBlockBuilderV2(opts, writer)
}

// Convenience alias for
type BlockBuilder = V2Builder

type V2Builder struct {
	opts BlockOptions

	writer BlockWriter
	index  *IndexBuilder
	blooms *BloomBlockBuilder
}

type SeriesWithBlooms struct {
	Series *Series
	Blooms SizedIterator[*Bloom]
}

func NewBlockBuilderV2(opts BlockOptions, writer BlockWriter) (*V2Builder, error) {
	if opts.Schema.version != V2 {
		return nil, errors.Errorf("schema mismatch creating v2 builder, expected %v, got %v", V2, opts.Schema.version)
	}

	index, err := writer.Index()
	if err != nil {
		return nil, errors.Wrap(err, "initializing index writer")
	}
	blooms, err := writer.Blooms()
	if err != nil {
		return nil, errors.Wrap(err, "initializing blooms writer")
	}

	return &V2Builder{
		opts:   opts,
		writer: writer,
		index:  NewIndexBuilder(opts, index),
		blooms: NewBloomBlockBuilder(opts, blooms),
	}, nil
}

func (b *V2Builder) BuildFrom(itr Iterator[SeriesWithBlooms]) (uint32, error) {
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
		blockFull, err := b.AddSeries(*at.Series, offsets)
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

func (b *V2Builder) Close() (uint32, error) {
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

func (b *V2Builder) AddBloom(bloom *Bloom) (BloomOffset, error) {
	return b.blooms.Append(bloom)
}

// AddSeries adds a series to the block. It returns true after adding the series, the block is full.
func (b *V2Builder) AddSeries(series Series, offsets []BloomOffset) (bool, error) {
	if err := b.index.Append(SeriesWithOffsets{
		Offsets: offsets,
		Series:  series,
	}); err != nil {
		return false, errors.Wrapf(err, "writing index for series %v", series.Fingerprint)
	}

	full, _, err := b.writer.Full(b.opts.BlockSize)
	if err != nil {
		return false, errors.Wrap(err, "checking if block is full")
	}

	return full, nil
}

// Now the same for legacy V1
type SeriesWithBloom struct {
	Series *Series
	Bloom  *Bloom
}

type V1Builder struct {
	opts BlockOptions

	writer BlockWriter
	index  *IndexBuilder
	blooms *BloomBlockBuilder
}

func NewBlockBuilderV1(opts BlockOptions, writer BlockWriter) (*V2Builder, error) {
	if opts.Schema.version != V1 {
		return nil, errors.Errorf("schema mismatch creating v1 builder, expected %v, got %v", V1, opts.Schema.version)
	}

	index, err := writer.Index()
	if err != nil {
		return nil, errors.Wrap(err, "initializing index writer")
	}
	blooms, err := writer.Blooms()
	if err != nil {
		return nil, errors.Wrap(err, "initializing blooms writer")
	}

	return &V2Builder{
		opts:   opts,
		writer: writer,
		index:  NewIndexBuilder(opts, index),
		blooms: NewBloomBlockBuilder(opts, blooms),
	}, nil
}

func (b *V1Builder) BuildFrom(itr Iterator[SeriesWithBloom]) (uint32, error) {
	for itr.Next() {
		at := itr.At()
		offset, err := b.AddBloom(at.Bloom)
		if err != nil {
			return 0, errors.Wrap(err, "writing bloom")
		}

		blockFull, err := b.AddSeries(*at.Series, offset)

		if err != nil {
			return 0, errors.Wrapf(err, "writing series")
		}
		if blockFull {
			break
		}
	}

	if err := itr.Err(); err != nil {
		return 0, errors.Wrap(err, "iterating series")
	}

	return b.Close()
}

func (b *V1Builder) Close() (uint32, error) {
	// Implement your logic here
	return 0, nil
}

func (b *V1Builder) AddBloom(bloom *Bloom) (BloomOffset, error) {
	// Implement your logic here
	return BloomOffset{}, nil
}

func (b *V1Builder) AddSeries(series Series, offset BloomOffset) (bool, error) {
	// Implement your logic here
	return false, nil
}
