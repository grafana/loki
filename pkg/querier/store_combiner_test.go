package querier

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/goleak"

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
	logs   []logproto.Stream
	series []logproto.SeriesIdentifier
	stats  *stats.Stats
	shards *logproto.ShardsResponse

	// sampleSeries is the input SelectSamples turns into one iterator per series, then merges and
	// orders by req.Order. A caller need not pre-sort it.
	sampleSeries []logproto.Series
	labelValues  []string
	labelNames   []string
	volumeResult *logproto.VolumeResponse

	// selectSamplesErr, if set, makes SelectSamples return this error instead of building an iterator.
	selectSamplesErr error

	// closeErr, if set, makes a Close call on a previously returned iterator return this error.
	closeErr error

	// selectSamplesCalls counts calls to SelectSamples.
	selectSamplesCalls atomic.Int64

	// closedIterators counts Close calls on iterators a prior SelectSamples call returned.
	closedIterators atomic.Int64

	// receivedSampleReqs and receivedLogReqs keep the request each Select* call got, by pointer,
	// so a test can read the time range back after the call returned. A caller that hands every
	// store the same request shows up here, even when the call itself read the range eagerly.
	receivedSampleReqs []*logproto.SampleQueryRequest
	receivedLogReqs    []*logproto.QueryRequest
}

func (m *mockStore) SelectLogs(_ context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	m.receivedLogReqs = append(m.receivedLogReqs, req.QueryRequest)
	streams := make([]logproto.Stream, len(m.logs))
	copy(streams, m.logs)
	return iter.NewStreamsIterator(streams, req.Direction), nil
}

func (m *mockStore) SelectSeries(_ context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	m.receivedLogReqs = append(m.receivedLogReqs, req.QueryRequest)
	return m.series, nil
}

func (m *mockStore) Stats(_ context.Context, _ string, _ model.Time, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return m.stats, nil
}

func (m *mockStore) GetShards(_ context.Context, _ string, _ model.Time, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return m.shards, nil
}

func (m *mockStore) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	m.selectSamplesCalls.Add(1)
	m.receivedSampleReqs = append(m.receivedSampleReqs, req.SampleQueryRequest)
	if req.SampleQueryRequest == nil {
		return nil, fmt.Errorf("SampleQueryRequest must not be nil")
	}
	if m.selectSamplesErr != nil {
		return nil, m.selectSamplesErr
	}

	its := make([]iter.SampleIterator, 0, len(m.sampleSeries))
	for _, s := range m.sampleSeries {
		its = append(its, iter.NewSeriesIterator(s))
	}

	// A real store returns samples in the order the request asked for. Mimic that so
	// StoreCombiner is exercised the way a real caller would use it.
	var merged iter.SampleIterator
	switch req.Order {
	case logproto.SAMPLE_ORDER_BY_TIMESTAMP:
		merged = iter.NewTimestampFirstMergeSampleIterator(ctx, its)
	case logproto.SAMPLE_ORDER_BY_STREAM:
		merged = iter.NewStreamFirstMergeSampleIterator(ctx, its)
	default:
		return nil, fmt.Errorf("unknown sample order %v", req.Order)
	}
	return &closeTrackingSampleIterator{SampleIterator: merged, closed: &m.closedIterators, closeErr: m.closeErr}, nil
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

// closeTrackingSampleIterator counts Close calls, so a test can assert a caller that opened an
// iterator also closed it. If closeErr is set, Close returns it instead of delegating, so a
// test can assert a caller doesn't let a close error corrupt an unrelated returned error.
type closeTrackingSampleIterator struct {
	iter.SampleIterator
	closed   *atomic.Int64
	closeErr error
}

