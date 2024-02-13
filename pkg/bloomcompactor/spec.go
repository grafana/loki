package bloomcompactor

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	logql_log "github.com/grafana/loki/pkg/logql/log"
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
	blocksIter  v1.CloseableIterator[*bloomshipper.CloseableBlockQuerier]

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
	blocksIter v1.CloseableIterator[*bloomshipper.CloseableBlockQuerier],
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

func (s *SimpleBloomGenerator) Generate(ctx context.Context) ([]v1.BlockMetadata, []io.Closer, v1.Iterator[*v1.Block], error) {
	skippedBlocks := make([]v1.BlockMetadata, 0)
	toClose := make([]io.Closer, 0)
	blocksMatchingSchema := make([]*bloomshipper.CloseableBlockQuerier, 0)

	for s.blocksIter.Next() && s.blocksIter.Err() == nil {
		block := s.blocksIter.At()
		toClose = append(toClose, block)

		logger := log.With(s.logger, "block", block.BlockRef)
		md, err := block.Metadata()
		schema := md.Options.Schema
		if err != nil {
			level.Warn(logger).Log("msg", "failed to get schema for block", "err", err)
			skippedBlocks = append(skippedBlocks, md)
			continue
		}

		if !s.opts.Schema.Compatible(schema) {
			level.Warn(logger).Log("msg", "block schema incompatible with options", "generator_schema", fmt.Sprintf("%+v", s.opts.Schema), "block_schema", fmt.Sprintf("%+v", schema))
			skippedBlocks = append(skippedBlocks, md)
			continue
		}

		level.Debug(logger).Log("msg", "adding compatible block to bloom generation inputs")
		blocksMatchingSchema = append(blocksMatchingSchema, block)
	}

	if s.blocksIter.Err() != nil {
		// should we ignore the error and continue with the blocks we got?
		return skippedBlocks, toClose, v1.NewSliceIter([]*v1.Block{}), s.blocksIter.Err()
	}

	level.Debug(s.logger).Log("msg", "generating bloom filters for blocks", "num_blocks", len(blocksMatchingSchema), "skipped_blocks", len(skippedBlocks), "schema", fmt.Sprintf("%+v", s.opts.Schema))

	series := v1.NewPeekingIter(s.store)
	blockIter := NewLazyBlockBuilderIterator(ctx, s.opts, s.populator(ctx), s.readWriterFn, series, blocksMatchingSchema)
	return skippedBlocks, toClose, blockIter, nil
}

// LazyBlockBuilderIterator is a lazy iterator over blocks that builds
// each block by adding series to them until they are full.
type LazyBlockBuilderIterator struct {
	ctx          context.Context
	opts         v1.BlockOptions
	populate     func(*v1.Series, *v1.Bloom) error
	readWriterFn func() (v1.BlockWriter, v1.BlockReader)
	series       v1.PeekingIterator[*v1.Series]
	blocks       []*bloomshipper.CloseableBlockQuerier

	blocksAsPeekingIter []v1.PeekingIterator[*v1.SeriesWithBloom]
	curr                *v1.Block
	err                 error
}

func NewLazyBlockBuilderIterator(
	ctx context.Context,
	opts v1.BlockOptions,
	populate func(*v1.Series, *v1.Bloom) error,
	readWriterFn func() (v1.BlockWriter, v1.BlockReader),
	series v1.PeekingIterator[*v1.Series],
	blocks []*bloomshipper.CloseableBlockQuerier,
) *LazyBlockBuilderIterator {
	it := &LazyBlockBuilderIterator{
		ctx:          ctx,
		opts:         opts,
		populate:     populate,
		readWriterFn: readWriterFn,
		series:       series,
		blocks:       blocks,

		blocksAsPeekingIter: make([]v1.PeekingIterator[*v1.SeriesWithBloom], len(blocks)),
	}

	return it
}

