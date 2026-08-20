package logql

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestApproxCountDistinctLocalEval(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-30 * time.Second), Line: `mac="aa:bb"`},
				{Timestamp: now.Add(-20 * time.Second), Line: `mac="aa:bb"`},
				{Timestamp: now.Add(-10 * time.Second), Line: `mac="cc:dd"`},
			},
		},
		{
			Labels: `{job="devices", version="2"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-15 * time.Second), Line: `mac="ee:ff"`},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec, ok := res.Data.(promql.Vector)
	require.True(t, ok)
	require.Len(t, vec, 2)

	byVersion := map[string]float64{}
	for _, sample := range vec {
		byVersion[sample.Metric.Get("version")] = sample.F
	}
	require.InDelta(t, 2, byVersion["1"], 0.01)
	require.InDelta(t, 1, byVersion["2"], 0.01)
}

func TestApproxCountDistinctInstantOnly(t *testing.T) {
	q := &countingSampleQuerier{MockQuerier: NewMockQuerier(1, nil)}
	eng := NewEngine(EngineOpts{}, q, NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"}[1m]) by (version)`,
		time.Unix(0, 0), time.Unix(60, 0), time.Second, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	_, err = eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "only supported on instant queries")
	require.Zero(t, q.selectSamples)
}

type countingSampleQuerier struct {
	MockQuerier
	selectSamples int
}

func (q *countingSampleQuerier) SelectSamples(ctx context.Context, req SelectSampleParams) (iter.SampleIterator, error) {
	q.selectSamples++
	return q.MockQuerier.SelectSamples(ctx, req)
}

func TestApproxCountDistinctBoundaries(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				// Exactly at lower bound (T-D): excluded (open lower).
				{Timestamp: now.Add(-time.Minute), Line: `mac="lower"`},
				// Inside range.
				{Timestamp: now.Add(-30 * time.Second), Line: `mac="inside"`},
				// Exactly at T: included (closed upper via +1ns on End).
				{Timestamp: now, Line: `mac="upper"`},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)
	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)
	require.InDelta(t, 2, vec[0].F, 0.01)
}
