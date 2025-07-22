package executor

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

var matchAllFilter = logs.LogMessageFilterRowPredicate{
	Keep: func(_ []byte) bool { return true },
}

// buildLogsPredicate builds a logs predicate from an expression.
func buildLogsPredicate(expr physical.Expression) (logs.RowPredicate, error) {
	// TODO(rfratto): implement converting expressions into logs predicates.
	//
	// There's a few challenges here:
	//
	// - Expressions do not cleanly map to [logs.RowPredicate]s. For example,
	//   an expression may be simply a column reference, but a logs predicate is
	//   always some expression that can evaluate to true.
	//
	// - Mapping expressions into [dataobj.TimeRangePredicate] is a massive pain;
	//   since TimeRangePredicate specifies both bounds for the time range, we
	//   would need to find and collapse multiple physical.Expressions into a
	//   single TimeRangePredicate.
	//
	// - While [dataobj.MetadataMatcherPredicate] and
	//   [dataobj.LogMessageFilterPredicate] are catch-alls for function-based
	//   predicates, they are row-based and not column-based, so our
	//   expressionEvaluator cannot be used here.
	//
	// Long term, we likely want two things:
	//
	// 1. Use dataset.Reader and dataset.Predicate directly instead of
	//    dataobj.LogsReader.
	//
	// 2. Update dataset.Reader to be vector based instead of row-based.
	//
	// It's not clear if we should resolve the issues with LogsPredicate (or find
	// hacks to make them work in the short term), or skip straight to using
	// dataset.Reader instead.
	//
	// Implementing DataObjScan in the dataobj package would be a clean way to
	// handle all of this, but that would cause cyclic dependencies. I also don't
	// think we should start removing things from internal for this; we can probably
	// find a way to remove the explicit dependency from the dataobj package from
	// the physical planner instead.
	return mapInitiallySupportedPredicates(expr)
}

// Support for timestamp and metadata predicates has been implemented.
// TODO(owen-d): this can go away when we use dataset.Reader & dataset.Predicate directly
func mapInitiallySupportedPredicates(expr physical.Expression) (logs.RowPredicate, error) {
	return mapPredicates(expr)
}

func mapPredicates(expr physical.Expression) (logs.RowPredicate, error) {
	switch e := expr.(type) {
	case *physical.BinaryExpr:

		// Special case: both sides of the binary op are again binary expressions (where LHS is expected to be a column expression)
		if e.Left.Type() == physical.ExprTypeBinary && e.Right.Type() == physical.ExprTypeBinary {
			left, err := mapPredicates(e.Left)
			if err != nil {
				return nil, err
			}
			right, err := mapPredicates(e.Right)
			if err != nil {
				return nil, err
			}
			switch e.Op {
			case types.BinaryOpAnd:
				return logs.AndRowPredicate{
					Left:  left,
					Right: right,
				}, nil
			case types.BinaryOpOr:
				return logs.OrRowPredicate{
					Left:  left,
					Right: right,
				}, nil
			default:
				return nil, fmt.Errorf("unsupported operator in predicate: %s", e.Op)
			}
		}

		if e.Left.Type() != physical.ExprTypeColumn {
			return nil, fmt.Errorf("unsupported predicate, expected column ref on LHS: %s", expr.String())
		}

		left := e.Left.(*physical.ColumnExpr)
		switch left.Ref.Type {
		case types.ColumnTypeBuiltin:
			if left.Ref.Column == types.ColumnNameBuiltinTimestamp {
				return mapTimestampPredicate(e)
			}
			if left.Ref.Column == types.ColumnNameBuiltinMessage {
				return mapMessagePredicate(e)
			}
			return nil, fmt.Errorf("unsupported builtin column in predicate (only timestamp is supported for now): %s", left.Ref.Column)
		case types.ColumnTypeMetadata:
			return mapMetadataPredicate(e)
		default:
			return nil, fmt.Errorf("unsupported column ref type (%T) in predicate: %s", left.Ref, left.Ref.String())
		}
	default:
		return nil, fmt.Errorf("unsupported expression type (%T) in predicate: %s", expr, expr.String())
	}
}

