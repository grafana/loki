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
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	logql_log "github.com/grafana/loki/pkg/logql/log"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
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

// IndexLoader loads an index. This helps us do things like
// load TSDBs for a specific period excluding multitenant (pre-compacted) indices
type indexLoader interface {
	Index() (tsdb.Index, error)
}

// chunkData models a single chunk
type chunkData struct {
	ref v1.ChunkRef
	itr iter.EntryIterator
}

// chunkItersByFingerprint models the chunks belonging to a fingerprint
type chunkItersByFingerprint struct {
	fp  model.Fingerprint
	itr v1.Iterator[chunkData]
}

// chunkLoader loads chunks from a store
type chunkLoader interface {
	Load(context.Context, *ChunkRefsByFingerprint) (*chunkItersByFingerprint, error)
}

type IndexManager struct {
	userID      string
	indexLoader indexLoader
	chunkLoader chunkLoader
}

func NewIndexManager(userID string, indexLoader indexLoader, chunkLoader chunkLoader) *IndexManager {
	return &IndexManager{
		userID:      userID,
		indexLoader: indexLoader,
		chunkLoader: chunkLoader,
	}
}

// ChunkRefsByFingerprint is a single series in the index
type ChunkRefsByFingerprint struct {
	fp     model.Fingerprint
	Chunks []logproto.ChunkRef
}

func (i *IndexManager) Iter(ctx context.Context) (*ChunkLoaderIter, error) {
	idx, err := i.indexLoader.Index()
	if err != nil {
		return nil, err
	}

	everything := labels.MustNewMatcher(labels.MatchEqual, "", "")
	refs, err := idx.GetChunkRefs(ctx, i.userID, 0, math.MaxInt64, nil, nil, everything)
	if err != nil {
		return nil, err
	}

	chkRefItr := newChunksByFpIterator(refs)
	return NewChunkLoaderIter(ctx, i.chunkLoader, chkRefItr), nil
}

// ChunksByFpIter consolidates a ChunkRef iterator into an ChunksByFp iterator
// by grouping chunks by fingerprint. This can group multiple series with the same fp together,
// but this is accepted (correctness will not be affected).
func newChunksByFpIterator(chks []tsdb.ChunkRef) *v1.DedupeIter[tsdb.ChunkRef, *ChunkRefsByFingerprint] {
	itr := v1.NewPeekingIter[tsdb.ChunkRef](v1.NewSliceIter[tsdb.ChunkRef](chks))

	eq := func(a tsdb.ChunkRef, b *ChunkRefsByFingerprint) bool {
		return a.Fingerprint == b.fp
	}

	from := func(a tsdb.ChunkRef) *ChunkRefsByFingerprint {
		return &ChunkRefsByFingerprint{
			fp: a.Fingerprint,
			// TODO(owen-d): pool?
			Chunks: []logproto.ChunkRef{a.LogProto()},
		}
	}

	merge := func(a tsdb.ChunkRef, b *ChunkRefsByFingerprint) *ChunkRefsByFingerprint {
		b.Chunks = append(b.Chunks, a.LogProto())
		return b
	}

	return v1.NewDedupingIter[tsdb.ChunkRef, *ChunkRefsByFingerprint](
		eq,
		from,
		merge,
		itr,
	)
}

// ChunkLoaderIter implements v1.PeekingIterator[chunkItersByFingerprint]
// via a chunkLoader
type ChunkLoaderIter struct {
	ctx    context.Context
	loader chunkLoader
	src    v1.Iterator[*ChunkRefsByFingerprint]

	cur *chunkItersByFingerprint
	err error
}

func NewChunkLoaderIter(ctx context.Context, loader chunkLoader, src v1.Iterator[*ChunkRefsByFingerprint]) *ChunkLoaderIter {
	return &ChunkLoaderIter{
		ctx:    ctx,
		loader: loader,
		src:    src,
	}
}

func (c *ChunkLoaderIter) Next() bool {
	if err := c.ctx.Err(); err != nil {
		c.err = err
		return false
	}

	if !c.src.Next() {
		return false
	}

	chunkRefs := c.src.At()
	chunkIters, err := c.loader.Load(context.Background(), chunkRefs)
	if err != nil {
		c.err = err
		return false
	}

	c.cur = chunkIters
	return true
}

func (c *ChunkLoaderIter) At() *chunkItersByFingerprint {
	return c.cur
}

func (c *ChunkLoaderIter) Err() error {
	return c.err
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
	fetcherProvider fetcherProvider
}

func (s *StoreChunkLoader) Load(ctx context.Context, grp *ChunkRefsByFingerprint) (*chunkItersByFingerprint, error) {
	// TODO(owen-d): This is probalby unnecessary as we should only have one fetcher
	// because we'll only be working on a single index period at a time, but this should protect
	// us in the case of refactoring/changing this and likely isn't a perf bottleneck.
	chksByFetcher := make(map[chunkFetcher][]chunk.Chunk)
	for _, chk := range grp.Chunks {
		fetcher := s.fetcherProvider.GetChunkFetcher(chk.From)
		chksByFetcher[fetcher] = append(chksByFetcher[fetcher], chunk.Chunk{
			ChunkRef: chk,
		})
	}

	work := make([]chunkWork, 0, len(chksByFetcher))
	for fetcher, chks := range chksByFetcher {
		work = append(work, chunkWork{
			fetcher: fetcher,
			chks:    chks,
		})
	}

	return &chunkItersByFingerprint{
		fp:  grp.fp,
		itr: newBatchedLoader(ctx, work, batchedLoaderDefaultBatchSize),
	}, nil
}

type chunkWork struct {
	fetcher chunkFetcher
	chks    []chunk.Chunk
}

// batchedLoader implements `v1.Iterator[chunkData]` in batches
// to ensure memory is bounded while loading chunks
// TODO(owen-d): testware
type batchedLoader struct {
	batchSize int
	ctx       context.Context
	work      []chunkWork

	cur   chunkData
	batch []chunk.Chunk
	err   error
}

const batchedLoaderDefaultBatchSize = 50

func newBatchedLoader(ctx context.Context, work []chunkWork, batchSize int) *batchedLoader {
	return &batchedLoader{
		batchSize: batchSize,
		ctx:       ctx,
		work:      work,
	}
}

func (b *batchedLoader) Next() bool {
	if len(b.batch) > 0 {
		b.cur, b.err = b.format(b.batch[0])
		b.batch = b.batch[1:]
		if b.err != nil {
			return false
		}
		return true
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
	if b.err != nil {
		return false
	}

	return true
}

func (b *batchedLoader) format(c chunk.Chunk) (chunkData, error) {
	chk := c.Data.(*chunkenc.Facade).LokiChunk()
	itr, err := chk.Iterator(
		b.ctx,
		time.Unix(0, 0), // TODO: Parameterize/better handle the timestamps?
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		logql_log.NewNoopPipeline().ForStream(c.Metric),
	)

	if err != nil {
		return chunkData{}, err
	}

	return chunkData{
		ref: v1.ChunkRef{
			Start:    c.From,
			End:      c.Through,
			Checksum: c.Checksum,
		},
		itr: itr,
	}, nil
}

func (b *batchedLoader) At() chunkData {
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
