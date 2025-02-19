package querier

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

func TestStoreCombiner_findStoresForTimeRange(t *testing.T) {
	tests := []struct {
		name      string
		stores    []StoreConfig
		from      model.Time
		through   model.Time
		expected  []storeWithRange
		wantEmpty bool
	}{
		{
			name:      "empty stores",
			stores:    nil,
			from:      model.Time(100),
			through:   model.Time(200),
			wantEmpty: true,
		},
		{
			name: "single store covers entire range",
			stores: []StoreConfig{
				{From: model.Time(0)},
			},
			from:    model.Time(100),
			through: model.Time(200),
			expected: []storeWithRange{
				{from: model.Time(100), through: model.Time(200)},
			},
		},
		{
			name: "query range before any store",
			stores: []StoreConfig{
				{From: model.Time(100)},
			},
			from:      model.Time(0),
			through:   model.Time(50),
			wantEmpty: true,
		},
		{
			name: "query range spans multiple stores",
			stores: []StoreConfig{
				{From: model.Time(200)},
				{From: model.Time(100)},
				{From: model.Time(0)},
			},
			from:    model.Time(150),
			through: model.Time(250),
			expected: []storeWithRange{
				{from: model.Time(150), through: model.Time(199)},
				{from: model.Time(200), through: model.Time(250)},
			},
		},
		{
			name: "query range exactly matches store boundaries",
			stores: []StoreConfig{
				{From: model.Time(200)},
				{From: model.Time(100)},
			},
			from:    model.Time(100),
			through: model.Time(200),
			expected: []storeWithRange{
				{from: model.Time(100), through: model.Time(199)},
				{from: model.Time(200), through: model.Time(200)},
			},
		},
		{
			name: "pre-1970 dates",
			stores: []StoreConfig{
				{From: model.Time(100)},
				{From: model.Time(0)},
				{From: model.Time(-100)},
			},
			from:    model.Time(-50),
			through: model.Time(50),
			expected: []storeWithRange{
				{from: model.Time(-50), through: model.Time(-1)},
				{from: model.Time(0), through: model.Time(50)},
			},
		},
		{
			name: "query range spans all stores",
			stores: []StoreConfig{
				{From: model.Time(300)},
				{From: model.Time(200)},
				{From: model.Time(100)},
			},
			from:    model.Time(50),
			through: model.Time(350),
			expected: []storeWithRange{
				{from: model.Time(100), through: model.Time(199)},
				{from: model.Time(200), through: model.Time(299)},
				{from: model.Time(300), through: model.Time(350)},
			},
		},
		{
			name: "query range in future",
			stores: []StoreConfig{
				{From: model.Time(100)},
				{From: model.Time(0)},
			},
			from:    model.Time(200),
			through: model.Time(300),
			expected: []storeWithRange{
				{from: model.Time(200), through: model.Time(300)},
			},
		},
		{
			name: "store with 0 from",
			stores: []StoreConfig{
				{From: model.Time(0)},
				{From: model.Time(100)},
			},
			from:    model.Time(0),
			through: model.Time(300),
			expected: []storeWithRange{
				{from: model.Time(0), through: model.Time(99)},
				{from: model.Time(100), through: model.Time(300)},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := NewStoreCombiner(tc.stores)
			got := sc.findStoresForTimeRange(tc.from, tc.through)

			if tc.wantEmpty {
				require.Empty(t, got)
				return
			}

			require.Equal(t, len(tc.expected), len(got), "number of store ranges", tc.expected, got)
			for i := range tc.expected {
				require.Equal(t, tc.expected[i].from, got[i].from, "from time for store %d", i)
				require.Equal(t, tc.expected[i].through, got[i].through, "through time for store %d", i)
			}
		})
	}
}

func TestStoreCombiner_StoreOrdering(t *testing.T) {
	unorderedStores := []StoreConfig{
		{From: model.Time(100)},
		{From: model.Time(300)},
		{From: model.Time(200)},
	}

	sc := NewStoreCombiner(unorderedStores)

	// Verify stores are sorted in ascending order
	for i := 1; i < len(sc.stores); i++ {
		require.True(t, sc.stores[i-1].From < sc.stores[i].From,
			"stores should be sorted in ascending order, but found %v before %v",
			time.Unix(int64(sc.stores[i-1].From), 0),
			time.Unix(int64(sc.stores[i].From), 0))
	}
}

