package queryfrontend

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// TestAbsentSamples_MatchEngine checks that a skipped absent_over_time answers on
// the same steps the engine evaluates over an empty window.
func TestAbsentSamples_MatchEngine(t *testing.T) {
	const query = `absent_over_time({app="api"} |= "timeout" [5s])`
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	step := time.Second

	tests := []struct {
		name       string
		start, end time.Time
		step       time.Duration
	}{
		{name: "end on a step", start: base, end: base.Add(2 * time.Second), step: step},
		{name: "end between steps", start: base, end: base.Add(2500 * time.Millisecond), step: step},
		{
			name:  "sub-millisecond start and end",
			start: base.Add(500 * time.Microsecond),
			end:   base.Add(time.Second + 300*time.Microsecond),
			step:  step,
		},
		{name: "instant", start: base, end: base},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params, err := logql.NewLiteralParams(query, tc.start, tc.end, tc.step, 0, logproto.FORWARD, 100, nil, nil)
			require.NoError(t, err)
			eng := logql.NewEngine(logql.EngineOpts{}, logql.NewMockQuerier(1, nil), logql.NoLimits, nil)
			res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "tenant"))
			require.NoError(t, err)

			var want []int64
			switch data := res.Data.(type) {
			case promql.Matrix:
				for _, series := range data {
					for _, p := range series.Floats {
						want = append(want, p.T)
					}
				}
			case promql.Vector:
				for _, s := range data {
					want = append(want, s.T)
				}
			}

			var req queryrangebase.Request = &queryrange.LokiRequest{
				Query: query, StartTs: tc.start, EndTs: tc.end, Step: tc.step.Milliseconds(),
			}
			if tc.step == 0 {
				req = &queryrange.LokiInstantRequest{Query: query, TimeTs: tc.start}
			}
			var got []int64
			for _, s := range absentSamples(req) {
				got = append(got, s.TimestampMs)
			}
			require.Equal(t, want, got)
		})
	}
}

// TestSetAbsentSeries_MergesWithEvaluatedSplit checks that skipped splits and an
// evaluated split merge into the same single series the engine returns for the
// whole range.
func TestSetAbsentSeries_MergesWithEvaluatedSplit(t *testing.T) {
	const query = `absent_over_time({app="api"} |= "timeout" [5s])`
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	step := 5 * time.Second
	streams := []logproto.Stream{{
		Labels:  `{app="api"}`,
		Entries: []logproto.Entry{{Timestamp: base.Add(10 * time.Second), Line: "timeout"}},
	}}
	eng := logql.NewEngine(logql.EngineOpts{}, logql.NewMockQuerier(1, streams), logql.NoLimits, nil)

	evaluate := func(start, end time.Time) *queryrange.LokiPromResponse {
		params, err := logql.NewLiteralParams(query, start, end, step, 0, logproto.FORWARD, 100, nil, nil)
		require.NoError(t, err)
		res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "tenant"))
		require.NoError(t, err)
		resp, err := queryrange.ResultToResponse(res, params)
		require.NoError(t, err)
		return resp.(*queryrange.LokiPromResponse)
	}
	skip := func(start, end time.Time) *queryrange.LokiPromResponse {
		expr, err := syntax.ParseExpr(query)
		require.NoError(t, err)
		req := &queryrange.LokiRequest{Query: query, StartTs: start, EndTs: end, Step: step.Milliseconds()}
		resp := skippedMetricResponse()
		require.NoError(t, setAbsentSeries(resp, req, expr))
		return resp
	}

	end := base.Add(20 * time.Second)
	merged, err := queryrange.DefaultCodec.MergeResponse(
		skip(base, base.Add(5*time.Second)),
		evaluate(base.Add(10*time.Second), base.Add(15*time.Second)),
		skip(end, end),
	)
	require.NoError(t, err)
	require.Equal(t, evaluate(base, end).Response.Data.Result, merged.(*queryrange.LokiPromResponse).Response.Data.Result)
}