func (b *LazyBlockBuilderIterator) Next() bool {
	// No more series to process
	if _, hasNext := b.series.Peek(); !hasNext {
		return false
	}

	// reset all the blocks to the start
	for i, block := range b.blocks {
		if err := block.Reset(); err != nil {
			b.err = errors.Wrapf(err, "failed to reset block iterator %d", i)
			return false
		}
		b.blocksAsPeekingIter[i] = v1.NewPeekingIter[*v1.SeriesWithBloom](block)
	}

	if err := b.ctx.Err(); err != nil {
		b.err = errors.Wrap(err, "context canceled")
		return false
	}

	mergeBuilder := v1.NewMergeBuilder(b.blocksAsPeekingIter, b.series, b.populate)
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

type Fetcher[A, B any] interface {
	Fetch(ctx context.Context, inputs []A) ([]B, error)
}

type FetchFunc[A, B any] func(ctx context.Context, inputs []A) ([]B, error)

func (f FetchFunc[A, B]) Fetch(ctx context.Context, inputs []A) ([]B, error) {
	return f(ctx, inputs)
}

// batchedLoader implements `v1.Iterator[v1.ChunkRefWithIter]` in batches
// to ensure memory is bounded while loading chunks
// TODO(owen-d): testware
type batchedLoader[A, B, C any] struct {
	metrics   *Metrics
	batchSize int
	ctx       context.Context
	fetchers  []Fetcher[A, B]
	work      [][]A

	mapper func(B) (C, error)
	cur    C
	batch  []B
	err    error
}

const batchedLoaderDefaultBatchSize = 50

func newBatchedLoader[A, B, C any](
	ctx context.Context,
	fetchers []Fetcher[A, B],
	inputs [][]A,
	mapper func(B) (C, error),
	batchSize int,
) *batchedLoader[A, B, C] {
	return &batchedLoader[A, B, C]{
		batchSize: max(batchSize, 1),
		ctx:       ctx,
		fetchers:  fetchers,
		work:      inputs,
		mapper:    mapper,
	}
}

func (b *batchedLoader[A, B, C]) Next() bool {

	// iterate work until we have non-zero length batch
	for len(b.batch) == 0 {

		// empty batch + no work remaining = we're done
		if len(b.work) == 0 {
			return false
		}

		// setup next batch
		next := b.work[0]
		batchSize := min(b.batchSize, len(next))
		toFetch := next[:batchSize]
		fetcher := b.fetchers[0]

		// update work
		b.work[0] = b.work[0][batchSize:]
		if len(b.work[0]) == 0 {
			// if we've exhausted work from this set of inputs,
			// set pointer to next set of inputs
			// and their respective fetcher
			b.work = b.work[1:]
			b.fetchers = b.fetchers[1:]
		}

		// there was no work in this batch; continue (should not happen)
		if len(toFetch) == 0 {
			continue
		}

		b.batch, b.err = fetcher.Fetch(b.ctx, toFetch)
		// error fetching, short-circuit iteration
		if b.err != nil {
			return false
		}
	}

	return b.prepNext()
}

func (b *batchedLoader[_, B, C]) prepNext() bool {
	b.cur, b.err = b.mapper(b.batch[0])
	b.batch = b.batch[1:]
	return b.err == nil
}

func newBatchedChunkLoader(
	ctx context.Context,
	fetchers []Fetcher[chunk.Chunk, chunk.Chunk],
	inputs [][]chunk.Chunk,
	metrics *Metrics,
	batchSize int,
) *batchedLoader[chunk.Chunk, chunk.Chunk, v1.ChunkRefWithIter] {

	mapper := func(c chunk.Chunk) (v1.ChunkRefWithIter, error) {
		chk := c.Data.(*chunkenc.Facade).LokiChunk()
		metrics.chunkSize.Observe(float64(chk.UncompressedSize()))
		itr, err := chk.Iterator(
			ctx,
			time.Unix(0, 0),
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
	return newBatchedLoader(ctx, fetchers, inputs, mapper, batchSize)
}

func (b *batchedLoader[_, _, C]) At() C {
	return b.cur
}

func (b *batchedLoader[_, _, _]) Err() error {
	return b.err
}
