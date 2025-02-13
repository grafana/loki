package builder

import (
	"context"
	"io"
	"math"
	"slices"
	"time"

	"github.com/grafana/dskit/multierror"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type Fetcher[A, B any] interface {
	Fetch(ctx context.Context, inputs []A) ([]B, error)
}

type FetchFunc[A, B any] func(ctx context.Context, inputs []A) ([]B, error)

func (f FetchFunc[A, B]) Fetch(ctx context.Context, inputs []A) ([]B, error) {
	return f(ctx, inputs)
}

// batchedLoader implements `v1.Iterator[C]` in batches
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

func (b *batchedLoader[_, _, C]) At() C {
	return b.cur
}

func (b *batchedLoader[_, _, _]) Err() error {
	return b.err
}

// to ensure memory is bounded while loading chunks
// TODO(owen-d): testware
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
			logql_log.NewNoopPipeline().ForStream(nil),
		)

		if err != nil {
			return v1.ChunkRefWithIter{}, err
		}

		return v1.ChunkRefWithIter{
			Ref: v1.ChunkRef{
				From:     c.From,
				Through:  c.Through,
				Checksum: c.Checksum,
			},
			Itr: itr,
		}, nil
	}
	return newBatchedLoader(ctx, fetchers, inputs, mapper, batchSize)
}

func newBatchedBlockLoader(
	ctx context.Context,
	fetcher Fetcher[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier],
	blocks []bloomshipper.BlockRef,
	batchSize int,
) *batchedLoader[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier, *bloomshipper.CloseableBlockQuerier] {

	fetchers := []Fetcher[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier]{fetcher}
	inputs := [][]bloomshipper.BlockRef{blocks}
	mapper := func(a *bloomshipper.CloseableBlockQuerier) (*bloomshipper.CloseableBlockQuerier, error) {
		return a, nil
	}

	return newBatchedLoader(ctx, fetchers, inputs, mapper, batchSize)
}

// compiler checks
var _ iter.Iterator[*v1.SeriesWithBlooms] = &blockLoadingIter{}
var _ iter.CloseIterator[*v1.SeriesWithBlooms] = &blockLoadingIter{}
var _ iter.ResetIterator[*v1.SeriesWithBlooms] = &blockLoadingIter{}

// TODO(chaudum): testware
func newBlockLoadingIter(ctx context.Context, blocks []bloomshipper.BlockRef, fetcher FetchFunc[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier], batchSize int) *blockLoadingIter {

	return &blockLoadingIter{
		ctx:       ctx,
		fetcher:   fetcher,
		inputs:    blocks,
		batchSize: batchSize,
		loaded:    make(map[io.Closer]struct{}),
	}
}

type blockLoadingIter struct {
	// constructor arguments
	ctx         context.Context
	fetcher     Fetcher[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier]
	inputs      []bloomshipper.BlockRef
	overlapping iter.Iterator[[]bloomshipper.BlockRef]
	batchSize   int
	// optional arguments
	filter func(*bloomshipper.CloseableBlockQuerier) bool
	// internals
	initialized bool
	err         error
	iter        iter.Iterator[*v1.SeriesWithBlooms]
	loader      *batchedLoader[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier, *bloomshipper.CloseableBlockQuerier]
	loaded      map[io.Closer]struct{}
}

// At implements v1.Iterator.
func (i *blockLoadingIter) At() *v1.SeriesWithBlooms {
	if !i.initialized {
		panic("iterator not initialized")
	}
	return i.iter.At()
}

// Err implements v1.Iterator.
func (i *blockLoadingIter) Err() error {
	if !i.initialized {
		panic("iterator not initialized")
	}
	if i.err != nil {
		return i.err
	}
	return i.iter.Err()
}

func (i *blockLoadingIter) init() {
	if i.initialized {
		return
	}

	// group overlapping blocks
	i.overlapping = overlappingBlocksIter(i.inputs)

	// set initial iter
	i.iter = iter.NewEmptyIter[*v1.SeriesWithBlooms]()

	// set "match all" filter function if not present
	if i.filter == nil {
		i.filter = func(_ *bloomshipper.CloseableBlockQuerier) bool { return true }
	}

	// done
	i.initialized = true
}

