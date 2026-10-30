package hintprovider

import "github.com/grafana/loki/v4/pkg/logql/syntax"

func SupportedMetricQuery(expr syntax.Expr, ngramLength int) []string {
	selector, ok := metricLogSelector(expr)
	if !ok {
		return nil
	}
	return SupportedQuery(selector, ngramLength)
}

func metricLogSelector(expr syntax.Expr) (syntax.Expr, bool) {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
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