func (c *closeTrackingSampleIterator) Close() error {
	c.closed.Add(1)
	if c.closeErr != nil {
		return c.closeErr
	}
	return c.SampleIterator.Close()
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
			sampleSeries: []logproto.Series{
				{Samples: []logproto.Sample{{Timestamp: time.Unix(1, 0).UnixNano(), Value: 1.0, Hash: 1}}},
			},
		}
		store2 := &mockStore{
			sampleSeries: []logproto.Series{
				{Samples: []logproto.Sample{{Timestamp: time.Unix(2, 0).UnixNano(), Value: 2.0, Hash: 2}}},
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

	t.Run("SelectSamples honors stream-first order and dedups across stores", func(t *testing.T) {
		// Two schema periods (two stores). Each returns stream-first samples with a stable
		// per-stream hash. Stream "a" is present in both (a replica duplicate). The combiner must
		// merge stream-first, grouped by stream with the duplicate collapsed, not reorder by
		// timestamp.
		hash := func(app string) uint64 { return labels.StableHash(labels.FromStrings("app", app)) }
		mkSeries := func(app string, s ...logproto.Sample) logproto.Series {
			return logproto.Series{Labels: `{app="` + app + `"}`, StreamHash: hash(app), Samples: s}
		}

		store1 := &mockStore{sampleSeries: []logproto.Series{
			mkSeries("a", logproto.Sample{Timestamp: 1, Value: 1, Hash: 11}, logproto.Sample{Timestamp: 10, Value: 1, Hash: 110}),
		}}
		store2 := &mockStore{sampleSeries: []logproto.Series{
			mkSeries("a", logproto.Sample{Timestamp: 1, Value: 1, Hash: 11}), // duplicate of store1's a@1
			mkSeries("b", logproto.Sample{Timestamp: 5, Value: 1, Hash: 5}),
		}}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		it, err := sc.SelectSamples(context.Background(), logql.SelectSampleParams{
			SampleQueryRequest: &logproto.SampleQueryRequest{
				Start: time.Unix(0, 0),
				End:   time.Unix(1, 0), // wide enough to select both store periods (model.Time is ms)
				Order: logproto.SAMPLE_ORDER_BY_STREAM,
			},
		})
		require.NoError(t, err)

		type point struct {
			hash uint64
			ts   int64
		}
		var got []point
		for it.Next() {
			got = append(got, point{it.StreamHash(), it.At().Timestamp})
		}
		require.NoError(t, it.Err())
		require.Less(t, hash("b"), hash("a"))
		require.Equal(t, []point{
			{hash("b"), 5},
			{hash("a"), 1}, // a@1 deduped across stores
			{hash("a"), 10},
		}, got)
	})

	t.Run("SelectSamples rejects an unknown order via the first store it reaches", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		// StoreCombiner does not validate Order itself: it relies on each store rejecting an
		// order it doesn't understand, the same way mockStore does here.
		store1 := &mockStore{sampleSeries: []logproto.Series{{Labels: `{app="a"}`}}}
		store2 := &mockStore{sampleSeries: []logproto.Series{{Labels: `{app="b"}`}}}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		it, err := sc.SelectSamples(context.Background(), logql.SelectSampleParams{
			SampleQueryRequest: &logproto.SampleQueryRequest{
				Start: time.Unix(0, 0),
				End:   time.Unix(2, 0),
				Order: logproto.SampleOrder(99),
			},
		})
		require.Error(t, err)
		require.Nil(t, it)

		// store1 fails first and stops the fan-out; store2 is never reached.
		require.Equal(t, int64(1), store1.selectSamplesCalls.Load())
		require.Zero(t, store2.selectSamplesCalls.Load())
	})

	t.Run("SelectSamples closes earlier stores' iterators when a later store errors", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		// store1's own Close call fails too, with a distinct error. The returned error must
		// still be exactly store2's, unpolluted by store1's close failure.
		store1CloseErr := fmt.Errorf("store1 close failed")
		store1 := &mockStore{sampleSeries: []logproto.Series{{Labels: `{app="a"}`}}, closeErr: store1CloseErr}
		store2SelectErr := fmt.Errorf("store2 unavailable")
		store2 := &mockStore{selectSamplesErr: store2SelectErr}

		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: model.Time(0)},
			{Store: store2, From: model.Time(2)},
		})

		it, err := sc.SelectSamples(context.Background(), logql.SelectSampleParams{
			SampleQueryRequest: &logproto.SampleQueryRequest{
				Start: time.Unix(0, 0),
				End:   time.Unix(2, 0),
				Order: logproto.SAMPLE_ORDER_BY_STREAM,
			},
		})
		require.ErrorIs(t, err, store2SelectErr)
		require.NotErrorIs(t, err, store1CloseErr)
		require.Nil(t, it)

		require.Equal(t, int64(1), store1.selectSamplesCalls.Load())
		require.Equal(t, int64(1), store2.selectSamplesCalls.Load())
		require.Equal(t, int64(1), store1.closedIterators.Load(), "store1's iterator, already open when store2 errored, must be closed")
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

