package base

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/prom1/storage/metric"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/chunkcompat"
)

const (
	mint, maxt = 0, 10
)

func TestDistributorQuerier(t *testing.T) {
	d := &MockDistributor{}
	d.On("Query", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		model.Matrix{
			// Matrixes are unsorted, so this tests that the labels get sorted.
			&model.SampleStream{
				Metric: model.Metric{
					"foo": "bar",
				},
			},
			&model.SampleStream{
				Metric: model.Metric{
					"bar": "baz",
				},
			},
		},
		nil)

	queryable := newDistributorQueryable(d, false, nil, 0)
	querier, err := queryable.Querier(context.Background(), mint, maxt)
	require.NoError(t, err)

	seriesSet := querier.Select(true, &storage.SelectHints{Start: mint, End: maxt})
	require.NoError(t, seriesSet.Err())

	require.True(t, seriesSet.Next())
	series := seriesSet.At()
	require.Equal(t, labels.Labels{{Name: "bar", Value: "baz"}}, series.Labels())

	require.True(t, seriesSet.Next())
	series = seriesSet.At()
	require.Equal(t, labels.Labels{{Name: "foo", Value: "bar"}}, series.Labels())

	require.False(t, seriesSet.Next())
	require.NoError(t, seriesSet.Err())
}

func TestDistributorQuerier_SelectShouldHonorQueryIngestersWithin(t *testing.T) {
	now := time.Now()

	tests := map[string]struct {
		querySeries          bool
		queryIngestersWithin time.Duration
		queryMinT            int64
		queryMaxT            int64
		expectedMinT         int64
		expectedMaxT         int64
	}{
		"should not manipulate query time range if queryIngestersWithin is disabled": {
			queryIngestersWithin: 0,
			queryMinT:            util.TimeToMillis(now.Add(-100 * time.Minute)),
			queryMaxT:            util.TimeToMillis(now.Add(-30 * time.Minute)),
			expectedMinT:         util.TimeToMillis(now.Add(-100 * time.Minute)),
			expectedMaxT:         util.TimeToMillis(now.Add(-30 * time.Minute)),
		},
		"should not manipulate query time range if queryIngestersWithin is enabled but query min time is newer": {
			queryIngestersWithin: time.Hour,
			queryMinT:            util.TimeToMillis(now.Add(-50 * time.Minute)),
			queryMaxT:            util.TimeToMillis(now.Add(-30 * time.Minute)),
			expectedMinT:         util.TimeToMillis(now.Add(-50 * time.Minute)),
			expectedMaxT:         util.TimeToMillis(now.Add(-30 * time.Minute)),
		},
		"should manipulate query time range if queryIngestersWithin is enabled and query min time is older": {
			queryIngestersWithin: time.Hour,
			queryMinT:            util.TimeToMillis(now.Add(-100 * time.Minute)),
			queryMaxT:            util.TimeToMillis(now.Add(-30 * time.Minute)),
			expectedMinT:         util.TimeToMillis(now.Add(-60 * time.Minute)),
			expectedMaxT:         util.TimeToMillis(now.Add(-30 * time.Minute)),
		},
		"should skip the query if the query max time is older than queryIngestersWithin": {
			queryIngestersWithin: time.Hour,
			queryMinT:            util.TimeToMillis(now.Add(-100 * time.Minute)),
			queryMaxT:            util.TimeToMillis(now.Add(-90 * time.Minute)),
			expectedMinT:         0,
			expectedMaxT:         0,
		},
		"should not manipulate query time range if queryIngestersWithin is enabled and query max time is older, but the query is for /series": {
			querySeries:          true,
			queryIngestersWithin: time.Hour,
			queryMinT:            util.TimeToMillis(now.Add(-100 * time.Minute)),
			queryMaxT:            util.TimeToMillis(now.Add(-90 * time.Minute)),
			expectedMinT:         util.TimeToMillis(now.Add(-100 * time.Minute)),
			expectedMaxT:         util.TimeToMillis(now.Add(-90 * time.Minute)),
		},
	}

	for _, streamingEnabled := range []bool{false, true} {
		for testName, testData := range tests {
			t.Run(fmt.Sprintf("%s (streaming enabled: %t)", testName, streamingEnabled), func(t *testing.T) {
				distributor := &MockDistributor{}
				distributor.On("Query", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(model.Matrix{}, nil)
				distributor.On("QueryStream", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&client.QueryStreamResponse{}, nil)
				distributor.On("MetricsForLabelMatchers", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]metric.Metric{}, nil)

				ctx := user.InjectOrgID(context.Background(), "test")
				queryable := newDistributorQueryable(distributor, streamingEnabled, nil, testData.queryIngestersWithin)
				querier, err := queryable.Querier(ctx, testData.queryMinT, testData.queryMaxT)
				require.NoError(t, err)

				// Select hints are passed by Prometheus when querying /series.
				var hints *storage.SelectHints
				if testData.querySeries {
					hints = &storage.SelectHints{Func: seriesFunc}
				}

				seriesSet := querier.Select(true, hints)
				require.NoError(t, seriesSet.Err())

				if testData.expectedMinT == 0 && testData.expectedMaxT == 0 {
					assert.Len(t, distributor.Calls, 0)
				} else {
					require.Len(t, distributor.Calls, 1)
					assert.InDelta(t, testData.expectedMinT, int64(distributor.Calls[0].Arguments.Get(1).(model.Time)), float64(5*time.Second.Milliseconds()))
					assert.Equal(t, testData.expectedMaxT, int64(distributor.Calls[0].Arguments.Get(2).(model.Time)))
				}
			})
		}
	}
}

