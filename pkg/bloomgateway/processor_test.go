package bloomgateway

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logql/syntax"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

var _ store = &dummyStore{}

type dummyStore struct {
	metas     []bloomshipper.Meta
	blocks    []bloomshipper.BlockRef
	querieres []bloomshipper.BlockQuerierWithFingerprintRange
}

func (s *dummyStore) ResolveMetas(_ context.Context, _ bloomshipper.MetaSearchParams) ([][]bloomshipper.MetaRef, []*bloomshipper.Fetcher, error) {
	//TODO(chaudum) Filter metas based on search params
	refs := make([]bloomshipper.MetaRef, 0, len(s.metas))
	for _, meta := range s.metas {
		refs = append(refs, meta.MetaRef)
	}
	return [][]bloomshipper.MetaRef{refs}, []*bloomshipper.Fetcher{nil}, nil
}

func (s *dummyStore) FetchMetas(_ context.Context, _ bloomshipper.MetaSearchParams) ([]bloomshipper.Meta, error) {
	//TODO(chaudum) Filter metas based on search params
	return s.metas, nil
}

func (s *dummyStore) Fetcher(_ model.Time) *bloomshipper.Fetcher {
	return nil
}

func (s *dummyStore) Stop() {
}

func (s *dummyStore) LoadBlocks(_ context.Context, refs []bloomshipper.BlockRef) (v1.Iterator[bloomshipper.BlockQuerierWithFingerprintRange], error) {
	result := make([]bloomshipper.BlockQuerierWithFingerprintRange, len(s.querieres))

	for _, ref := range refs {
		for _, bq := range s.querieres {
			if ref.Bounds().Equal(bq.FingerprintBounds) {
				result = append(result, bq)
			}
		}
	}

	rand.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})

	return v1.NewSliceIter(result), nil
}

func TestProcessor(t *testing.T) {
	ctx := context.Background()
	tenant := "fake"
	now := mktime("2024-01-27 12:00")

	t.Run("dummy", func(t *testing.T) {
		blocks, metas, queriers, data := createBlocks(t, tenant, 10, now.Add(-1*time.Hour), now, 0x0000, 0x1000)
		p := &processor{
			store: &dummyStore{
				querieres: queriers,
				metas:     metas,
				blocks:    blocks,
			},
		}

		chunkRefs := createQueryInputFromBlockData(t, tenant, data, 10)
		swb := seriesWithBounds{
			series: groupRefs(t, chunkRefs),
			bounds: model.Interval{
				Start: now.Add(-1 * time.Hour),
				End:   now,
			},
			day: truncateDay(now),
		}
		filters := []syntax.LineFilter{
			{Ty: 0, Match: "no match"},
		}

		t.Log("series", len(swb.series))
		task, _ := NewTask(ctx, "fake", swb, filters)
		tasks := []Task{task}

		results := atomic.NewInt64(0)
		var wg sync.WaitGroup
		for i := range tasks {
			wg.Add(1)
			go func(ta Task) {
				defer wg.Done()
				for range ta.resCh {
					results.Inc()
				}
				t.Log("done", results.Load())
			}(tasks[i])
		}

		err := p.run(ctx, tasks)
		wg.Wait()
		require.NoError(t, err)
		require.Equal(t, int64(len(swb.series)), results.Load())
	})
}
