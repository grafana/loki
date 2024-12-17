package stores

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/dskit/test"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

type mockStore int

func (m mockStore) Put(_ context.Context, _ []chunk.Chunk) error {
	return nil
}

func (m mockStore) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return nil
}

func (m mockStore) LabelValuesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (m mockStore) SetChunkFilterer(_ chunk.RequestChunkFilterer) {}

func (m mockStore) GetChunks(_ context.Context, _ string, _, _ model.Time, _ chunk.Predicate, _ *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	return nil, nil, nil
}

func (m mockStore) GetSeries(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) ([]labels.Labels, error) {
	return nil, nil
}

func (m mockStore) LabelNamesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (m mockStore) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return nil
}

func (m mockStore) Stats(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return nil, nil
}

func (m mockStore) Volume(_ context.Context, _ string, _, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return nil, nil
}

func (m mockStore) GetShards(_ context.Context, _ string, _, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return nil, nil
}

func (m mockStore) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
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
	cs := CompositeStore{
		stores: []compositeStoreEntry{
			{model.TimeFromUnix(0), mockStore(1)},
			{model.TimeFromUnix(100), mockStore(2)},
			{model.TimeFromUnix(200), mockStore(3)},
		},
	}

	for i, tc := range []struct {
		cs            CompositeStore
		from, through int64
		want          []result
	}{
		// Test we have sensible results when there are no schema's defined
		{
			CompositeStore{}, 0, 1, []result{},
		},

		// Test we have sensible results when there is a single schema
		{
			CompositeStore{
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
			CompositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			-10, -9,
			[]result{},
		},
		{
			CompositeStore{
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
			CompositeStore{
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

func (m mockStoreLabel) LabelValuesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return m.values, nil
}

func (m mockStoreLabel) LabelNamesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return m.values, nil
}

func TestCompositeStoreLabels(t *testing.T) {
	t.Parallel()

	cs := CompositeStore{
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

func (m mockStoreGetChunkFetcher) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return m.chunkFetcher
}

func TestCompositeStore_GetChunkFetcher(t *testing.T) {
	cs := CompositeStore{
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

type mockStoreVolume struct {
	mockStore
	value *logproto.VolumeResponse
	err   error
}

func (m mockStoreVolume) Volume(_ context.Context, _ string, _, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return m.value, m.err
}

func TestVolume(t *testing.T) {
	t.Run("it returns volumes from all stores", func(t *testing.T) {
		cs := CompositeStore{
			stores: []compositeStoreEntry{
				{model.TimeFromUnix(10), mockStoreVolume{mockStore: mockStore(0), value: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{{Name: `{foo="bar"}`, Volume: 15}}, Limit: 10,
				}}},
				{model.TimeFromUnix(20), mockStoreVolume{mockStore: mockStore(1), value: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{{Name: `{foo="bar"}`, Volume: 30}}, Limit: 10,
				}}},
			},
		}

		volumes, err := cs.Volume(context.Background(), "fake", 10001, 20001, 10, nil, "")
		require.NoError(t, err)
		require.Equal(t, []logproto.Volume{{Name: `{foo="bar"}`, Volume: 45}}, volumes.Volumes)
	})

	t.Run("it returns an error if any store returns an error", func(t *testing.T) {
		cs := CompositeStore{
			stores: []compositeStoreEntry{
				{model.TimeFromUnix(10), mockStoreVolume{mockStore: mockStore(0), value: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{{Name: `{foo="bar"}`, Volume: 15}}, Limit: 10,
				}}},
				{model.TimeFromUnix(20), mockStoreVolume{mockStore: mockStore(1), err: errors.New("something bad")}},
			},
		}

		volumes, err := cs.Volume(context.Background(), "fake", 10001, 20001, 10, nil, "")
		require.Error(t, err, "something bad")
		require.Nil(t, volumes)
	})
}

func TestFilterForTimeRange(t *testing.T) {
	mkRefs := func(from, through model.Time) (res []*logproto.ChunkRef) {
		for i := from; i <= through; i++ {
			res = append(res, &logproto.ChunkRef{
				From:    i,
				Through: i + 1,
			})
		}
		return res
	}

	mkChks := func(from, through model.Time) (res []chunk.Chunk) {
		for _, ref := range mkRefs(from, through) {
			res = append(res, chunk.Chunk{ChunkRef: *ref})
		}
		return res
	}

	for _, tc := range []struct {
		desc          string
		input         []*logproto.ChunkRef
		from, through model.Time
		exp           []chunk.Chunk
	}{
		{
			desc:    "no refs",
			input:   nil,
			from:    0,
			through: 10,
			exp:     []chunk.Chunk{},
		},
		{
			desc:    "no refs in range",
			input:   mkRefs(0, 5),
			from:    10,
			through: 15,
			exp:     []chunk.Chunk{},
		},
		{
			desc:    "all refs in range",
			input:   mkRefs(0, 5),
			from:    0,
			through: 5,
			exp:     mkChks(0, 5),
		},
		{
			desc:    "some refs in range",
			input:   mkRefs(0, 5),
			from:    2,
			through: 3,
			exp:     mkChks(2, 3),
		},
		{
			desc:    "left overlap",
			input:   mkRefs(0, 5),
			from:    3,
			through: 7,
			exp:     mkChks(3, 5),
		},
		{
			desc:    "right overlap",
			input:   mkRefs(5, 10),
			from:    3,
			through: 7,
			exp:     mkChks(5, 7),
		},
		{
			desc: "ref with from == through",
			input: []*logproto.ChunkRef{
				{From: 1, Through: 1}, // outside
				{From: 2, Through: 2}, // ref.From == from == ref.Through
				{From: 3, Through: 3}, // inside
				{From: 4, Through: 4}, // ref.From == through == ref.Through
				{From: 5, Through: 5}, // outside
			},
			from:    2,
			through: 4,
			exp: []chunk.Chunk{
				{ChunkRef: logproto.ChunkRef{From: 2, Through: 2}},
				{ChunkRef: logproto.ChunkRef{From: 3, Through: 3}},
				{ChunkRef: logproto.ChunkRef{From: 4, Through: 4}},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := filterForTimeRange(tc.input, tc.from, tc.through)
			require.Equal(t, tc.exp, got)
		})
	}
}