func TestDistributorQueryableFilter(t *testing.T) {
	d := &MockDistributor{}
	dq := newDistributorQueryable(d, false, nil, 1*time.Hour)

	now := time.Now()

	queryMinT := util.TimeToMillis(now.Add(-5 * time.Minute))
	queryMaxT := util.TimeToMillis(now)

	require.True(t, dq.UseQueryable(now, queryMinT, queryMaxT))
	require.True(t, dq.UseQueryable(now.Add(time.Hour), queryMinT, queryMaxT))

	// Same query, hour+1ms later, is not sent to ingesters.
	require.False(t, dq.UseQueryable(now.Add(time.Hour).Add(1*time.Millisecond), queryMinT, queryMaxT))
}

func TestIngesterStreaming(t *testing.T) {
	// We need to make sure that there is atleast one chunk present,
	// else no series will be selected.
	promChunk, err := encoding.NewForEncoding(encoding.Bigchunk)
	require.NoError(t, err)

	clientChunks, err := chunkcompat.ToChunks([]chunk.Chunk{
		chunk.NewChunk("", 0, nil, promChunk, model.Earliest, model.Earliest),
	})
	require.NoError(t, err)

	d := &MockDistributor{}
	d.On("QueryStream", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		&client.QueryStreamResponse{
			Chunkseries: []client.TimeSeriesChunk{
				{
					Labels: []logproto.LabelAdapter{
						{Name: "bar", Value: "baz"},
					},
					Chunks: clientChunks,
				},
				{
					Labels: []logproto.LabelAdapter{
						{Name: "foo", Value: "bar"},
					},
					Chunks: clientChunks,
				},
			},
		},
		nil)

	ctx := user.InjectOrgID(context.Background(), "0")
	queryable := newDistributorQueryable(d, true, mergeChunks, 0)
	querier, err := queryable.Querier(ctx, mint, maxt)
	require.NoError(t, err)

	seriesSet := querier.Select(true, &storage.SelectHints{Start: mint, End: maxt})
	require.NoError(t, seriesSet.Err())

	require.True(t, seriesSet.Next())
	series := seriesSet.At()
	require.Equal(t, labels.Labels{{Name: "bar", Value: "baz"}}, series.Labels())

	require.True(t, seriesSet.Next())
	series = seriesSet.At()
	require.Equal(t, labels.Labels{{Name: "foo", Value: "bar"}}, series.Labels())

	require.False(t, seriesSet.Next())
	require.NoError(t, seriesSet.Err())
}

