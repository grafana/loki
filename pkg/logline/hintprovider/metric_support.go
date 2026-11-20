package hintprovider

import (
	"github.com/grafana/loki/v3/pkg/logline/needle"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func SupportedMetricQuery(expr syntax.Expr, ngramLength, maxConjunctions int) needle.Conjunctions {
	selector, ok := metricLogSelector(expr)
	if !ok {
		return nil
	}
	return SupportedQuery(selector, ngramLength, maxConjunctions)
}

func metricLogSelector(expr syntax.Expr) (syntax.Expr, bool) {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		// Unlike other aggregations, absent_over_time returns a 1 for an empty window,
		// and each wrapper (sum, stddev, topk, ...) turns that into a different result.
		// We'd have to handle each one, so for now, we don't support wrapped absent_over_time.
		if r, ok := e.Left.(*syntax.RangeAggregationExpr); ok && r.Operation == syntax.OpRangeTypeAbsent {
			return nil, false
		}
		return metricLogSelector(e.Left)
	case *syntax.RangeAggregationExpr:
		// Left is the offset range. Left.Left is the log selector inside it,
		// for example `{app="api"} |= "timeout"` in
		// count_over_time({app="api"} |= "timeout" [5m] offset 10m).
		// A supported query has one range aggregation, so the selector is always this field.
		return e.Left.Left, true
	default:
		return nil, false
	}
}
