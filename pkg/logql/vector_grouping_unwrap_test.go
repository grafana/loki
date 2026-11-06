package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

// noGroupingInjectionQuerier is a MockQuerier that hands the extractor the range aggregation
// alone, so a parent `sum by (...)` can't inject its grouping into it.
type noGroupingInjectionQuerier struct {
	MockQuerier
}

// newNoGroupingInjectionQuerier returns a querier over streams that builds extractors without the
// vector aggregation pushdown.
func newNoGroupingInjectionQuerier(streams []logproto.Stream) noGroupingInjectionQuerier {
	return noGroupingInjectionQuerier{MockQuerier: NewMockQuerier(1, streams)}
}

// SelectSamples returns the samples for the range aggregation under a vector aggregation, if any.
func (q noGroupingInjectionQuerier) SelectSamples(ctx context.Context, req SelectSampleParams) (iter.SampleIterator, error) {
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}
	if vec, ok := expr.(*syntax.VectorAggregationExpr); ok {
		stripped := *req.SampleQueryRequest
		stripped.Plan = &plan.QueryPlan{AST: vec.Left}
		req.SampleQueryRequest = &stripped
	}
	return q.MockQuerier.SelectSamples(ctx, req)
}

// unwrapGroupingStreams returns one stream whose JSON lines carry an unwrappable `v` field and an
// unrelated `other` field.
func unwrapGroupingStreams(ts time.Time) []logproto.Stream {
	return []logproto.Stream{{
		Labels: `{app="x"}`,
		Entries: []logproto.Entry{
			{Timestamp: ts, Line: `{"v":1,"other":"a"}`},
			{Timestamp: ts.Add(time.Second), Line: `{"v":2,"other":"a"}`},
			{Timestamp: ts.Add(2 * time.Second), Line: `{"v":2,"other":"b"}`},
		},
	}}
}

// execInstantVector runs an instant query and returns its samples keyed by label set.
func execInstantVector(t *testing.T, eng *QueryEngine, query string, ts time.Time) map[string]float64 {
	t.Helper()

	params, err := NewLiteralParams(query, ts, ts, 0, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec, ok := res.Data.(promql.Vector)
	require.True(t, ok, "expected a vector, got %T", res.Data)

	got := make(map[string]float64, len(vec))
	for _, s := range vec {
		got[s.Metric.String()] = s.F
	}
	return got
}

// TestVectorGroupingInjection_ShouldNotChangeUnwrapResults checks that `sum by (...)` gives the same
// answer whether or not its grouping is injected into a nested `unwrap` range aggregation.
//
// The baseline querier strips the vector aggregation before building the extractor, so the
// extractor is built the way it would be without the pushdown.
func TestVectorGroupingInjection_ShouldNotChangeUnwrapResults(t *testing.T) {
	ts := time.Unix(100, 0)
	streams := unwrapGroupingStreams(ts)
	pushedEng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	baselineEng := NewEngine(EngineOpts{}, newNoGroupingInjectionQuerier(streams), NoLimits, nil)
	evalTs := ts.Add(10 * time.Second)

	tests := map[string]struct {
		grouping string
	}{
		"parent groups by the unwrap label":                       {grouping: "by (v)"},
		"parent groups by the unwrap label and another label":     {grouping: "by (v, other)"},
		"parent groups by another label only":                     {grouping: "by (other)"},
		"parent groups without the unwrap label":                  {grouping: "without (v)"},
		"parent groups without another label":                     {grouping: "without (other)"},
		"parent has no grouping":                                  {grouping: ""},
		"parent groups by the unwrap label and a missing label":   {grouping: "by (v, missing)"},
		"parent groups without the unwrap label and another one":  {grouping: "without (v, other)"},
		"parent groups by the stream label and the unwrap label":  {grouping: "by (app, v)"},
		"parent groups without the stream label and unwrap label": {grouping: "without (app, v)"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			query := fmt.Sprintf(`sum %s (sum_over_time({app="x"} | json | unwrap v [1m]))`, tc.grouping)

			require.Equal(t,
				execInstantVector(t, baselineEng, query, evalTs),
				execInstantVector(t, pushedEng, query, evalTs),
			)
		})
	}
}
