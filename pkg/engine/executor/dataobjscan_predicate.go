package executor

import (
	"fmt"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// buildLogsPredicate builds a logs predicate from an expression.
func buildLogsPredicate(_ physical.Expression) (dataobj.LogsPredicate, error) {
	// TODO(rfratto): implement converting expressions into logs predicates.
	//
	// There's a few challenges here:
	//
	// - Expressions do not cleanly map to [dataobj.LogsPredicate]s. For example,
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
	return nil, fmt.Errorf("logs predicate conversion is not supported")
}

// The planner should only push down predicates that are binary expressions with LHS being a ColumnExpression and RHS being a LiteralExpression (or ColumnExpression, if we want to support that).
// Initially, we only support the following predicates in mapping
// TODO(owen-d): this can go away when we use dataset.Reader & dataset.Predicate directly

func mapPredicate(expr physical.Expression) (dataobj.Predicate, error) {
	switch e := expr.(type) {
	case *physical.BinaryExpr:
		if e.Left.Type() != physical.ExprTypeColumn {
			return nil, fmt.Errorf("unsupported predicate, expected column ref on LHS: %s", expr.String())
		}

		left := e.Left.(*physical.ColumnExpr)
		switch left.Ref.Type {
		case types.ColumnTypeBuiltin:
			if left.Ref.Column == types.ColumnNameBuiltinTimestamp {
				return mapTimestampPredicate(e)
			}
			return nil, fmt.Errorf("unsupported builtin column in predicate (only timestamp is supported for now): %s", left.Ref.Column)
		case types.ColumnTypeMetadata:
			// return mapMetadataPredicate(left, e.Right)
			panic("unimplemented")
		default:
			return nil, fmt.Errorf("unsupported column ref type (%T) in predicate: %s", left.Ref, left.Ref.String())
		}
	default:
		return nil, fmt.Errorf("unsupported expression type (%T) in predicate: %s", expr, expr.String())
	}
}

func mapTimestampPredicate(expr physical.Expression) (dataobj.TimeRangePredicate[dataobj.LogsPredicate], error) {
	m := newTimestampPredicateMapper()
	if err := m.verify(expr); err != nil {
		return dataobj.TimeRangePredicate[dataobj.LogsPredicate]{}, err
	}

	binop := expr.(*physical.BinaryExpr)
	err := m.update(binop.Op, binop.Right)

	return m.res, err
}

func newTimestampPredicateMapper() *timestampPredicateMapper {
	open := dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
		StartTime:    time.Unix(0, math.MinInt64),
		EndTime:      time.Unix(0, math.MaxInt64),
		IncludeStart: true,
		IncludeEnd:   true,
	}
	return &timestampPredicateMapper{
		res: open,
	}
}

type timestampPredicateMapper struct {
	res dataobj.TimeRangePredicate[dataobj.LogsPredicate]
}

// ensures the LHS is a timestamp column reference
// and the RHS is either a literal or a binary expression
func (m *timestampPredicateMapper) verify(expr physical.Expression) error {
	binop, ok := expr.(*physical.BinaryExpr)
	if !ok {
		return fmt.Errorf("unsupported expression type: %T", expr)
	}

	switch binop.Op {
	case types.BinaryOpEq, types.BinaryOpGt, types.BinaryOpGte, types.BinaryOpLt, types.BinaryOpLte:
	default:
		return fmt.Errorf("unsupported operator: %s", binop.Op)
	}

	lhs, ok := binop.Left.(*physical.ColumnExpr)
	if !ok {
		return fmt.Errorf("unsupported LHS type: %T", binop.Left)
	}

	if lhs.Ref.Column != types.ColumnNameBuiltinTimestamp {
		return fmt.Errorf("unsupported LHS column: %s", lhs.Ref.Column)
	}

	switch rhs := binop.Right.(type) {
	case *physical.LiteralExpr:
		if rhs.ValueType() != types.ValueTypeTimestamp {
			return fmt.Errorf("unsupported literal type: %s", rhs.ValueType())
		}
		return nil
	case *physical.BinaryExpr:
		return m.verify(binop.Right)
	default:
		return fmt.Errorf("unsupported RHS type: %T", binop.Right)
	}

}

// need to test the following patterns:
// 1) timestamp <op> <literal>
// e.g. timestamp > 1
// 2) timestamp <op> (binop <timestamp> <op> <literal>)
// e.g. timestamp > 1 and timestamp < 2
func (m *timestampPredicateMapper) update(op types.BinaryOp, right physical.Expression) error {
	switch right := right.(type) {
	case *physical.LiteralExpr:
		return m.rebound(op, right)
	case *physical.BinaryExpr:
		return m.update(op, right.Right)
	default:
		panic("not implemented")
	}
}

func (m *timestampPredicateMapper) rebound(op types.BinaryOp, right *physical.LiteralExpr) error {

	if right.ValueType() != types.ValueTypeTimestamp {
		return fmt.Errorf("unsupported literal type: %s", right.ValueType())
	}
	val := right.Value.Timestamp()

	switch op {

	case types.BinaryOpEq: // ts == a
		m.res.EndTime = time.Unix(0, int64(val))
		m.res.StartTime = time.Unix(0, int64(val))
		m.res.IncludeEnd = true
		m.res.IncludeStart = true
	case types.BinaryOpGt: // ts > a
		m.res.StartTime = time.Unix(0, int64(val))
		m.res.IncludeStart = false
	case types.BinaryOpGte: // ts >= a
		m.res.StartTime = time.Unix(0, int64(val))
		m.res.IncludeStart = true
	case types.BinaryOpLt: // ts < a
		m.res.EndTime = time.Unix(0, int64(val))
		m.res.IncludeEnd = false
	case types.BinaryOpLte: // ts <= a
		m.res.EndTime = time.Unix(0, int64(val))
		m.res.IncludeEnd = true
	default:
		return fmt.Errorf("unsupported operator: %s", op)
	}
	return nil
}
