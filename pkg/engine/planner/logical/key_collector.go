package logical

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// Type hint constants for unwrap operations
const (
	TypeHintFloat64  = "float64"
	TypeHintInt64    = "int64"
	TypeHintDuration = "duration"
)

// Unwrap operation and identifier names
const (
	UnwrapBytes           = "bytes"
	UnwrapDuration        = "duration"
	UnwrapDurationSeconds = "duration_seconds"
)

// getUnwrapTypeHint determines the type hint based on the unwrap operation
func getUnwrapTypeHint(unwrap *syntax.UnwrapExpr) string {
	if unwrap == nil {
		return TypeHintFloat64
	}

	// Check for conversion operation like bytes() or duration()
	if unwrap.Operation != "" {
		switch unwrap.Operation {
		case UnwrapBytes:
			return TypeHintInt64
		case UnwrapDuration, UnwrapDurationSeconds:
			return TypeHintDuration
		default:
			return TypeHintFloat64
		}
	}

	// Check if the identifier itself suggests a type
	// This is a heuristic for fields like "bytes" or "duration"
	switch unwrap.Identifier {
	case UnwrapBytes:
		return TypeHintInt64
	case UnwrapDuration:
		return TypeHintDuration
	default:
		return TypeHintFloat64
	}
}

// collectLogfmtFilterKeys walks the AST and collects keys from label filters
// that appear after a logfmt parser. This is used for both log and metric queries
// to determine which fields need to be parsed for filtering.
//
// Returns:
//   - keys: sorted list of unique keys from label filters after logfmt
//   - hasLogfmt: true if a logfmt parser was found
func collectLogfmtFilterKeys(expr syntax.Expr) ([]string, bool) {
	keysMap := make(map[string]bool)
	var foundLogfmt bool

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
	}

	expr.Accept(visitor)

	// If no logfmt was found, return empty
	if !foundLogfmt {
		return []string{}, false
	}

	// Convert map to sorted slice
	keys := make([]string, 0, len(keysMap))
	for key := range keysMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys, true
}

// collectLogfmtMetricKeys collects keys needed for metric aggregations from logfmt parsing.
// This includes groupBy labels from vector aggregations and unwrap identifiers with type hints.
//
// Returns:
//   - keys: sorted list of unique keys needed for metric operations
//   - hints: map of field name to type hint for unwrap operations
func collectLogfmtMetricKeys(expr syntax.Expr) ([]string, map[string]string) {
	keysMap := make(map[string]bool)
	hints := make(map[string]string)
	var foundLogfmt bool

	// Use the visitor pattern to walk the AST
	visitor := &syntax.DepthFirstTraversal{
		VisitLogfmtParserFn: func(_ syntax.RootVisitor, _ *syntax.LogfmtParserExpr) {
			foundLogfmt = true
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
			// First, visit the child expression to find logfmt
			if e.Left != nil {
				e.Left.Accept(v)
			}
			// If we found a logfmt parser and there's a groupBy clause,
			// collect all groupBy labels
			if foundLogfmt && e.Grouping != nil {
				for _, label := range e.Grouping.Groups {
					keysMap[label] = true
				}
			}
		},
	}

	expr.Accept(visitor)

	// If no logfmt was found, return empty
	if !foundLogfmt {
		return []string{}, map[string]string{}
	}

	// Convert map to sorted slice
	keys := make([]string, 0, len(keysMap))
	for key := range keysMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys, hints
}
