package bloomcompactor

import (
	"context"

	"github.com/grafana/dskit/multierror"

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