func mapTimestampPredicate(expr physical.Expression) (logs.TimeRangeRowPredicate, error) {
	m := newTimestampPredicateMapper()
	if err := m.verify(expr); err != nil {
		return logs.TimeRangeRowPredicate{}, err
	}

	if err := m.processExpr(expr); err != nil {
		return logs.TimeRangeRowPredicate{}, err
	}

	// Check for impossible ranges that might have been formed.
	if m.res.StartTime.After(m.res.EndTime) {
		return logs.TimeRangeRowPredicate{},
			fmt.Errorf("impossible time range: start_time (%v) is after end_time (%v)", m.res.StartTime, m.res.EndTime)
	}
	if m.res.StartTime.Equal(m.res.EndTime) && (!m.res.IncludeStart || !m.res.IncludeEnd) {
		return logs.TimeRangeRowPredicate{},
			fmt.Errorf("impossible time range: start_time (%v) equals end_time (%v) but the range is exclusive", m.res.StartTime, m.res.EndTime)
	}

	return m.res, nil
}

func newTimestampPredicateMapper() *timestampPredicateMapper {
	open := logs.TimeRangeRowPredicate{
		StartTime:    time.Unix(0, math.MinInt64).UTC(),
		EndTime:      time.Unix(0, math.MaxInt64).UTC(),
		IncludeStart: true,
		IncludeEnd:   true,
	}
	return &timestampPredicateMapper{
		res: open,
	}
}

type timestampPredicateMapper struct {
	res logs.TimeRangeRowPredicate
}

