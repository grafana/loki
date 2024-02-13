package bloomcompactor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

type dummyBlocksFetcher struct {
	count *atomic.Int32
}

func (f *dummyBlocksFetcher) FetchBlocks(_ context.Context, blocks []bloomshipper.BlockRef) ([]*bloomshipper.CloseableBlockQuerier, error) {
	f.count.Inc()
	return make([]*bloomshipper.CloseableBlockQuerier, len(blocks)), nil
}

func TestBatchedBlockLoader(t *testing.T) {
	ctx := context.Background()
	f := &dummyBlocksFetcher{count: atomic.NewInt32(0)}

	blocks := make([]bloomshipper.BlockRef, 25)
	blocksIter, err := newBatchedBlockLoader(ctx, f, blocks)
	require.NoError(t, err)

	var count int
	for blocksIter.Next() && blocksIter.Err() == nil {
		count++
	}

	require.Equal(t, len(blocks), count)
	require.Equal(t, int32(len(blocks)/blocksIter.batchSize+1), f.count.Load())
}
