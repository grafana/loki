package bloomgateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/util/constants"
)

var _ bloomshipper.Store = &dummyStore{}

func newMockBloomStore(bqs []*bloomshipper.CloseableBlockQuerier, metas []bloomshipper.Meta) *dummyStore {
	return &dummyStore{
		querieres: bqs,
		metas:     metas,
	}
}

type dummyStore struct {
	metas     []bloomshipper.Meta
	querieres []*bloomshipper.CloseableBlockQuerier

	// mock how long it takes to serve block queriers
	delay time.Duration
	// mock response error when serving block queriers in ForEach
	err error
}

func (s *dummyStore) ResolveMetas(_ context.Context, _ bloomshipper.MetaSearchParams) ([][]bloomshipper.MetaRef, []*bloomshipper.Fetcher, error) {
	time.Sleep(s.delay)

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

func (s *dummyStore) TenantFilesForInterval(_ context.Context, _ bloomshipper.Interval, _ func(tenant string, object client.StorageObject) bool) (map[string][]client.StorageObject, error) {
	return nil, nil
}

func (s *dummyStore) Fetcher(_ model.Time) (*bloomshipper.Fetcher, error) {
	return nil, nil
}

func (s *dummyStore) Client(_ model.Time) (bloomshipper.Client, error) {
	return nil, nil
}

func (s *dummyStore) Stop() {
}

func (s *dummyStore) FetchBlocks(_ context.Context, refs []bloomshipper.BlockRef, _ ...bloomshipper.FetchOption) ([]*bloomshipper.CloseableBlockQuerier, error) {
	result := make([]*bloomshipper.CloseableBlockQuerier, 0, len(s.querieres))

	if s.err != nil {
		time.Sleep(s.delay)
		return result, s.err
	}

	for _, ref := range refs {
		for _, bq := range s.querieres {
			if ref.Bounds.Equal(bq.Bounds) {
				result = append(result, bq)
			}
		}
	}

	time.Sleep(s.delay)

	return result, nil
}

func TestProcessor(t *testing.T) {
	ctx := context.Background()
	tenant := "fake"
	now := mktime("2024-01-27 12:00")
	metrics := newWorkerMetrics(prometheus.NewPedanticRegistry(), constants.Loki, "bloom_gatway")

	t.Run("success case", func(t *testing.T) {
		_, metas, queriers, data := createBlocks(t, tenant, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)

		mockStore := newMockBloomStore(queriers, metas)
		p := newProcessor("worker", mockStore, log.NewNopLogger(), metrics)

		chunkRefs := createQueryInputFromBlockData(t, tenant, data, 10)
		swb := seriesWithInterval{
			series: groupRefs(t, chunkRefs),
			interval: bloomshipper.Interval{
				Start: now.Add(-1 * time.Hour),
				End:   now,
			},
			day: config.NewDayTime(truncateDay(now)),
		}
		filters := []syntax.LineFilterExpr{
			{
				LineFilter: syntax.LineFilter{
					Ty:    0,
					Match: "no match",
				},
			},
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

	t.Run("failure case", func(t *testing.T) {
		_, metas, queriers, data := createBlocks(t, tenant, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)

		mockStore := newMockBloomStore(queriers, metas)
		mockStore.err = errors.New("store failed")

		p := newProcessor("worker", mockStore, log.NewNopLogger(), metrics)

		chunkRefs := createQueryInputFromBlockData(t, tenant, data, 10)
		swb := seriesWithInterval{
			series: groupRefs(t, chunkRefs),
			interval: bloomshipper.Interval{
				Start: now.Add(-1 * time.Hour),
				End:   now,
			},
			day: config.NewDayTime(truncateDay(now)),
		}
		filters := []syntax.LineFilterExpr{
			{
				LineFilter: syntax.LineFilter{
					Ty:    0,
					Match: "no match",
				},
			},
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
		require.Errorf(t, err, "store failed")
		require.Equal(t, int64(0), results.Load())
	})
}