func TestIngesterStreamingMixedResults(t *testing.T) {
	const (
		mint = 0
		maxt = 10000
	)
	s1 := []logproto.LegacySample{
		{Value: 1, TimestampMs: 1000},
		{Value: 2, TimestampMs: 2000},
		{Value: 3, TimestampMs: 3000},
		{Value: 4, TimestampMs: 4000},
		{Value: 5, TimestampMs: 5000},
	}
	s2 := []logproto.LegacySample{
		{Value: 1, TimestampMs: 1000},
		{Value: 2.5, TimestampMs: 2500},
		{Value: 3, TimestampMs: 3000},
		{Value: 5.5, TimestampMs: 5500},
	}

	mergedSamplesS1S2 := []logproto.LegacySample{
		{Value: 1, TimestampMs: 1000},
		{Value: 2, TimestampMs: 2000},
		{Value: 2.5, TimestampMs: 2500},
		{Value: 3, TimestampMs: 3000},
		{Value: 4, TimestampMs: 4000},
		{Value: 5, TimestampMs: 5000},
		{Value: 5.5, TimestampMs: 5500},
	}

	d := &MockDistributor{}
	d.On("QueryStream", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		&client.QueryStreamResponse{
			Chunkseries: []client.TimeSeriesChunk{
				{
					Labels: []logproto.LabelAdapter{{Name: labels.MetricName, Value: "one"}},
					Chunks: convertToChunks(t, s1),
				},
				{
					Labels: []logproto.LabelAdapter{{Name: labels.MetricName, Value: "two"}},
					Chunks: convertToChunks(t, s1),
				},
			},

			Timeseries: []logproto.TimeSeries{
				{
					Labels:  []logproto.LabelAdapter{{Name: labels.MetricName, Value: "two"}},
					Samples: s2,
				},
				{
					Labels:  []logproto.LabelAdapter{{Name: labels.MetricName, Value: "three"}},
					Samples: s1,
				},
			},
		},
		nil)

	ctx := user.InjectOrgID(context.Background(), "0")
	queryable := newDistributorQueryable(d, true, mergeChunks, 0)
	querier, err := queryable.Querier(ctx, mint, maxt)
	require.NoError(t, err)

	seriesSet := querier.Select(true, &storage.SelectHints{Start: mint, End: maxt}, labels.MustNewMatcher(labels.MatchRegexp, labels.MetricName, ".*"))
	require.NoError(t, seriesSet.Err())

	require.True(t, seriesSet.Next())
	verifySeries(t, seriesSet.At(), labels.Labels{{Name: labels.MetricName, Value: "one"}}, s1)

	require.True(t, seriesSet.Next())
	verifySeries(t, seriesSet.At(), labels.Labels{{Name: labels.MetricName, Value: "three"}}, s1)

	require.True(t, seriesSet.Next())
	verifySeries(t, seriesSet.At(), labels.Labels{{Name: labels.MetricName, Value: "two"}}, mergedSamplesS1S2)

	require.False(t, seriesSet.Next())
	require.NoError(t, seriesSet.Err())
}

func verifySeries(t *testing.T, series storage.Series, l labels.Labels, samples []logproto.LegacySample) {
	require.Equal(t, l, series.Labels())

	it := series.Iterator()
	for _, s := range samples {
		require.True(t, it.Next())
		require.Nil(t, it.Err())
		ts, v := it.At()
		require.Equal(t, s.Value, v)
		require.Equal(t, s.TimestampMs, ts)
	}
	require.False(t, it.Next())
	require.Nil(t, it.Err())
}
func TestDistributorQuerier_LabelNames(t *testing.T) {
	someMatchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}
	labelNames := []string{"foo", "job"}

	t.Run("with matchers", func(t *testing.T) {
		metrics := []metric.Metric{
			{Metric: model.Metric{"foo": "bar"}},
			{Metric: model.Metric{"job": "baz"}},
			{Metric: model.Metric{"job": "baz", "foo": "boom"}},
		}
		d := &MockDistributor{}
		d.On("MetricsForLabelMatchers", mock.Anything, model.Time(mint), model.Time(maxt), someMatchers).
			Return(metrics, nil)

		queryable := newDistributorQueryable(d, false, nil, 0)
		querier, err := queryable.Querier(context.Background(), mint, maxt)
		require.NoError(t, err)

		names, warnings, err := querier.LabelNames(someMatchers...)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.Equal(t, labelNames, names)
	})
}

func convertToChunks(t *testing.T, samples []logproto.LegacySample) []client.Chunk {
	// We need to make sure that there is atleast one chunk present,
	// else no series will be selected.
	promChunk, err := encoding.NewForEncoding(encoding.Bigchunk)
	require.NoError(t, err)

	for _, s := range samples {
		c, err := promChunk.Add(model.SamplePair{Value: model.SampleValue(s.Value), Timestamp: model.Time(s.TimestampMs)})
		require.NoError(t, err)
		require.Nil(t, c)
	}

	clientChunks, err := chunkcompat.ToChunks([]chunk.Chunk{
		chunk.NewChunk("", 0, nil, promChunk, model.Time(samples[0].TimestampMs), model.Time(samples[len(samples)-1].TimestampMs)),
	})
	require.NoError(t, err)

	return clientChunks
}
