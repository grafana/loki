package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// BuildLogCountQuery takes a LogQL query and the original query time range,
// extracts the log selector and pipeline, and builds a sum(count_over_time())
// query that counts all matching log lines the original query would read.
//
// For log queries the entire expression is wrapped in
// sum(count_over_time(<query> [<range>])). For metric queries the inner log
// selector is extracted from the first LogRangeExpr and the range interval is
// added to cover the lookback window. If the original metric query has an
// outer aggregation with grouping labels, those are preserved in the sum
// (e.g. sum by (level) (count_over_time(...))).
//
// It returns the count query and the timestamp to run it as an instant query.
func BuildLogCountQuery(query string, start, end time.Time) (countQuery string, evalTime time.Time, err error) {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parsing query: %w", err)
	}

	evalTime = end

	if _, isSample := expr.(syntax.SampleExpr); !isSample {
		duration := end.Sub(start)
		if duration <= 0 {
			duration = time.Second
		}
		inner := fmt.Sprintf("count_over_time(%s [%ds])", query, int64(duration.Seconds()))
		return fmt.Sprintf("sum(%s)", inner), evalTime, nil
	}

	var selector string
	var maxInterval time.Duration

	expr.Walk(func(e syntax.Expr) bool {
		lr, ok := e.(*syntax.LogRangeExpr)
		if !ok {
			return true
		}
		if selector == "" {
			selector = lr.Left.String()
		}
		if lr.Interval > maxInterval {
			maxInterval = lr.Interval
		}
		return true
	})

	if selector == "" {
		return "", time.Time{}, fmt.Errorf("could not extract log selector from metric query")
	}

	duration := end.Sub(start) + maxInterval
	if duration <= 0 {
		duration = time.Second
	}

	inner := fmt.Sprintf("count_over_time(%s [%ds])", selector, int64(duration.Seconds()))
	grouping := extractOuterGrouping(expr)

	return fmt.Sprintf("%s(%s)", grouping, inner), evalTime, nil
}

// extractOuterGrouping walks up from the outermost expression to find the
// first VectorAggregationExpr and returns "sum by (...)" or "sum without (...)"
// matching the original aggregation's grouping. If no grouping is found it
// returns a plain "sum".
func extractOuterGrouping(expr syntax.Expr) string {
	var grouping string

	expr.Walk(func(e syntax.Expr) bool {
		va, ok := e.(*syntax.VectorAggregationExpr)
		if !ok {
			return true
		}
		if grouping != "" {
			return false
		}
		if va.Grouping != nil && len(va.Grouping.Groups) > 0 {
			labels := strings.Join(va.Grouping.Groups, ", ")
			if va.Grouping.Without {
				grouping = fmt.Sprintf("sum without (%s)", labels)
			} else {
				grouping = fmt.Sprintf("sum by (%s)", labels)
			}
		}
		return false
	})

	if grouping == "" {
		return "sum"
	}
	return grouping
}
