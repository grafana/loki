package logql

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// sharedOffsetStepEvaluators runs the legs of a range query that differ only in their offset as one
// query over the union of their time ranges, so that the legs read each byte once. It returns
// ok=false when no legs can share a query.
func (ev *DownstreamEvaluator) sharedOffsetStepEvaluators(ctx context.Context, queries []DownstreamQuery, params Params) ([]StepEvaluator, bool, error) {
	type group struct {
		query   DownstreamQuery
		expr    syntax.SampleExpr
		offsets []time.Duration
	}

	step := params.Step()
	if step <= 0 {
		return nil, false, nil
	}

	var (
		groups []*group
		byKey  = map[string]*group{}
	)
	for _, q := range queries {
		expr, ok := q.Params.GetExpression().(syntax.SampleExpr)
		if !ok {
			return nil, false, nil
		}
		offsets := getOffsets(expr)
		if len(offsets) != 1 {
			return nil, false, nil
		}
		key := withOffset(expr, 0).String() + strings.Join(q.Params.Shards(), ",")
		g, ok := byKey[key]
		if !ok {
			g = &group{query: q, expr: expr}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.offsets = append(g.offsets, offsets[0])
	}
	if len(groups) == len(queries) {
		return nil, false, nil
	}

	window := params.End().Sub(params.Start()) + step
	shared := make([]DownstreamQuery, 0, len(groups))
	for _, g := range groups {
		minOff, maxOff := slices.Min(g.offsets), slices.Max(g.offsets)
		// The shared query must answer on the steps of every leg, and must not answer on more steps
		// than the legs together would.
		for _, off := range g.offsets {
			if (off-minOff)%step != 0 {
				return nil, false, nil
			}
		}
		if maxOff-minOff > time.Duration(len(g.offsets)-1)*window {
			return nil, false, nil
		}
		shared = append(shared, DownstreamQuery{
			Params: paramsWithStart{
				Params: ParamsWithExpressionOverride{
					Params:             g.query.Params,
					ExpressionOverride: withOffset(g.expr, minOff),
				},
				start: params.Start().Add(minOff - maxOff),
			},
		})
	}

	results, err := ev.Downstream(ctx, shared, NewBufferedAccumulator(len(shared)))
	if err != nil {
		return nil, true, err
	}

	xs := make([]StepEvaluator, 0, len(queries))
	for i, g := range groups {
		m, ok := results[i].Data.(promql.Matrix)
		if !ok {
			return nil, true, fmt.Errorf("unexpected type (%T) uncoercible to StepEvaluator", results[i].Data)
		}
		minOff := slices.Min(g.offsets)
		for _, off := range g.offsets {
			xs = append(xs, newOffsetStepEvaluator(m, params, off-minOff))
		}
	}
	return xs, true, nil
}

// withOffset returns a copy of expr with the offset of its range aggregations set to offset.
func withOffset(expr syntax.SampleExpr, offset time.Duration) syntax.SampleExpr {
	expr = syntax.MustClone(expr)
	expr.Walk(func(e syntax.Expr) bool {
		if r, ok := e.(*syntax.RangeAggregationExpr); ok {
			r.Left.Offset = offset
		}
		return true
	})
	return expr
}

// paramsWithStart overrides the start of the query.
type paramsWithStart struct {
	Params
	start time.Time
}

// Start returns the overriding start.
func (p paramsWithStart) Start() time.Time {
	return p.start
}

// offsetStepEvaluator steps through the result of a query as if the query had offset more.
type offsetStepEvaluator struct {
	*MatrixStepEvaluator
	offset int64
}

func newOffsetStepEvaluator(m promql.Matrix, params Params, offset time.Duration) *offsetStepEvaluator {
	start := params.Start().Add(-offset)

	// MatrixStepEvaluator consumes the points of its series, so every evaluator needs its own series.
	series := make(promql.Matrix, 0, len(m))
	for _, s := range m {
		i := sort.Search(len(s.Floats), func(i int) bool { return s.Floats[i].T >= start.UnixMilli() })
		if i < len(s.Floats) {
			series = append(series, promql.Series{Metric: s.Metric, Floats: s.Floats[i:]})
		}
	}

	return &offsetStepEvaluator{
		MatrixStepEvaluator: NewMatrixStepEvaluator(start, params.End().Add(-offset), params.Step(), series),
		offset:              offset.Milliseconds(),
	}
}

func (e *offsetStepEvaluator) Next() (bool, int64, StepResult) {
	ok, ts, r := e.MatrixStepEvaluator.Next()
	if !ok {
		return false, 0, nil
	}
	vec := r.SampleVector()
	for i := range vec {
		vec[i].T += e.offset
	}
	return true, ts + e.offset, r
}
