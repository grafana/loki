package logical

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// CollectRequestedKeys walks the AST and collects keys that need to be extracted
// from logfmt parsing in metric queries, along with numeric type hints for unwrap operations.
//
// Returns:
//   - keys: sorted list of unique keys to extract
//   - hints: map of field name to type hint ("int64", "float64", or "duration")
//
// TODO(twhitney): These nil cases are temporary and will be removed once we implement support for log queries.
// Returns nil, nil if:
//   - The query is not a metric query (no range or vector aggregation)
//   - The query does not use logfmt parsing
//
// This function is intentionally limited in scope to support optimization of
// logfmt metric queries in the v2 engine. Support for log queries and other
// parsers may be added in the future.
func CollectRequestedKeys(expr syntax.Expr) ([]string, map[string]string) {
	// Use a map to track unique keys
	keysMap := make(map[string]bool)
	hints := make(map[string]string)
	var foundLogfmt bool
	var isMetricQuery bool

	// Use the visitor pattern to walk the AST
	visitor := &syntax.DepthFirstTraversal{
		VisitLogfmtParserFn: func(_ syntax.RootVisitor, _ *syntax.LogfmtParserExpr) {
			foundLogfmt = true
		},
		VisitLabelFilterFn: func(_ syntax.RootVisitor, e *syntax.LabelFilterExpr) {
			// If we found a logfmt parser and now have a label filter,
			// collect the label names using RequiredLabelNames()
			if foundLogfmt && e.LabelFilterer != nil {
				// All LabelFilterers implement the Stage interface which has RequiredLabelNames()
				if stage, ok := e.LabelFilterer.(log.Stage); ok {
					for _, key := range stage.RequiredLabelNames() {
						keysMap[key] = true
					}
				}
			}
		},
		VisitLogRangeFn: func(v syntax.RootVisitor, e *syntax.LogRangeExpr) {
			// First, visit the LogSelectorExpr child to find logfmt
			if e.Left != nil {
				e.Left.Accept(v)
			}
			// If we found a logfmt parser and there's an unwrap expression,
			// collect the unwrap identifier and its type hint
			if foundLogfmt && e.Unwrap != nil {
				keysMap[e.Unwrap.Identifier] = true
				hints[e.Unwrap.Identifier] = getUnwrapTypeHint(e.Unwrap)
			}
		},
		VisitRangeAggregationFn: func(v syntax.RootVisitor, e *syntax.RangeAggregationExpr) {
			isMetricQuery = true
			// First, visit the LogRangeExpr child to find logfmt
			if e.Left != nil {
				e.Left.Accept(v)
			}
			// Then check for unwrap in the embedded LogRangeExpr
			if foundLogfmt && e.Left != nil && e.Left.Unwrap != nil {
				keysMap[e.Left.Unwrap.Identifier] = true
				hints[e.Left.Unwrap.Identifier] = getUnwrapTypeHint(e.Left.Unwrap)
			}
		},
		VisitVectorAggregationFn: func(v syntax.RootVisitor, e *syntax.VectorAggregationExpr) {
			isMetricQuery = true
			// First, visit the child expression to find logfmt
			if e.Left != nil {
				e.Left.Accept(v)
			}
			// If we found a logfmt parser and there's a groupBy clause,
			// collect all groupBy labels (we'll filter stream labels later)
			if foundLogfmt && e.Grouping != nil {
				for _, label := range e.Grouping.Groups {
					keysMap[label] = true
				}
			}
		},
	}

	expr.Accept(visitor)

	// Return nil for non-metric queries or non-logfmt queries
	if !isMetricQuery || !foundLogfmt {
		return nil, nil
	}

	// Convert map to sorted slice
	keys := make([]string, 0, len(keysMap))
	for key := range keysMap {
		keys = append(keys, key)
	}
	// Sort for deterministic results
	sort.Strings(keys)

	// Return empty map if no hints found (not nil)
	if len(hints) == 0 {
		hints = map[string]string{}
	}

	return keys, hints
}

const (
	typeHintFloat64  = "float64"
	typeHintInt64    = "int64"
	typeHintDuration = "duration"
)

// getUnwrapTypeHint determines the type hint based on the unwrap operation
func getUnwrapTypeHint(unwrap *syntax.UnwrapExpr) string {
	if unwrap == nil {
		return typeHintFloat64
	}

	// Check for conversion operation like bytes() or duration()
	if unwrap.Operation != "" {
		switch unwrap.Operation {
		case "bytes":
			return typeHintInt64
		case typeHintDuration, "duration_seconds":
			return typeHintDuration
		default:
			return typeHintFloat64
		}
	}

	// Check if the identifier itself suggests a type
	// This is a heuristic for fields like "bytes" or "duration"
	switch unwrap.Identifier {
	case "bytes":
		return typeHintInt64
	case typeHintDuration:
		return typeHintDuration
	default:
		return typeHintFloat64
	}
}
