package bloomcompactor

import (
	"context"
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

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
	Generate(ctx context.Context) (skippedBlocks []v1.BlockMetadata, toClose []io.Closer, results v1.Iterator[*v1.Block], err error)
}

// Simple implementation of a BloomGenerator.
type SimpleBloomGenerator struct {
	userID      string
	store       v1.Iterator[*v1.Series]
	chunkLoader ChunkLoader
	blocksIter  v1.ResettableIterator[*v1.SeriesWithBloom]

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
	userID string,
	opts v1.BlockOptions,
	store v1.Iterator[*v1.Series],
	chunkLoader ChunkLoader,
	blocksIter v1.ResettableIterator[*v1.SeriesWithBloom],
	readWriterFn func() (v1.BlockWriter, v1.BlockReader),
	metrics *Metrics,
	logger log.Logger,
) *SimpleBloomGenerator {
	return &SimpleBloomGenerator{
		userID:       userID,
		opts:         opts,
		store:        store,
		chunkLoader:  chunkLoader,
		blocksIter:   blocksIter,
		logger:       log.With(logger, "component", "bloom_generator"),
		readWriterFn: readWriterFn,
		metrics:      metrics,

		tokenizer: v1.NewBloomTokenizer(opts.Schema.NGramLen(), opts.Schema.NGramSkip(), metrics.bloomMetrics),
	}
}

func (s *SimpleBloomGenerator) populator(ctx context.Context) func(series *v1.Series, bloom *v1.Bloom) error {
	return func(series *v1.Series, bloom *v1.Bloom) error {
		chunkItersWithFP, err := s.chunkLoader.Load(ctx, s.userID, series)
		if err != nil {
			return errors.Wrapf(err, "failed to load chunks for series: %+v", series)
		}

		return s.tokenizer.Populate(
			&v1.SeriesWithBloom{
				Series: series,
				Bloom:  bloom,
			},
			chunkItersWithFP.itr,
		)
	}

}

func (s *SimpleBloomGenerator) Generate(ctx context.Context) v1.Iterator[*v1.Block] {
	level.Debug(s.logger).Log("msg", "generating bloom filters for blocks", "schema", fmt.Sprintf("%+v", s.opts.Schema))

	series := v1.NewPeekingIter(s.store)

	// TODO: Use interface
	impl, ok := s.blocksIter.(*blockLoadingIter)
	if ok {
		impl.Filter(
			func(bq *bloomshipper.CloseableBlockQuerier) bool {

				logger := log.With(s.logger, "block", bq.BlockRef)
				md, err := bq.Metadata()
				schema := md.Options.Schema
				if err != nil {
					level.Warn(logger).Log("msg", "failed to get schema for block", "err", err)
					bq.Close() // close unused querier
					return false
				}

				if !s.opts.Schema.Compatible(schema) {
					level.Warn(logger).Log("msg", "block schema incompatible with options", "generator_schema", fmt.Sprintf("%+v", s.opts.Schema), "block_schema", fmt.Sprintf("%+v", schema))
					bq.Close() // close unused querier
					return false
				}

				level.Debug(logger).Log("msg", "adding compatible block to bloom generation inputs")
				return true
			},
		)
	}

	return NewLazyBlockBuilderIterator(ctx, s.opts, s.metrics, s.populator(ctx), s.readWriterFn, series, s.blocksIter)
}

// LazyBlockBuilderIterator is a lazy iterator over blocks that builds
// each block by adding series to them until they are full.
type LazyBlockBuilderIterator struct {
	ctx          context.Context
	opts         v1.BlockOptions
	metrics      *Metrics
	populate     func(*v1.Series, *v1.Bloom) error
	readWriterFn func() (v1.BlockWriter, v1.BlockReader)
	series       v1.PeekingIterator[*v1.Series]
	blocks       v1.ResettableIterator[*v1.SeriesWithBloom]

	curr *v1.Block
	err  error
}