func TestStoreCombiner_TimeRangeBoundaries(t *testing.T) {
	stores := []StoreConfig{
		{From: model.Time(300)},
		{From: model.Time(200)},
		{From: model.Time(100)},
	}

	tests := []struct {
		name           string
		from, through  model.Time
		expectedRanges [][2]model.Time // pairs of [from, through]
	}{
		{
			name:    "exact boundaries",
			from:    model.Time(200),
			through: model.Time(300),
			expectedRanges: [][2]model.Time{
				{model.Time(200), model.Time(299)},
				{model.Time(300), model.Time(300)},
			},
		},
		{
			name:    "overlapping boundaries",
			from:    model.Time(250),
			through: model.Time(350),
			expectedRanges: [][2]model.Time{
				{model.Time(250), model.Time(299)},
				{model.Time(300), model.Time(350)},
			},
		},
		{
			name:    "within single store",
			from:    model.Time(210),
			through: model.Time(290),
			expectedRanges: [][2]model.Time{
				{model.Time(210), model.Time(290)},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := NewStoreCombiner(stores)
			ranges := sc.findStoresForTimeRange(tc.from, tc.through)

			require.Equal(t, len(tc.expectedRanges), len(ranges), "number of time ranges")
			for i, expected := range tc.expectedRanges {
				require.Equal(t, expected[0], ranges[i].from, "from time for range %d", i)
				require.Equal(t, expected[1], ranges[i].through, "through time for range %d", i)
			}
		})
	}
}

type mockStore struct {
	logs         []logproto.Stream
	series       []logproto.SeriesIdentifier
	stats        *stats.Stats
	shards       *logproto.ShardsResponse
	samples      []logproto.Sample
	labelValues  []string
	labelNames   []string
	volumeResult *logproto.VolumeResponse
}

func (m *mockStore) SelectLogs(_ context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	streams := make([]logproto.Stream, len(m.logs))
	copy(streams, m.logs)
	return iter.NewStreamsIterator(streams, req.Direction), nil
}

func (m *mockStore) SelectSeries(_ context.Context, _ logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	return m.series, nil
}

func (m *mockStore) Stats(_ context.Context, _ string, _ model.Time, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return m.stats, nil
}

func (m *mockStore) GetShards(_ context.Context, _ string, _ model.Time, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return m.shards, nil
}

func (m *mockStore) SelectSamples(_ context.Context, _ logql.SelectSampleParams) (iter.SampleIterator, error) {
	return iter.NewSeriesIterator(logproto.Series{
		Labels:  "",
		Samples: m.samples,
	}), nil
}

func (m *mockStore) LabelValuesForMetricName(_ context.Context, _ string, _ model.Time, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return m.labelValues, nil
}

func (m *mockStore) LabelNamesForMetricName(_ context.Context, _ string, _ model.Time, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return m.labelNames, nil
}

func (m *mockStore) Volume(_ context.Context, _ string, _ model.Time, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return m.volumeResult, nil
}

