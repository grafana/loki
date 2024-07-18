package builder

import (
	"context"
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
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
	Generate(ctx context.Context) (skippedBlocks []v1.BlockMetadata, toClose []io.Closer, results iter.Iterator[*v1.Block], err error)
}

// Simple implementation of a BloomGenerator.
type SimpleBloomGenerator struct {
	userID      string
	store       iter.Iterator[*v1.Series]
	chunkLoader ChunkLoader
	blocksIter  iter.ResetIterator[*v1.SeriesWithBlooms]

	// options to build blocks with
	opts v1.BlockOptions

	metrics *v1.Metrics
	logger  log.Logger

	writerReaderFunc func() (v1.BlockWriter, v1.BlockReader)
	reporter         func(model.Fingerprint)

	tokenizer *v1.BloomTokenizer
}

// SimpleBloomGenerator is a foundational implementation of BloomGenerator.
// It mainly wires up a few different components to generate bloom filters for a set of blocks
// and handles schema compatibility:
// Blocks which are incompatible with the schema are skipped and will have their chunks reindexed
func NewSimpleBloomGenerator(
	userID string,
	opts v1.BlockOptions,
	store iter.Iterator[*v1.Series],
	chunkLoader ChunkLoader,
	blocksIter iter.ResetIterator[*v1.SeriesWithBlooms],
	writerReaderFunc func() (v1.BlockWriter, v1.BlockReader),
	reporter func(model.Fingerprint),
	metrics *v1.Metrics,
	logger log.Logger,
) *SimpleBloomGenerator {
	return &SimpleBloomGenerator{
		userID:      userID,
		opts:        opts,
		store:       store,
		chunkLoader: chunkLoader,
		blocksIter:  blocksIter,
		logger: log.With(
			logger,
			"component", "bloom_generator",
			"org_id", userID,
		),
		writerReaderFunc: writerReaderFunc,
		metrics:          metrics,
		reporter:         reporter,

		tokenizer: v1.NewBloomTokenizer(
			opts.Schema.NGramLen(),
			opts.Schema.NGramSkip(),
			int(opts.UnencodedBlockOptions.MaxBloomSizeBytes),
			metrics,
			log.With(
				logger,
				"component", "bloom_tokenizer",
				"org_id", userID,
			),
		),
	}
}

func (s *SimpleBloomGenerator) populator(ctx context.Context) v1.BloomPopulatorFunc {
	return func(
		series *v1.Series,
		srcBlooms iter.SizedIterator[*v1.Bloom],
		toAdd v1.ChunkRefs,
		ch chan *v1.BloomCreation,
	) {
		level.Debug(s.logger).Log(
			"msg", "populating bloom filter",
			"stage", "before",
			"fp", series.Fingerprint,
			"chunks", len(series.Chunks),
		)
		chunkItersWithFP := s.chunkLoader.Load(ctx, s.userID, &v1.Series{
			Fingerprint: series.Fingerprint,
			Chunks:      toAdd,
		})

		s.tokenizer.Populate(srcBlooms, chunkItersWithFP.itr, ch)

		if s.reporter != nil {
			s.reporter(series.Fingerprint)
		}
	}
}

func (s *SimpleBloomGenerator) Generate(ctx context.Context) *LazyBlockBuilderIterator {
	level.Debug(s.logger).Log("msg", "generating bloom filters for blocks", "schema", fmt.Sprintf("%+v", s.opts.Schema))

	series := iter.NewPeekIter(s.store)

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

	return NewLazyBlockBuilderIterator(ctx, s.opts, s.metrics, s.populator(ctx), s.writerReaderFunc, series, s.blocksIter)
}

// LazyBlockBuilderIterator is a lazy iterator over blocks that builds
// each block by adding series to them until they are full.
type LazyBlockBuilderIterator struct {
	ctx              context.Context
	opts             v1.BlockOptions
	metrics          *v1.Metrics
	populate         v1.BloomPopulatorFunc
	writerReaderFunc func() (v1.BlockWriter, v1.BlockReader)
	series           iter.PeekIterator[*v1.Series]
	blocks           iter.ResetIterator[*v1.SeriesWithBlooms]

	bytesAdded int
	curr       *v1.Block
	err        error
}

func NewLazyBlockBuilderIterator(
	ctx context.Context,
	opts v1.BlockOptions,
	metrics *v1.Metrics,
	populate v1.BloomPopulatorFunc,
	writerReaderFunc func() (v1.BlockWriter, v1.BlockReader),
	series iter.PeekIterator[*v1.Series],
	blocks iter.ResetIterator[*v1.SeriesWithBlooms],
) *LazyBlockBuilderIterator {
	return &LazyBlockBuilderIterator{
		ctx:              ctx,
		opts:             opts,
		metrics:          metrics,
		populate:         populate,
		writerReaderFunc: writerReaderFunc,
		series:           series,
		blocks:           blocks,
	}
}

func (b *LazyBlockBuilderIterator) Bytes() (bytes int) {
	return b.bytesAdded
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

	mergeBuilder := v1.NewMergeBuilder(b.blocks, b.series, b.populate, b.metrics)
	writer, reader := b.writerReaderFunc()
	blockBuilder, err := v1.NewBlockBuilder(b.opts, writer)
	if err != nil {
		_ = writer.Cleanup()
		b.err = errors.Wrap(err, "failed to create bloom block builder")
		return false
	}
	_, sourceBytes, err := mergeBuilder.Build(blockBuilder)
	b.bytesAdded += sourceBytes

	if err != nil {
		_ = writer.Cleanup()
		b.err = errors.Wrap(err, "failed to build bloom block")
		return false
	}

	b.curr = v1.NewBlock(reader, b.metrics)
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
	itr iter.Iterator[v1.ChunkRefWithIter]
}

// ChunkLoader loads chunks from a store
type ChunkLoader interface {
	Load(ctx context.Context, userID string, series *v1.Series) *ChunkItersByFingerprint
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

func (s *StoreChunkLoader) Load(ctx context.Context, userID string, series *v1.Series) *ChunkItersByFingerprint {
	// NB(owen-d): This is probably unnecessary as we should only have one fetcher
	// because we'll only be working on a single index period at a time, but this should protect
	// us in the case of refactoring/changing this and likely isn't a perf bottleneck.
	chksByFetcher := make(map[*fetcher.Fetcher][]chunk.Chunk)
	for _, chk := range series.Chunks {
		fetcher := s.fetcherProvider.GetChunkFetcher(chk.From)
		chksByFetcher[fetcher] = append(chksByFetcher[fetcher], chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(series.Fingerprint),
				UserID:      userID,
				From:        chk.From,
				Through:     chk.Through,
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
	}
}