// load next populates the underlying iter via relevant batches
// and returns the result of iter.Next()
func (i *blockLoadingIter) loadNext() bool {
	for i.overlapping.Next() {
		blockRefs := i.overlapping.At()

		loader := newBatchedBlockLoader(i.ctx, i.fetcher, blockRefs, i.batchSize)
		filtered := iter.NewFilterIter(loader, i.filter)

		iters := make([]iter.PeekIterator[*v1.SeriesWithBlooms], 0, len(blockRefs))
		for filtered.Next() {
			bq := filtered.At()
			i.loaded[bq] = struct{}{}
			itr, err := bq.SeriesIter()
			if err != nil {
				i.err = err
				i.iter = iter.NewEmptyIter[*v1.SeriesWithBlooms]()
				return false
			}
			iters = append(iters, itr)
		}

		if err := filtered.Err(); err != nil {
			i.err = err
			i.iter = iter.NewEmptyIter[*v1.SeriesWithBlooms]()
			return false
		}

		// edge case: we've filtered out all blocks in the batch; check next batch
		if len(iters) == 0 {
			continue
		}

		// Turn the list of blocks into a single iterator that returns the next series
		mergedBlocks := v1.NewHeapIterForSeriesWithBloom(iters...)
		// two overlapping blocks can conceivably have the same series, so we need to dedupe,
		// preferring the one with the most chunks already indexed since we'll have
		// to add fewer chunks to the bloom
		i.iter = iter.NewDedupingIter(
			func(a, b *v1.SeriesWithBlooms) bool {
				return a.Series.Fingerprint == b.Series.Fingerprint
			},
			iter.Identity[*v1.SeriesWithBlooms],
			func(a, b *v1.SeriesWithBlooms) *v1.SeriesWithBlooms {
				if len(a.Series.Chunks) > len(b.Series.Chunks) {
					return a
				}
				return b
			},
			iter.NewPeekIter(mergedBlocks),
		)
		return i.iter.Next()
	}

	i.iter = iter.NewEmptyIter[*v1.SeriesWithBlooms]()
	i.err = i.overlapping.Err()
	return false
}

// Next implements v1.Iterator.
func (i *blockLoadingIter) Next() bool {
	i.init()

	if i.ctx.Err() != nil {
		i.err = i.ctx.Err()
		return false
	}

	return i.iter.Next() || i.loadNext()
}

// Close implements v1.CloseableIterator.
func (i *blockLoadingIter) Close() error {
	var err multierror.MultiError
	for k := range i.loaded {
		err.Add(k.Close())
	}
	return err.Err()
}

// Reset implements v1.ResettableIterator.
// TODO(chaudum) Cache already fetched blocks to to avoid the overhead of
// creating the reader.
func (i *blockLoadingIter) Reset() error {
	if !i.initialized {
		return nil
	}
	// close loaded queriers
	err := i.Close()
	i.initialized = false
	clear(i.loaded)
	return err
}

func (i *blockLoadingIter) Filter(filter func(*bloomshipper.CloseableBlockQuerier) bool) {
	if i.initialized {
		panic("iterator already initialized")
	}
	i.filter = filter
}

func overlappingBlocksIter(inputs []bloomshipper.BlockRef) iter.Iterator[[]bloomshipper.BlockRef] {
	// can we assume sorted blocks?
	peekIter := iter.NewPeekIter(iter.NewSliceIter(inputs))

	return iter.NewDedupingIter(
		func(a bloomshipper.BlockRef, b []bloomshipper.BlockRef) bool {
			minFp := b[0].Bounds.Min
			maxFp := slices.MaxFunc(b, func(a, b bloomshipper.BlockRef) int { return int(a.Bounds.Max - b.Bounds.Max) }).Bounds.Max
			return a.Bounds.Overlaps(v1.NewBounds(minFp, maxFp))
		},
		func(a bloomshipper.BlockRef) []bloomshipper.BlockRef {
			return []bloomshipper.BlockRef{a}
		},
		func(a bloomshipper.BlockRef, b []bloomshipper.BlockRef) []bloomshipper.BlockRef {
			return append(b, a)
		},
		peekIter,
	)
}
