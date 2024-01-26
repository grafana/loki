package bloomcompactor

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
)

// TODO(owen-d): add metrics
type Metrics struct {
	bloomMetrics *v1.Metrics
}

func NewMetrics(_ prometheus.Registerer, bloomMetrics *v1.Metrics) *Metrics {
	return &Metrics{
		bloomMetrics: bloomMetrics,
	}
}

// inclusive range
type Keyspace struct {
	min, max model.Fingerprint
}

func (k Keyspace) Cmp(other Keyspace) v1.BoundsCheck {
	if other.max < k.min {
		return v1.Before
	} else if other.min > k.max {
		return v1.After
	}
	return v1.Overlap
}

// Store is likely bound within. This allows specifying impls like ShardedStore<Store>
// to only request the shard-range needed from the existing store.
type BloomGenerator interface {
	Generate(ctx context.Context) (skippedBlocks []*v1.Block, results v1.Iterator[*v1.Block], err error)
}

// Simple implementation of a BloomGenerator.
type SimpleBloomGenerator struct {
	store v1.Iterator[*v1.Series]
	// TODO(owen-d): blocks need not be all downloaded prior. Consider implementing
	// as an iterator of iterators, where each iterator is a batch of overlapping blocks.
	blocks []*v1.Block

	// options to build blocks with
	opts v1.BlockOptions

	metrics *Metrics
	logger  log.Logger

	readWriterFn func() (v1.BlockWriter, v1.BlockReader)

	tokenizer *v1.BloomTokenizer
}

// SimpleBloomGenerator is a foundational implementation of BloomGenerator.
// It mainly wires up a few different components to generate bloom filters for a set of blocks
// and handles schema compatibility:
// Blocks which are incompatible with the schema are skipped and will have their chunks reindexed
func NewSimpleBloomGenerator(
	opts v1.BlockOptions,
	store v1.Iterator[*v1.Series],
	blocks []*v1.Block,
	readWriterFn func() (v1.BlockWriter, v1.BlockReader),
	metrics *Metrics,
	logger log.Logger,
) *SimpleBloomGenerator {
	return &SimpleBloomGenerator{
		opts:         opts,
		store:        store,
		blocks:       blocks,
		logger:       logger,
		readWriterFn: readWriterFn,
		metrics:      metrics,

		tokenizer: v1.NewBloomTokenizer(opts.Schema.NGramLen(), opts.Schema.NGramSkip(), metrics.bloomMetrics),
	}
}

func (s *SimpleBloomGenerator) populate(series *v1.Series, bloom *v1.Bloom) error {
	// TODO(owen-d): impl after threading in store
	var chunkItr v1.Iterator[[]chunk.Chunk] = v1.NewEmptyIter[[]chunk.Chunk](nil)

	return s.tokenizer.PopulateSeriesWithBloom(
		&v1.SeriesWithBloom{
			Series: series,
			Bloom:  bloom,
		},
		chunkItr,
	)
}

func (s *SimpleBloomGenerator) Generate(_ context.Context) (skippedBlocks []*v1.Block, results v1.Iterator[*v1.Block], err error) {

	blocksMatchingSchema := make([]v1.PeekingIterator[*v1.SeriesWithBloom], 0, len(s.blocks))
	for _, block := range s.blocks {
		// TODO(owen-d): implement block naming so we can log the affected block in all these calls
		logger := log.With(s.logger, "block", fmt.Sprintf("%#v", block))
		schema, err := block.Schema()
		if err != nil {
			level.Warn(logger).Log("msg", "failed to get schema for block", "err", err)
			skippedBlocks = append(skippedBlocks, block)
		}

		if !s.opts.Schema.Compatible(schema) {
			level.Warn(logger).Log("msg", "block schema incompatible with options", "generator_schema", fmt.Sprintf("%#v", s.opts.Schema), "block_schema", fmt.Sprintf("%#v", schema))
			skippedBlocks = append(skippedBlocks, block)
		}

		level.Debug(logger).Log("msg", "adding compatible block to bloom generation inputs")
		itr := v1.NewPeekingIter[*v1.SeriesWithBloom](v1.NewBlockQuerier(block))
		blocksMatchingSchema = append(blocksMatchingSchema, itr)
	}

	level.Debug(s.logger).Log("msg", "generating bloom filters for blocks", "num_blocks", len(blocksMatchingSchema), "skipped_blocks", len(skippedBlocks), "schema", fmt.Sprintf("%#v", s.opts.Schema))

	// TODO(owen-d): implement bounded block sizes

	mergeBuilder := v1.NewMergeBuilder(blocksMatchingSchema, s.store, s.populate)
	writer, reader := s.readWriterFn()
	blockBuilder, err := v1.NewBlockBuilder(v1.NewBlockOptionsFromSchema(s.opts.Schema), writer)
	if err != nil {
		return skippedBlocks, nil, errors.Wrap(err, "failed to create bloom block builder")
	}
	_, err = mergeBuilder.Build(blockBuilder)
	if err != nil {
		return skippedBlocks, nil, errors.Wrap(err, "failed to build bloom block")
	}

	return skippedBlocks, v1.NewSliceIter[*v1.Block]([]*v1.Block{v1.NewBlock(reader)}), nil

}
