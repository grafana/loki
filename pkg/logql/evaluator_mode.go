package logql

import "github.com/grafana/loki/v3/pkg/logql/syntax"

type EvaluatorMode int

const (
	ModeDefault = iota
	ModeMetricsOnly
)

// canSkipLogLines Determines whether lines can be skipped in the new chunk format V5.
func canSkipLogLines(expr syntax.SampleExpr) bool {
	switch e := expr.(type) {
	case *syntax.RangeAggregationExpr:
		switch e.Operation {
		case syntax.OpRangeTypeCount:
			// can also be syntax.OpRangeTypeRate
			return true
		}
	case *syntax.VectorAggregationExpr:
		return canSkipLogLines(e.Left)
	}

	return false
}

// Returns true if the expression is a sum aggregation over a range expression like:
// sum(count_over_time({app="foo"}[5m])) or
// sum by (app)(rate({app="foo"}[5m]))
func isSumRangeExpr(expr syntax.Expr) bool {
	// First check if it's a vector aggregation
	vecAgg, ok := expr.(*syntax.VectorAggregationExpr)
	if !ok {
		return false
	}

	// Check if it's a sum operation
	if vecAgg.Operation != syntax.OpTypeSum {
		return false
	}

	// Check if the left side is a range operation
	rangeExpr, ok := vecAgg.Left.(*syntax.RangeAggregationExpr)
	if !ok {
		return false
	}

	// These range operations don't need log lines
	switch rangeExpr.Operation {
	case syntax.OpRangeTypeCount,
		syntax.OpRangeTypeRate:
		return true
	}

	return false
}