func TestStoreCombiner_PerStoreTimeRange(t *testing.T) {
	// SelectLogParams and SelectSampleParams hold a pointer to the request, so narrowing the time
	// range must not write through that pointer: every store would then see the last store's
	// range, and the caller's own request would change under it.
	const (
		store2From = model.Time(200)
		queryStart = model.Time(100)
		queryEnd   = model.Time(300)
	)

	expectedRanges := [][2]time.Time{
		{queryStart.Time(), (store2From - 1).Time()},
		{store2From.Time(), queryEnd.Time()},
	}

	t.Run("SelectLogs", func(t *testing.T) {
		store1, store2 := &mockStore{}, &mockStore{}
		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: queryStart},
			{Store: store2, From: store2From},
		})

		req := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Start:     queryStart.Time(),
			End:       queryEnd.Time(),
			Direction: logproto.FORWARD,
		}}

		it, err := sc.SelectLogs(context.Background(), req)
		require.NoError(t, err)
		require.NoError(t, it.Close())

		requireLogRanges(t, expectedRanges, store1, store2)
		require.Equal(t, queryStart.Time(), req.Start, "caller's start must not change")
		require.Equal(t, queryEnd.Time(), req.End, "caller's end must not change")
	})

	t.Run("SelectSeries", func(t *testing.T) {
		store1, store2 := &mockStore{}, &mockStore{}
		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: queryStart},
			{Store: store2, From: store2From},
		})

		req := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Start: queryStart.Time(),
			End:   queryEnd.Time(),
		}}

		_, err := sc.SelectSeries(context.Background(), req)
		require.NoError(t, err)

		requireLogRanges(t, expectedRanges, store1, store2)
		require.Equal(t, queryStart.Time(), req.Start, "caller's start must not change")
		require.Equal(t, queryEnd.Time(), req.End, "caller's end must not change")
	})

	t.Run("SelectSamples", func(t *testing.T) {
		store1, store2 := &mockStore{}, &mockStore{}
		sc := NewStoreCombiner([]StoreConfig{
			{Store: store1, From: queryStart},
			{Store: store2, From: store2From},
		})

		req := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start: queryStart.Time(),
			End:   queryEnd.Time(),
			Order: logproto.SAMPLE_ORDER_BY_TIMESTAMP,
		}}

		it, err := sc.SelectSamples(context.Background(), req)
		require.NoError(t, err)
		require.NoError(t, it.Close())

		for i, store := range []*mockStore{store1, store2} {
			require.Len(t, store.receivedSampleReqs, 1, "store %d call count", i)
			got := store.receivedSampleReqs[0]
			require.Equal(t, expectedRanges[i][0], got.Start, "store %d start", i)
			require.Equal(t, expectedRanges[i][1], got.End, "store %d end", i)
		}
		require.NotSame(t, store1.receivedSampleReqs[0], store2.receivedSampleReqs[0], "stores must not share a request")
		require.Equal(t, queryStart.Time(), req.Start, "caller's start must not change")
		require.Equal(t, queryEnd.Time(), req.End, "caller's end must not change")
	})
}

func requireLogRanges(t *testing.T, expected [][2]time.Time, stores ...*mockStore) {
	t.Helper()

	for i, store := range stores {
		require.Len(t, store.receivedLogReqs, 1, "store %d call count", i)
		got := store.receivedLogReqs[0]
		require.Equal(t, expected[i][0], got.Start, "store %d start", i)
		require.Equal(t, expected[i][1], got.End, "store %d end", i)
	}
	require.NotSame(t, stores[0].receivedLogReqs[0], stores[1].receivedLogReqs[0], "stores must not share a request")
}
