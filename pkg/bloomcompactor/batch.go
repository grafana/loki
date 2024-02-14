package bloomcompactor

import (
	"context"
	"math"
	"time"

	"github.com/grafana/dskit/multierror"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	logql_log "github.com/grafana/loki/pkg/logql/log"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

// interface modeled from `pkg/storage/stores/shipper/bloomshipper.Fetcher`
type blocksFetcher interface {
	FetchBlocks(context.Context, []bloomshipper.BlockRef) ([]*bloomshipper.CloseableBlockQuerier, error)
}

func newBatchedBlockLoader(ctx context.Context, fetcher blocksFetcher, blocks []bloomshipper.BlockRef) (*batchedBlockLoader, error) {
	return &batchedBlockLoader{
		ctx:       ctx,
		batchSize: 10, // make configurable?
		source:    blocks,
		fetcher:   fetcher,
	}, nil
}

type batchedBlockLoader struct {
	ctx       context.Context
	batchSize int

	source  []bloomshipper.BlockRef
	fetcher blocksFetcher

	batch []*bloomshipper.CloseableBlockQuerier
	cur   *bloomshipper.CloseableBlockQuerier
	err   error
}

// At implements v1.CloseableIterator.
func (b *batchedBlockLoader) At() *bloomshipper.CloseableBlockQuerier {
	return b.cur
}

// Close implements v1.CloseableIterator.
func (b *batchedBlockLoader) Close() error {
	if b.cur != nil {
		return b.cur.Close()
	}
	return nil
}

// CloseBatch closes the remaining items from the current batch
func (b *batchedBlockLoader) CloseBatch() error {
	var err multierror.MultiError
	for _, cur := range b.batch {
		err.Add(cur.Close())
	}
	if len(b.batch) > 0 {
		b.batch = b.batch[:0]
	}
	return err.Err()
}

// Err implements v1.CloseableIterator.
func (b *batchedBlockLoader) Err() error {
	return b.err
}

// Next implements v1.CloseableIterator.
func (b *batchedBlockLoader) Next() bool {
	if len(b.batch) > 0 {
		return b.setNext()
	}

	if len(b.source) == 0 {
		return false
	}

	// setup next batch
	batchSize := min(b.batchSize, len(b.source))
	toFetch := b.source[:batchSize]

	// update source
	b.source = b.source[batchSize:]

	b.batch, b.err = b.fetcher.FetchBlocks(b.ctx, toFetch)
	if b.err != nil {
		return false
	}
	return b.setNext()
}

func (b *batchedBlockLoader) setNext() bool {
	b.cur, b.err = b.batch[0], nil
	b.batch = b.batch[1:]
	return true
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
