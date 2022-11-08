package stores

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/test"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

type mockStore int

func (m mockStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	return nil
}

func (m mockStore) PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error {
	return nil
}

func (m mockStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (m mockStore) SetChunkFilterer(f chunk.RequestChunkFilterer) {}

func (m mockStore) GetChunkRefs(tx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	return nil, nil, nil
}

func (m mockStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	return nil, nil
}

func (m mockStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	return nil, nil
}

func (m mockStore) GetChunkFetcher(tm model.Time) *fetcher.Fetcher {
	return nil
}

func (m mockStore) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	return nil, nil
}

func (m mockStore) Stop() {}

func TestCompositeStore(t *testing.T) {
	type result struct {
		from, through model.Time
		store         Store
	}
	collect := func(results *[]result) func(_ context.Context, from, through model.Time, store Store) error {
		return func(_ context.Context, from, through model.Time, store Store) error {
			*results = append(*results, result{from, through, store.(compositeStoreEntry).Store})
			return nil
		}
	}
	cs := compositeStore{
		stores: []compositeStoreEntry{
			{model.TimeFromUnix(0), mockStore(1)},
			{model.TimeFromUnix(100), mockStore(2)},
			{model.TimeFromUnix(200), mockStore(3)},
		},
	}

	for i, tc := range []struct {
		cs            compositeStore
		from, through int64
		want          []result
	}{
		// Test we have sensible results when there are no schema's defined
		{compositeStore{}, 0, 1, []result{}},

		// Test we have sensible results when there is a single schema
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			0, 10,
			[]result{
				{model.TimeFromUnix(0), model.TimeFromUnix(10), mockStore(1)},
			},
		},

		// Test we have sensible results for negative (ie pre 1970) times
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			-10, -9,
			[]result{},
		},
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			-10, 10,
			[]result{
				{model.TimeFromUnix(0), model.TimeFromUnix(10), mockStore(1)},
			},
		},

		// Test we have sensible results when there is two schemas
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
					{model.TimeFromUnix(100), mockStore(2)},
				},
			},
			34, 165,
			[]result{
				{model.TimeFromUnix(34), model.TimeFromUnix(100) - 1, mockStore(1)},
				{model.TimeFromUnix(100), model.TimeFromUnix(165), mockStore(2)},
			},
		},

		// Test all the various combination we can get when there are three schemas
		{
			cs, 34, 65,
			[]result{
				{model.TimeFromUnix(34), model.TimeFromUnix(65), mockStore(1)},
			},
		},

		{
			cs, 244, 6785,
			[]result{
				{model.TimeFromUnix(244), model.TimeFromUnix(6785), mockStore(3)},
			},
		},

		{
			cs, 34, 165,
			[]result{
				{model.TimeFromUnix(34), model.TimeFromUnix(100) - 1, mockStore(1)},
				{model.TimeFromUnix(100), model.TimeFromUnix(165), mockStore(2)},
			},
		},

		{
			cs, 151, 264,
			[]result{
				{model.TimeFromUnix(151), model.TimeFromUnix(200) - 1, mockStore(2)},
				{model.TimeFromUnix(200), model.TimeFromUnix(264), mockStore(3)},
			},
		},

		{
			cs, 32, 264,
			[]result{
				{model.TimeFromUnix(32), model.TimeFromUnix(100) - 1, mockStore(1)},
				{model.TimeFromUnix(100), model.TimeFromUnix(200) - 1, mockStore(2)},
				{model.TimeFromUnix(200), model.TimeFromUnix(264), mockStore(3)},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			have := []result{}
			err := tc.cs.forStores(context.Background(), model.TimeFromUnix(tc.from), model.TimeFromUnix(tc.through), collect(&have))
			require.NoError(t, err)
			if !reflect.DeepEqual(tc.want, have) {
				t.Fatalf("wrong stores - %s", test.Diff(tc.want, have))
			}
		})
	}
}

type mockStoreLabel struct {
	mockStore
	values []string
}

func (m mockStoreLabel) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	return m.values, nil
}

func (m mockStoreLabel) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	return m.values, nil
}

func TestCompositeStoreLabels(t *testing.T) {
	t.Parallel()

	cs := compositeStore{
		stores: []compositeStoreEntry{
			{model.TimeFromUnix(0), mockStore(1)},
			{model.TimeFromUnix(20), mockStoreLabel{mockStore(1), []string{"b", "c", "e"}}},
			{model.TimeFromUnix(40), mockStoreLabel{mockStore(1), []string{"a", "b", "c", "f"}}},
		},
	}

	for i, tc := range []struct {
		from, through int64
		want          []string
	}{
		{
			0, 10,
			nil,
		},
		{
			0, 30,
			[]string{"b", "c", "e"},
		},
		{
			0, 40,
			[]string{"a", "b", "c", "e", "f"},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			have, err := cs.LabelNamesForMetricName(context.Background(), "", model.TimeFromUnix(tc.from), model.TimeFromUnix(tc.through), "")
			require.NoError(t, err)
			if !reflect.DeepEqual(tc.want, have) {
				t.Fatalf("wrong label names - %s", test.Diff(tc.want, have))
			}
			have, err = cs.LabelValuesForMetricName(context.Background(), "", model.TimeFromUnix(tc.from), model.TimeFromUnix(tc.through), "", "")
			require.NoError(t, err)
			if !reflect.DeepEqual(tc.want, have) {
				t.Fatalf("wrong label values - %s", test.Diff(tc.want, have))
			}
		})
	}
}

type mockStoreGetChunkFetcher struct {
	mockStore
	chunkFetcher *fetcher.Fetcher
}

func (m mockStoreGetChunkFetcher) GetChunkFetcher(tm model.Time) *fetcher.Fetcher {
	return m.chunkFetcher
}

func TestCompositeStore_GetChunkFetcher(t *testing.T) {
	cs := compositeStore{
		stores: []compositeStoreEntry{
			{model.TimeFromUnix(10), mockStoreGetChunkFetcher{mockStore(0), &fetcher.Fetcher{}}},
			{model.TimeFromUnix(20), mockStoreGetChunkFetcher{mockStore(1), &fetcher.Fetcher{}}},
		},
	}

	for _, tc := range []struct {
		name            string
		tm              model.Time
		expectedFetcher *fetcher.Fetcher
	}{
		{
			name: "no matching store",
			tm:   model.TimeFromUnix(0),
		},
		{
			name:            "first store",
			tm:              model.TimeFromUnix(10),
			expectedFetcher: cs.stores[0].Store.(mockStoreGetChunkFetcher).chunkFetcher,
		},
		{
			name:            "still first store",
			tm:              model.TimeFromUnix(11),
			expectedFetcher: cs.stores[0].Store.(mockStoreGetChunkFetcher).chunkFetcher,
		},
		{
			name:            "second store",
			tm:              model.TimeFromUnix(20),
			expectedFetcher: cs.stores[1].Store.(mockStoreGetChunkFetcher).chunkFetcher,
		},
		{
			name:            "still second store",
			tm:              model.TimeFromUnix(21),
			expectedFetcher: cs.stores[1].Store.(mockStoreGetChunkFetcher).chunkFetcher,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Same(t, tc.expectedFetcher, cs.GetChunkFetcher(tc.tm))
		})
	}
}
