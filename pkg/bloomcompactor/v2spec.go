package bloomcompactor

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	logql_log "github.com/grafana/loki/pkg/logql/log"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

/*
This file maintains a number of things supporting bloom generation. Most notably, the `BloomGenerator` interface/implementation which builds bloom filters.

- `BloomGenerator`: Builds blooms. Most other things in this file are supporting this in various ways.
- `SimpleBloomGenerator`: A foundational implementation of `BloomGenerator` which wires up a few different components to generate bloom filters for a set of blocks and handles schema compatibility:
- `chunkLoader`: Loads chunks w/ a specific fingerprint from the store, returns an iterator of chunk iterators. We return iterators rather than chunk implementations mainly for ease of testing. In practice, this will just be an iterator over `MemChunk`s.
*/

type Metrics struct {
	bloomMetrics *v1.Metrics
	chunkSize    prometheus.Histogram // uncompressed size of all chunks summed per series
}

func NewMetrics(r prometheus.Registerer, bloomMetrics *v1.Metrics) *Metrics {
	return &Metrics{
		bloomMetrics: bloomMetrics,
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunk_series_size",
			Help:    "Uncompressed size of chunks in a series",
			Buckets: prometheus.ExponentialBucketsRange(1024, 1073741824, 10),
		}),
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
	store       v1.Iterator[*v1.Series]
	chunkLoader ChunkLoader
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
	chunkLoader ChunkLoader,
	blocks []*v1.Block,
	readWriterFn func() (v1.BlockWriter, v1.BlockReader),
	metrics *Metrics,
	logger log.Logger,
) *SimpleBloomGenerator {
	return &SimpleBloomGenerator{
		opts: opts,
		// TODO(owen-d): implement Iterator[Series] against TSDB files to hook in here.
		store:        store,
		chunkLoader:  chunkLoader,
		blocks:       blocks,
		logger:       logger,
		readWriterFn: readWriterFn,
		metrics:      metrics,

		tokenizer: v1.NewBloomTokenizer(opts.Schema.NGramLen(), opts.Schema.NGramSkip(), metrics.bloomMetrics),
	}
}

func (s *SimpleBloomGenerator) populator(ctx context.Context) func(series *v1.Series, bloom *v1.Bloom) error {
	return func(series *v1.Series, bloom *v1.Bloom) error {
		chunkItersWithFP, err := s.chunkLoader.Load(ctx, series)
		if err != nil {
			return errors.Wrapf(err, "failed to load chunks for series: %#v", series)
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

func (s *SimpleBloomGenerator) Generate(ctx context.Context) (skippedBlocks []*v1.Block, results v1.Iterator[*v1.Block], err error) {

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

	mergeBuilder := v1.NewMergeBuilder(blocksMatchingSchema, s.store, s.populator(ctx))
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
	Load(context.Context, *v1.Series) (*ChunkItersByFingerprint, error)
}

// interface modeled from `pkg/storage/stores/composite_store.ChunkFetcherProvider`
type fetcherProvider interface {
	GetChunkFetcher(model.Time) chunkFetcher
}

// interface modeled from `pkg/storage/chunk/fetcher.Fetcher`
type chunkFetcher interface {
	FetchChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error)
}

// StoreChunkLoader loads chunks from a store
type StoreChunkLoader struct {
	userID          string
	fetcherProvider fetcherProvider
	metrics         *Metrics
}

func NewStoreChunkLoader(userID string, fetcherProvider fetcherProvider, metrics *Metrics) *StoreChunkLoader {
	return &StoreChunkLoader{
		userID:          userID,
		fetcherProvider: fetcherProvider,
		metrics:         metrics,
	}
}

func (s *StoreChunkLoader) Load(ctx context.Context, series *v1.Series) (*ChunkItersByFingerprint, error) {
	// TODO(owen-d): This is probalby unnecessary as we should only have one fetcher
	// because we'll only be working on a single index period at a time, but this should protect
	// us in the case of refactoring/changing this and likely isn't a perf bottleneck.
	chksByFetcher := make(map[chunkFetcher][]chunk.Chunk)
	for _, chk := range series.Chunks {
		fetcher := s.fetcherProvider.GetChunkFetcher(chk.Start)
		chksByFetcher[fetcher] = append(chksByFetcher[fetcher], chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(series.Fingerprint),
				UserID:      s.userID,
				From:        chk.Start,
				Through:     chk.End,
				Checksum:    chk.Checksum,
			},
		})
	}

	work := make([]chunkWork, 0, len(chksByFetcher))
	for fetcher, chks := range chksByFetcher {
		work = append(work, chunkWork{
			fetcher: fetcher,
			chks:    chks,
		})
	}

	return &ChunkItersByFingerprint{
		fp:  series.Fingerprint,
		itr: newBatchedLoader(ctx, work, batchedLoaderDefaultBatchSize, s.metrics),
	}, nil
}

type chunkWork struct {
	fetcher chunkFetcher
	chks    []chunk.Chunk
}

// batchedLoader implements `v1.Iterator[v1.ChunkRefWithIter]` in batches
// to ensure memory is bounded while loading chunks
// TODO(owen-d): testware
type batchedLoader struct {
	metrics   *Metrics
	batchSize int
	ctx       context.Context
	work      []chunkWork

	cur   v1.ChunkRefWithIter
	batch []chunk.Chunk
	err   error
}

const batchedLoaderDefaultBatchSize = 50

func newBatchedLoader(ctx context.Context, work []chunkWork, batchSize int, metrics *Metrics) *batchedLoader {
	return &batchedLoader{
		metrics:   metrics,
		batchSize: batchSize,
		ctx:       ctx,
		work:      work,
	}
}

func (b *batchedLoader) Next() bool {
	if len(b.batch) > 0 {
		b.cur, b.err = b.format(b.batch[0])
		b.batch = b.batch[1:]
		return b.err == nil
	}

	if len(b.work) == 0 {
		return false
	}

	// setup next batch
	next := b.work[0]
	batchSize := min(b.batchSize, len(next.chks))
	toFetch := next.chks[:batchSize]
	// update work
	b.work[0].chks = next.chks[batchSize:]
	if len(b.work[0].chks) == 0 {
		b.work = b.work[1:]
	}

	b.batch, b.err = next.fetcher.FetchChunks(b.ctx, toFetch)
	return b.err == nil
}

func (b *batchedLoader) format(c chunk.Chunk) (v1.ChunkRefWithIter, error) {
	chk := c.Data.(*chunkenc.Facade).LokiChunk()
	b.metrics.chunkSize.Observe(float64(chk.UncompressedSize()))
	itr, err := chk.Iterator(
		b.ctx,
		time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		logql_log.NewNoopPipeline().ForStream(c.Metric),
	)

	if err != nil {
		return v1.ChunkRefWithIter{}, err
	}

	return v1.ChunkRefWithIter{
		Ref: v1.ChunkRef{
			Start:    c.From,
			End:      c.Through,
			Checksum: c.Checksum,
		},
		Itr: itr,
	}, nil
}

func (b *batchedLoader) At() v1.ChunkRefWithIter {
	return b.cur
}

func (b *batchedLoader) Err() error {
	return b.err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
