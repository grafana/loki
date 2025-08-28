package logical

import (
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

// collectLogfmtMetricKeys collects type hints for unwrap operations in metric queries.
// This is used to determine how to cast parsed values during query execution.
//
// Returns:
//   - keys: empty slice (kept for compatibility, will be removed later)
//   - hints: map of field name to type hint for unwrap operations
func collectLogfmtMetricKeys(expr syntax.Expr) ([]string, map[string]string) {
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
				hints[e.Left.Unwrap.Identifier] = getUnwrapTypeHint(e.Left.Unwrap)
			}
		},
	}

	expr.Accept(visitor)

	// Return empty keys slice and hints map
	return []string{}, hints
}