func NewLazyBlockBuilderIterator(
	ctx context.Context,
	opts v1.BlockOptions,
	metrics *Metrics,
	populate func(*v1.Series, *v1.Bloom) error,
	readWriterFn func() (v1.BlockWriter, v1.BlockReader),
	series v1.PeekingIterator[*v1.Series],
	blocks v1.ResettableIterator[*v1.SeriesWithBloom],
) *LazyBlockBuilderIterator {
	return &LazyBlockBuilderIterator{
		ctx:          ctx,
		opts:         opts,
		metrics:      metrics,
		populate:     populate,
		readWriterFn: readWriterFn,
		series:       series,
		blocks:       blocks,
	}
}

func (b *LazyBlockBuilderIterator) Next() bool {
	// No more series to process
	if _, hasNext := b.series.Peek(); !hasNext {
		return false
	}

	if err := b.ctx.Err(); err != nil {
		b.err = errors.Wrap(err, "context canceled")
		return false
	}

	if err := b.blocks.Reset(); err != nil {
		b.err = errors.Wrap(err, "reset blocks iterator")
		return false
	}

	mergeBuilder := v1.NewMergeBuilder(b.blocks, b.series, b.populate, b.metrics.bloomMetrics)
	writer, reader := b.readWriterFn()
	blockBuilder, err := v1.NewBlockBuilder(b.opts, writer)
	if err != nil {
		b.err = errors.Wrap(err, "failed to create bloom block builder")
		return false
	}
	_, err = mergeBuilder.Build(blockBuilder)
	if err != nil {
		b.err = errors.Wrap(err, "failed to build bloom block")
		return false
	}

	b.curr = v1.NewBlock(reader)
	return true
}

func (b *LazyBlockBuilderIterator) At() *v1.Block {
	return b.curr
}

func (b *LazyBlockBuilderIterator) Err() error {
	return b.err
}

// IndexLoader loads an index. This helps us do things like
// load TSDBs for a specific period excluding multitenant (pre-compacted) indices
type indexLoader interface {
	Index() (tsdb.Index, error)
}

// ChunkItersByFingerprint models the chunks belonging to a fingerprint
type ChunkItersByFingerprint struct {
	fp  model.Fingerprint
	itr v1.Iterator[v1.ChunkRefWithIter]
}

// ChunkLoader loads chunks from a store
type ChunkLoader interface {
	Load(ctx context.Context, userID string, series *v1.Series) (*ChunkItersByFingerprint, error)
}

// StoreChunkLoader loads chunks from a store
type StoreChunkLoader struct {
	fetcherProvider stores.ChunkFetcherProvider
	metrics         *Metrics
}

func NewStoreChunkLoader(fetcherProvider stores.ChunkFetcherProvider, metrics *Metrics) *StoreChunkLoader {
	return &StoreChunkLoader{
		fetcherProvider: fetcherProvider,
		metrics:         metrics,
	}
}

func (s *StoreChunkLoader) Load(ctx context.Context, userID string, series *v1.Series) (*ChunkItersByFingerprint, error) {
	// NB(owen-d): This is probably unnecessary as we should only have one fetcher
	// because we'll only be working on a single index period at a time, but this should protect
	// us in the case of refactoring/changing this and likely isn't a perf bottleneck.
	chksByFetcher := make(map[*fetcher.Fetcher][]chunk.Chunk)
	for _, chk := range series.Chunks {
		fetcher := s.fetcherProvider.GetChunkFetcher(chk.Start)
		chksByFetcher[fetcher] = append(chksByFetcher[fetcher], chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(series.Fingerprint),
				UserID:      userID,
				From:        chk.Start,
				Through:     chk.End,
				Checksum:    chk.Checksum,
			},
		})
	}

	var (
		fetchers = make([]Fetcher[chunk.Chunk, chunk.Chunk], 0, len(chksByFetcher))
		inputs   = make([][]chunk.Chunk, 0, len(chksByFetcher))
	)
	for fetcher, chks := range chksByFetcher {
		fn := FetchFunc[chunk.Chunk, chunk.Chunk](fetcher.FetchChunks)
		fetchers = append(fetchers, fn)
		inputs = append(inputs, chks)
	}

	return &ChunkItersByFingerprint{
		fp:  series.Fingerprint,
		itr: newBatchedChunkLoader(ctx, fetchers, inputs, s.metrics, batchedLoaderDefaultBatchSize),
	}, nil
}
