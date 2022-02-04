package base

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestDuplicatesSamples(t *testing.T) {
	ts := logproto.TimeSeries{
		Labels: []logproto.LabelAdapter{
			{
				Name:  "lbl",
				Value: "val",
			},
		},
		Samples: []logproto.LegacySample{
			{Value: 0.948569891, TimestampMs: 1583946731937},
			{Value: 0.948569891, TimestampMs: 1583946731937},
			{Value: 0.949927461, TimestampMs: 1583946751878},
			{Value: 0.949927461, TimestampMs: 1583946751878},
			{Value: 0.951334505, TimestampMs: 1583946769353},
			{Value: 0.951334505, TimestampMs: 1583946769353},
			{Value: 0.951334505, TimestampMs: 1583946779855},
			{Value: 0.951334505, TimestampMs: 1583946779855},
			{Value: 0.952676231, TimestampMs: 1583946820080},
			{Value: 0.952676231, TimestampMs: 1583946820080},
			{Value: 0.954158847, TimestampMs: 1583946844857},
			{Value: 0.954158847, TimestampMs: 1583946844857},
			{Value: 0.955572384, TimestampMs: 1583946858609},
			{Value: 0.955572384, TimestampMs: 1583946858609},
			{Value: 0.955572384, TimestampMs: 1583946869878},
			{Value: 0.955572384, TimestampMs: 1583946869878},
			{Value: 0.955572384, TimestampMs: 1583946885903},
			{Value: 0.955572384, TimestampMs: 1583946885903},
			{Value: 0.956823037, TimestampMs: 1583946899767},
			{Value: 0.956823037, TimestampMs: 1583946899767},
		},
	}

	{
		out := runPromQLAndGetJSONResult(t, "rate(metr[1m])", ts, 10*time.Second)
		require.Contains(t, out, "\"NaN\"")
	}

	// run same query, but with deduplicated samples
	deduped := logproto.TimeSeries{
		Labels: []logproto.LabelAdapter{
			{
				Name:  "lbl",
				Value: "val",
			},
		},
		Samples: dedupeSorted(ts.Samples),
	}

	{
		out := runPromQLAndGetJSONResult(t, "rate(metr[1m])", deduped, 10*time.Second)
		require.NotContains(t, out, "\"NaN\"")
	}
}

func dedupeSorted(samples []logproto.LegacySample) []logproto.LegacySample {
	out := []logproto.LegacySample(nil)
	lastTs := int64(0)
	for _, s := range samples {
		if s.TimestampMs == lastTs {
			continue
		}

		out = append(out, s)
		lastTs = s.TimestampMs
	}
	return out
}

func runPromQLAndGetJSONResult(t *testing.T, query string, ts logproto.TimeSeries, step time.Duration) string {
	tq := &testQueryable{ts: newTimeSeriesSeriesSet([]logproto.TimeSeries{ts})}

	engine := promql.NewEngine(promql.EngineOpts{
		Logger:     log.NewNopLogger(),
		Timeout:    10 * time.Second,
		MaxSamples: 1e6,
	})

	start := model.Time(ts.Samples[0].TimestampMs).Time()
	end := model.Time(ts.Samples[len(ts.Samples)-1].TimestampMs).Time()

	q, err := engine.NewRangeQuery(tq, query, start, end, step)
	require.NoError(t, err)

	res := q.Exec(context.Background())
	require.NoError(t, err)

	out, err := json.Marshal(res)
	require.NoError(t, err)

	return string(out)
}

type testQueryable struct {
	ts storage.SeriesSet
}

func (t *testQueryable) Querier(_ context.Context, _, _ int64) (storage.Querier, error) {
	return testQuerier{ts: t.ts}, nil
}

type testQuerier struct {
	ts storage.SeriesSet
}

func (m testQuerier) Select(_ bool, _ *storage.SelectHints, _ ...*labels.Matcher) storage.SeriesSet {
	return m.ts
}

func (m testQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, nil
}

func (m testQuerier) LabelNames(matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, nil
}

func (testQuerier) Close() error {
	return nil
}