// ensures the LHS is a timestamp column reference
// and the RHS is either a literal or a binary expression
func (m *timestampPredicateMapper) verify(expr physical.Expression) error {
	binop, ok := expr.(*physical.BinaryExpr)
	if !ok {
		return fmt.Errorf("unsupported expression type for timestamp predicate: %T, expected *physical.BinaryExpr", expr)
	}

	switch binop.Op {
	case types.BinaryOpEq, types.BinaryOpGt, types.BinaryOpGte, types.BinaryOpLt, types.BinaryOpLte:
		lhs, okLHS := binop.Left.(*physical.ColumnExpr)
		if !okLHS || lhs.Ref.Type != types.ColumnTypeBuiltin || lhs.Ref.Column != types.ColumnNameBuiltinTimestamp {
			return fmt.Errorf("invalid LHS for comparison: expected timestamp column, got %s", binop.Left.String())
		}

		// RHS must be a literal timestamp for simple comparisons.
		rhsLit, okRHS := binop.Right.(*physical.LiteralExpr)
		if !okRHS {
			return fmt.Errorf("invalid RHS for comparison: expected literal timestamp, got %T", binop.Right)
		}
		if rhsLit.ValueType() != datatype.Loki.Timestamp {
			return fmt.Errorf("unsupported literal type for RHS: %s, expected timestamp", rhsLit.ValueType())
		}
		return nil

	case types.BinaryOpAnd:
		if err := m.verify(binop.Left); err != nil {
			return fmt.Errorf("invalid left operand for AND: %w", err)
		}
		if err := m.verify(binop.Right); err != nil {
			return fmt.Errorf("invalid right operand for AND: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported operator for timestamp predicate: %s", binop.Op)
	}
}

// processExpr recursively processes the expression tree to update the time range.
func (m *timestampPredicateMapper) processExpr(expr physical.Expression) error {
	// verify should have already ensured expr is *physical.BinaryExpr
	binExp := expr.(*physical.BinaryExpr)

	switch binExp.Op {
	case types.BinaryOpAnd:
		if err := m.processExpr(binExp.Left); err != nil {
			return err
		}
		return m.processExpr(binExp.Right)
	case types.BinaryOpEq, types.BinaryOpGt, types.BinaryOpGte, types.BinaryOpLt, types.BinaryOpLte:
		// This is a comparison operation, rebound m.res
		// The 'right' here is binExp.Right, which could be a literal or a nested BinaryExpr.
		// rebound will extract the actual literal value.
		return m.rebound(binExp.Op, binExp.Right)
	default:
		// This case should ideally not be reached if verify is correct.
		return fmt.Errorf("unexpected operator in processExpr: %s", binExp.Op)
	}
}

// rebound updates the time range (m.res) based on a single comparison operation.
// The `op` is the comparison operator (e.g., Gt, Lte).
// The `rightExpr` is the RHS of the comparison, which might be a literal
// or a nested binary expression (e.g., `timestamp > (timestamp = X)`).
// This function will traverse `rightExpr` to find the innermost literal value.
func (m *timestampPredicateMapper) rebound(op types.BinaryOp, rightExpr physical.Expression) error {
	// `verify` (called by processExpr before this) now ensures that for comparison ops,
	// the original expression's RHS was a literal, or if it was an AND, its constituent parts were.
	// `processExpr` will pass the direct LiteralExpr from a comparison to rebound.
	literalExpr, ok := rightExpr.(*physical.LiteralExpr)
	if !ok {
		// This should not happen if verify and processExpr are correct.
		return fmt.Errorf("internal error: rebound expected LiteralExpr, got %T for: %s", rightExpr, rightExpr.String())
	}

	if literalExpr.ValueType() != datatype.Loki.Timestamp {
		// Also should be caught by verify.
		return fmt.Errorf("internal error: unsupported literal type in rebound: %s, expected timestamp", literalExpr.ValueType())
	}
	v := literalExpr.Literal.(datatype.TimestampLiteral).Value()
	val := time.Unix(0, int64(v)).UTC()

	switch op {
	case types.BinaryOpEq: // ts == val
		m.updateLowerBound(val, true)
		m.updateUpperBound(val, true)
	case types.BinaryOpGt: // ts > val
		m.updateLowerBound(val, false)
	case types.BinaryOpGte: // ts >= val
		m.updateLowerBound(val, true)
	case types.BinaryOpLt: // ts < val
		m.updateUpperBound(val, false)
	case types.BinaryOpLte: // ts <= val
		m.updateUpperBound(val, true)
	default:
		// Should not be reached if processExpr filters operators correctly.
		return fmt.Errorf("unsupported operator in rebound: %s", op)
	}
	return nil
}

// updateLowerBound updates the start of the time range (m.res.StartTime, m.res.IncludeStart).
// `val` is the new potential start time from the condition.
// `includeVal` indicates if this new start time is inclusive (e.g., from '>=' or '==').
func (m *timestampPredicateMapper) updateLowerBound(val time.Time, includeVal bool) {
	if val.After(m.res.StartTime) {
		// The new value is strictly greater than the current start, so it becomes the new start.
		m.res.StartTime = val
		m.res.IncludeStart = includeVal
	} else if val.Equal(m.res.StartTime) {
		// The new value is equal to the current start.
		// The range becomes more restrictive if the current start was exclusive and the new one is also exclusive or inclusive.
		// Or if the current was inclusive and the new one is exclusive.
		// Effectively, IncludeStart becomes true only if *both* the existing and new condition allow/imply inclusion.
		// No, this should be: if new condition makes it more restrictive (i.e. current is [T and new is (T ), then new is (T )
		// m.res.IncludeStart = m.res.IncludeStart && includeVal (This is for intersection: [a,b] AND (a,c] -> (a, min(b,c)] )
		// m.res.IncludeStart = m.res.IncludeStart && includeVal
		if !includeVal { // if new bound is exclusive (e.g. from `> val`)
			m.res.IncludeStart = false // existing [val,... or (val,... combined with (> val) becomes (val,...
		}
		// If includeVal is true (e.g. from `>= val`), and m.res.IncludeStart was already true, it stays true.
		// If includeVal is true, and m.res.IncludeStart was false, it stays false ( (val,...) AND [val,...] is (val,...) )
		// This logic is subtle. Let's restate:
		// Current Start: S, Is       (m.res.StartTime, m.res.IncludeStart)
		// New Condition: val, includeVal
		// if val > S: new start is val, includeVal
		// if val == S: new start is val. IncludeStart becomes m.res.IncludeStart AND includeVal.
		//    Example: current (S, ...), new condition S >= S. Combined: (S, ...). So if m.res.IncludeStart=false, new includeVal=true -> false.
		//    Example: current [S, ...), new condition S > S. Combined: (S, ...). So if m.res.IncludeStart=true, new includeVal=false -> false.
		//    This is correct for intersection.
		m.res.IncludeStart = m.res.IncludeStart && includeVal
	}
	// If val is before m.res.StartTime, the current StartTime is already more restrictive, so no change.
}

// updateUpperBound updates the end of the time range (m.res.EndTime, m.res.IncludeEnd).
// `val` is the new potential end time from the condition.
// `includeVal` indicates if this new end time is inclusive (e.g., from '<=' or '==').
func (m *timestampPredicateMapper) updateUpperBound(val time.Time, includeVal bool) {
	if val.Before(m.res.EndTime) {
		// The new value is strictly less than the current end, so it becomes the new end.
		m.res.EndTime = val
		m.res.IncludeEnd = includeVal
	} else if val.Equal(m.res.EndTime) {
		// The new value is equal to the current end.
		// Similar to updateLowerBound, the inclusiveness is an AND condition.
		// Example: current (..., S), new condition ts <= S. Combined: (..., S). m.res.IncludeEnd=false, includeVal=true -> false.
		// Example: current (..., S], new condition ts < S. Combined: (..., S). m.res.IncludeEnd=true, includeVal=false -> false.
		m.res.IncludeEnd = m.res.IncludeEnd && includeVal
	}
	// If val is after m.res.EndTime, the current EndTime is already more restrictive, so no change.
}

// mapMetadataPredicate converts a physical.Expression into a dataobj.Predicate for metadata filtering.
// It supports MetadataMatcherPredicate for equality checks on metadata fields,
// and can recursively handle AndPredicate, OrPredicate, and NotPredicate.
func mapMetadataPredicate(expr physical.Expression) (logs.RowPredicate, error) {
	switch e := expr.(type) {
	case *physical.BinaryExpr:
		switch e.Op {
		case types.BinaryOpEq:
			if e.Left.Type() != physical.ExprTypeColumn {
				return nil, fmt.Errorf("unsupported LHS type (%v) for EQ metadata predicate, expected ColumnExpr", e.Left.Type())
			}
			leftColumn, ok := e.Left.(*physical.ColumnExpr)
			if !ok { // Should not happen due to Type() check but defensive
				return nil, fmt.Errorf("LHS of EQ metadata predicate failed to cast to ColumnExpr")
			}
			if leftColumn.Ref.Type != types.ColumnTypeMetadata {
				return nil, fmt.Errorf("unsupported LHS column type (%v) for EQ metadata predicate, expected ColumnTypeMetadata", leftColumn.Ref.Type)
			}

			if e.Right.Type() != physical.ExprTypeLiteral {
				return nil, fmt.Errorf("unsupported RHS type (%v) for EQ metadata predicate, expected LiteralExpr", e.Right.Type())
			}
			rightLiteral, ok := e.Right.(*physical.LiteralExpr)
			if !ok { // Should not happen
				return nil, fmt.Errorf("RHS of EQ metadata predicate failed to cast to LiteralExpr")
			}
			if rightLiteral.ValueType() != datatype.Loki.String {
				return nil, fmt.Errorf("unsupported RHS literal type (%v) for EQ metadata predicate, expected ValueTypeStr", rightLiteral.ValueType())
			}
			val := rightLiteral.Literal.(datatype.StringLiteral).Value()

			return logs.MetadataMatcherRowPredicate{
				Key:   leftColumn.Ref.Column,
				Value: val,
			}, nil
		case types.BinaryOpAnd:
			leftPredicate, err := mapMetadataPredicate(e.Left)
			if err != nil {
				return nil, fmt.Errorf("failed to map left operand of AND: %w", err)
			}
			rightPredicate, err := mapMetadataPredicate(e.Right)
			if err != nil {
				return nil, fmt.Errorf("failed to map right operand of AND: %w", err)
			}
			return logs.AndRowPredicate{
				Left:  leftPredicate,
				Right: rightPredicate,
			}, nil
		case types.BinaryOpOr:
			leftPredicate, err := mapMetadataPredicate(e.Left)
			if err != nil {
				return nil, fmt.Errorf("failed to map left operand of OR: %w", err)
			}
			rightPredicate, err := mapMetadataPredicate(e.Right)
			if err != nil {
				return nil, fmt.Errorf("failed to map right operand of OR: %w", err)
			}
			return logs.OrRowPredicate{
				Left:  leftPredicate,
				Right: rightPredicate,
			}, nil
		default:
			return nil, fmt.Errorf("unsupported binary operator (%s) for metadata predicate, expected EQ, AND, or OR", e.Op)
		}
	case *physical.UnaryExpr:
		if e.Op != types.UnaryOpNot {
			return nil, fmt.Errorf("unsupported unary operator (%s) for metadata predicate, expected NOT", e.Op)
		}
		innerPredicate, err := mapMetadataPredicate(e.Left)
		if err != nil {
			return nil, fmt.Errorf("failed to map inner expression of NOT: %w", err)
		}
		return logs.NotRowPredicate{
			Inner: innerPredicate,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported expression type (%T) for metadata predicate, expected BinaryExpr or UnaryExpr", expr)
	}
}

func mapMessagePredicate(expr physical.Expression) (logs.RowPredicate, error) {
	switch e := expr.(type) {
	case *physical.BinaryExpr:
		switch e.Op {

		case types.BinaryOpMatchSubstr, types.BinaryOpNotMatchSubstr, types.BinaryOpMatchRe, types.BinaryOpNotMatchRe:
			return match(e)

		case types.BinaryOpMatchPattern, types.BinaryOpNotMatchPattern:
			return nil, fmt.Errorf("unsupported binary operator (%s) for log message predicate", e.Op)

		case types.BinaryOpAnd:
			leftPredicate, err := mapMessagePredicate(e.Left)
			if err != nil {
				return nil, fmt.Errorf("failed to map left operand of AND: %w", err)
			}
			rightPredicate, err := mapMessagePredicate(e.Right)
			if err != nil {
				return nil, fmt.Errorf("failed to map right operand of AND: %w", err)
			}
			return logs.AndRowPredicate{
				Left:  leftPredicate,
				Right: rightPredicate,
			}, nil

		case types.BinaryOpOr:
			leftPredicate, err := mapMessagePredicate(e.Left)
			if err != nil {
				return nil, fmt.Errorf("failed to map left operand of OR: %w", err)
			}
			rightPredicate, err := mapMessagePredicate(e.Right)
			if err != nil {
				return nil, fmt.Errorf("failed to map right operand of OR: %w", err)
			}
			return logs.OrRowPredicate{
				Left:  leftPredicate,
				Right: rightPredicate,
			}, nil

		default:
			return nil, fmt.Errorf("unsupported binary operator (%s) for log message predicate, expected MATCH_STR, NOT_MATCH_STR, MATCH_RE, NOT_MATCH_RE, AND, or OR", e.Op)
		}

	case *physical.UnaryExpr:
		if e.Op != types.UnaryOpNot {
			return nil, fmt.Errorf("unsupported unary operator (%s) for log message predicate, expected NOT", e.Op)
		}
		innerPredicate, err := mapMessagePredicate(e.Left)
		if err != nil {
			return nil, fmt.Errorf("failed to map inner expression of NOT: %w", err)
		}
		return logs.NotRowPredicate{
			Inner: innerPredicate,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported expression type (%T) for log message predicate, expected BinaryExpr or UnaryExpr", expr)
	}
}

func match(e *physical.BinaryExpr) (logs.RowPredicate, error) {
	val, err := rhsValue(e)
	if err != nil {
		return nil, err
	}

	switch e.Op {

	case types.BinaryOpMatchSubstr:
		return logs.LogMessageFilterRowPredicate{
			Keep: func(line []byte) bool { return bytes.Contains(line, []byte(val)) },
		}, nil

	case types.BinaryOpNotMatchSubstr:
		return logs.LogMessageFilterRowPredicate{
			Keep: func(line []byte) bool { return !bytes.Contains(line, []byte(val)) },
		}, nil

	case types.BinaryOpMatchRe:
		re, err := regexp.Compile(val)
		if err != nil {
			return nil, err
		}
		return logs.LogMessageFilterRowPredicate{
			Keep: func(line []byte) bool { return re.Match(line) },
		}, nil

	case types.BinaryOpNotMatchRe:
		re, err := regexp.Compile(val)
		if err != nil {
			return nil, err
		}
		return logs.LogMessageFilterRowPredicate{
			Keep: func(line []byte) bool { return !re.Match(line) },
		}, nil

	default:
		return nil, fmt.Errorf("unsupported binary operator (%s) for log message predicate", e.Op)
	}
}

func rhsValue(e *physical.BinaryExpr) (string, error) {
	op := e.Op.String()

	if e.Left.Type() != physical.ExprTypeColumn {
		return "", fmt.Errorf("unsupported LHS type (%v) for %s message predicate, expected ColumnExpr", e.Left.Type(), op)
	}
	leftColumn, ok := e.Left.(*physical.ColumnExpr)
	if !ok { // Should not happen due to Type() check but defensive
		return "", fmt.Errorf("LHS of %s message predicate failed to cast to ColumnExpr", op)
	}
	if leftColumn.Ref.Type != types.ColumnTypeBuiltin {
		return "", fmt.Errorf("unsupported LHS column type (%v) for %s message predicate, expected ColumnTypeBuiltin", leftColumn.Ref.Type, op)
	}

	if e.Right.Type() != physical.ExprTypeLiteral {
		return "", fmt.Errorf("unsupported RHS type (%v) for %s message predicate, expected LiteralExpr", e.Right.Type(), op)
	}
	rightLiteral, ok := e.Right.(*physical.LiteralExpr)
	if !ok { // Should not happen
		return "", fmt.Errorf("RHS of %s message predicate failed to cast to LiteralExpr", op)
	}
	if rightLiteral.ValueType() != datatype.Loki.String {
		return "", fmt.Errorf("unsupported RHS literal type (%v) for %s message predicate, expected ValueTypeStr", rightLiteral.ValueType(), op)
	}

	return rightLiteral.Literal.(datatype.StringLiteral).Value(), nil
}