func TestStoreCombiner_Merging(t *testing.T) {
	t.Run("SelectLogs merges streams", func(t *testing.T) {
		store1 := &mockStore{
			logs: []logproto.Stream{
				{Labels: `{app="app1"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(1, 0), Line: "1"}}},
			},
		}
		store2 := &mockStore{
			logs: []logproto.Stream{
				{Labels: `{app="app2"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(2, 0), Line: "2"}}},
			},
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		iter, err := sc.SelectLogs(context.Background(), logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Start:     time.Unix(0, 0),
				End:       time.Unix(2, 0),
				Direction: logproto.FORWARD,
			},
		})
		require.NoError(t, err)

		// Convert iterator to streams for testing
		var streams []logproto.Stream
		for iter.Next() {
			stream := logproto.Stream{
				Labels:  iter.Labels(),
				Entries: []logproto.Entry{iter.At()},
			}
			streams = append(streams, stream)
		}
		require.NoError(t, iter.Err())
		require.Len(t, streams, 2)
		require.Equal(t, `{app="app1"}`, streams[0].Labels)
		require.Equal(t, `{app="app2"}`, streams[1].Labels)
	})

	t.Run("SelectSeries deduplicates series", func(t *testing.T) {
		store1 := &mockStore{
			series: []logproto.SeriesIdentifier{
				{Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "app", Value: "app1"}}},
			},
		}
		store2 := &mockStore{
			series: []logproto.SeriesIdentifier{
				{Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "app", Value: "app1"}}}, // Duplicate
				{Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "app", Value: "app2"}}},
			},
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		series, err := sc.SelectSeries(context.Background(), logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Start: time.Unix(0, 0),
				End:   time.Unix(2, 0),
			},
		})
		require.NoError(t, err)
		require.Len(t, series, 2) // Should deduplicate app1
	})

	t.Run("Stats merges stats", func(t *testing.T) {
		store1 := &mockStore{
			stats: &stats.Stats{Streams: 1, Chunks: 10, Bytes: 100},
		}
		store2 := &mockStore{
			stats: &stats.Stats{Streams: 2, Chunks: 20, Bytes: 200},
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		stats, err := sc.Stats(context.Background(), "user", 0, 2)
		require.NoError(t, err)
		require.Equal(t, uint64(3), stats.Streams) // 1 + 2
		require.Equal(t, uint64(30), stats.Chunks) // 10 + 20
		require.Equal(t, uint64(300), stats.Bytes) // 100 + 200
	})

	t.Run("GetShards returns largest shard set", func(t *testing.T) {
		store1 := &mockStore{
			shards: &logproto.ShardsResponse{
				Shards: []logproto.Shard{{Bounds: logproto.FPBounds{Min: 2, Max: 4}}},
			},
		}
		store2 := &mockStore{
			shards: &logproto.ShardsResponse{
				Shards: []logproto.Shard{{Bounds: logproto.FPBounds{Min: 1, Max: 2}}, {Bounds: logproto.FPBounds{Min: 2, Max: 3}}}, // More shards
			},
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		shards, err := sc.GetShards(context.Background(), "user", 0, 2, 1000, chunk.Predicate{})
		require.NoError(t, err)
		require.Equal(t, shards.Shards, []logproto.Shard{{Bounds: logproto.FPBounds{Min: 1, Max: 2}}, {Bounds: logproto.FPBounds{Min: 2, Max: 3}}}) // Should pick store2's response
	})

	t.Run("SelectSamples merges samples", func(t *testing.T) {
		store1 := &mockStore{
			samples: []logproto.Sample{
				{Timestamp: time.Unix(1, 0).UnixNano(), Value: 1.0, Hash: 1},
			},
		}
		store2 := &mockStore{
			samples: []logproto.Sample{
				{Timestamp: time.Unix(2, 0).UnixNano(), Value: 2.0, Hash: 2},
			},
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		iter, err := sc.SelectSamples(context.Background(), logql.SelectSampleParams{
			SampleQueryRequest: &logproto.SampleQueryRequest{
				Start: time.Unix(0, 0),
				End:   time.Unix(2, 0),
			},
		})
		require.NoError(t, err)

		var samples []logproto.Sample
		for iter.Next() {
			samples = append(samples, iter.At())
		}
		require.NoError(t, iter.Err())
		require.Len(t, samples, 2)
		require.Equal(t, float64(1.0), samples[0].Value)
		require.Equal(t, float64(2.0), samples[1].Value)
	})

	t.Run("LabelValuesForMetricName deduplicates values", func(t *testing.T) {
		store1 := &mockStore{
			labelValues: []string{"value1", "value2"},
		}
		store2 := &mockStore{
			labelValues: []string{"value2", "value3"}, // Note: value2 is duplicate
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		values, err := sc.LabelValuesForMetricName(context.Background(), "user", 0, 2, "logs", "label")
		require.NoError(t, err)
		require.Equal(t, []string{"value1", "value2", "value3"}, values)
	})

	t.Run("LabelNamesForMetricName deduplicates names", func(t *testing.T) {
		store1 := &mockStore{
			labelNames: []string{"name1", "name2"},
		}
		store2 := &mockStore{
			labelNames: []string{"name2", "name3"}, // Note: name2 is duplicate
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		names, err := sc.LabelNamesForMetricName(context.Background(), "user", 0, 2, "logs")
		require.NoError(t, err)
		require.Equal(t, []string{"name1", "name2", "name3"}, names)
	})

	t.Run("Volume merges responses", func(t *testing.T) {
		store1 := &mockStore{
			volumeResult: &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: "app1", Volume: 100},
				},
			},
		}
		store2 := &mockStore{
			volumeResult: &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: "app2", Volume: 200},
				},
			},
		}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		result, err := sc.Volume(context.Background(), "user", 0, 2, 10, nil, "")
		require.NoError(t, err)
		require.Len(t, result.Volumes, 2)
		require.Equal(t, uint64(200), result.Volumes[0].Volume)
		require.Equal(t, uint64(100), result.Volumes[1].Volume)
	})
}
