package logql

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestApproxCountDistinctEval(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(60 * time.Second)
	step := 30 * time.Second
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				// T=100 window (40s, 100s] only.
				{Timestamp: start.Add(-50 * time.Second), Line: `mac="early"`},
				// T=100 and T=130 windows.
				{Timestamp: start.Add(-10 * time.Second), Line: `mac="mid"`},
				// T=160 window (100s, 160s] only.
				{Timestamp: start.Add(40 * time.Second), Line: `mac="late"`},
			},
		},
		{
			Labels: `{job="devices", version="2"}`,
			Entries: []logproto.Entry{
				// T=100 and T=130 windows.
				{Timestamp: start.Add(-15 * time.Second), Line: `mac="other"`},
			},
		},
	}

	type seriesExpect struct {
		metric  labels.Labels
		instant float64
		rangeTs []float64
	}

	tests := []struct {
		name    string
		query   string
		instant bool
		series  []seriesExpect
	}{
		{
			name:    "instant default grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m])`,
			instant: true,
			series: []seriesExpect{
				{metric: labels.FromStrings("job", "devices", "version", "1"), instant: 2},
				{metric: labels.FromStrings("job", "devices", "version", "2"), instant: 1},
			},
		},
		{
			name:    "instant ungrouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by ()`,
			instant: true,
			series: []seriesExpect{
				{metric: labels.EmptyLabels(), instant: 3},
			},
		},
		{
			name:    "instant grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
			instant: true,
			series: []seriesExpect{
				{metric: labels.FromStrings("version", "1"), instant: 2},
				{metric: labels.FromStrings("version", "2"), instant: 1},
			},
		},
		{
			name:    "range default grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m])`,
			instant: false,
			series: []seriesExpect{
				{metric: labels.FromStrings("job", "devices", "version", "1"), rangeTs: []float64{2, 1, 1}},
				{metric: labels.FromStrings("job", "devices", "version", "2"), rangeTs: []float64{1, 1}},
			},
		},
		{
			name:    "range ungrouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by ()`,
			instant: false,
			series: []seriesExpect{
				{metric: labels.EmptyLabels(), rangeTs: []float64{3, 2, 1}},
			},
		},
		{
			name:    "range grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
			instant: false,
			series: []seriesExpect{
				{metric: labels.FromStrings("version", "1"), rangeTs: []float64{2, 1, 1}},
				{metric: labels.FromStrings("version", "2"), rangeTs: []float64{1, 1}},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qStart, qEnd, qStep := start, start, time.Duration(0)
			if !tc.instant {
				qEnd, qStep = end, step
			}
			params, err := NewLiteralParams(
				tc.query, qStart, qEnd, qStep, 0, logproto.FORWARD, 1000, nil, nil,
			)
			require.NoError(t, err)

			res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
			require.NoError(t, err)

			if tc.instant {
				vec, ok := res.Data.(promql.Vector)
				require.True(t, ok)
				require.Len(t, vec, len(tc.series))
				got := map[uint64]float64{}
				for _, sample := range vec {
					got[labels.StableHash(sample.Metric)] = sample.F
				}
				for _, exp := range tc.series {
					require.InDelta(t, exp.instant, got[labels.StableHash(exp.metric)], 0.01, exp.metric.String())
				}
				return
			}

			matrix, ok := res.Data.(promql.Matrix)
			require.True(t, ok)
			require.Len(t, matrix, len(tc.series))
			got := map[uint64][]promql.FPoint{}
			for _, series := range matrix {
				got[labels.StableHash(series.Metric)] = series.Floats
			}
			for _, exp := range tc.series {
				points := got[labels.StableHash(exp.metric)]
				require.Len(t, points, len(exp.rangeTs), exp.metric.String())
				for i, want := range exp.rangeTs {
					require.Equal(t, start.Add(time.Duration(i)*step).UnixMilli(), points[i].T)
					require.InDelta(t, want, points[i].F, 0.01, exp.metric.String())
				}
			}
		})
	}
}

func TestApproxCountDistinctRangeOffset(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				// Inside the offset window (T-1m-30s, T-30s] = (10s, 70s].
				{Timestamp: now.Add(-50 * time.Second), Line: `mac="shifted"`},
				// After the offset window; excluded.
				{Timestamp: now.Add(-10 * time.Second), Line: `mac="too-new"`},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m] offset 30s) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)
	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)
	require.Equal(t, now.UnixMilli(), vec[0].T)
	require.InDelta(t, 1, vec[0].F, 0.01)
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
