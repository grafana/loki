package logical

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func timestampColumnRef() *ColumnRef {
	return &ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}
}

func logColumnRef() *ColumnRef {
	return &ColumnRef{Column: types.ColumnNameBuiltinLog, Type: types.ColumnTypeBuiltin}
}

func convertMatcherType(t labels.MatchType) types.BinaryOp {
	switch t {
	case labels.MatchEqual:
		return types.BinaryOpEq
	case labels.MatchNotEqual:
		return types.BinaryOpNeq
	case labels.MatchRegexp:
		return types.BinaryOpMatchRe
	case labels.MatchNotRegexp:
		return types.BinaryOpNotMatchRe
	}
	return types.BinaryOpInvalid
}

func convertLabelMatchers(matchers []*labels.Matcher) Value {
	var value *BinOp

	for i, matcher := range matchers {
		expr := &BinOp{
			Left:  &ColumnRef{Column: matcher.Name, Type: types.ColumnTypeLabel},
			Right: LiteralString(matcher.Value),
			Op:    convertMatcherType(matcher.Type),
		}
		if i == 0 {
			value = expr
			continue
		}
		value = &BinOp{
			Left:  value,
			Right: expr,
			Op:    types.BinaryOpAnd,
		}
	}

	return value
}

func convertLabelMatchType(op labels.MatchType) types.BinaryOp {
	switch op {
	case labels.MatchEqual:
		return types.BinaryOpMatchStr
	case labels.MatchNotEqual:
		return types.BinaryOpNotMatchStr
	case labels.MatchRegexp:
		return types.BinaryOpMatchRe
	case labels.MatchNotRegexp:
		return types.BinaryOpNotMatchRe
	default:
		panic("invalid match type")
	}
}

func convertLineMatchType(op log.LineMatchType) types.BinaryOp {
	switch op {
	case log.LineMatchEqual:
		return types.BinaryOpMatchStr
	case log.LineMatchNotEqual:
		return types.BinaryOpNotMatchStr
	case log.LineMatchRegexp:
		return types.BinaryOpMatchRe
	case log.LineMatchNotRegexp:
		return types.BinaryOpNotMatchRe
	case log.LineMatchPattern:
		return types.BinaryOpMatchPattern
	case log.LineMatchNotPattern:
		return types.BinaryOpNotMatchPattern
	default:
		panic("invalid match type")
	}
}

func convertLineFilter(filter syntax.LineFilter) Value {
	return &BinOp{
		Left:  logColumnRef(),
		Right: LiteralString(filter.Match),
		Op:    convertLineMatchType(filter.Ty),
	}
}

func convertLineFilterExpr(expr *syntax.LineFilterExpr) Value {
	if expr.Left != nil {
		op := types.BinaryOpAnd
		if expr.IsOrChild {
			op = types.BinaryOpOr
		}
		return &BinOp{
			Left:  convertLineFilterExpr(expr.Left),
			Right: convertLineFilter(expr.LineFilter),
			Op:    op,
		}
	}
	return convertLineFilter(expr.LineFilter)
}

func convertLabelFilter(expr log.LabelFilterer) (Value, error) {
	switch e := expr.(type) {
	case *log.BinaryLabelFilter:
		op := types.BinaryOpOr
		if e.And == true {
			op = types.BinaryOpAnd
		}
		left, err := convertLabelFilter(e.Left)
		if err != nil {
			return nil, err
		}
		right, err := convertLabelFilter(e.Right)
		if err != nil {
			return nil, err
		}
		return &BinOp{Left: left, Right: right, Op: op}, nil
	case *log.BytesLabelFilter:
		return nil, fmt.Errorf("not implemented: %T", e)
	case *log.NumericLabelFilter:
		return nil, fmt.Errorf("not implemented: %T", e)
	case *log.DurationLabelFilter:
		return nil, fmt.Errorf("not implemented: %T", e)
	case *log.NoopLabelFilter:
		return nil, fmt.Errorf("not implemented: %T", e)
	case *log.StringLabelFilter:
		m := e.Matcher
		return &BinOp{
			Left:  &ColumnRef{Column: m.Name, Type: types.ColumnTypeAmbiguous},
			Right: LiteralString(m.Value),
			Op:    convertLabelMatchType(m.Type),
		}, nil
	case *log.LineFilterLabelFilter:
		m := e.Matcher
		return &BinOp{
			Left:  &ColumnRef{Column: m.Name, Type: types.ColumnTypeAmbiguous},
			Right: LiteralString(m.Value),
			Op:    convertLabelMatchType(m.Type),
		}, nil
	}
	return nil, fmt.Errorf("invalid label filter %T", expr)
}

func convertQueryRangeToPredicates(start, end int64) []*BinOp {
	return []*BinOp{
		{
			Left:  timestampColumnRef(),
			Right: LiteralUint64(uint64(start)),
			Op:    types.BinaryOpGte,
		},
		{
			Left:  timestampColumnRef(),
			Right: LiteralUint64(uint64(end)),
			Op:    types.BinaryOpLt,
		},
	}
}

func ConvertToLogicalPlan(params logql.Params) (*Plan, error) {
	expr := params.GetExpression()

	var selector Value
	var predicates []Value

	// TODO(chaudum): Implement a Walk function that can return an error
	var err error

	expr.Walk(func(e syntax.Expr) {
		switch e := e.(type) {
		case *syntax.MatchersExpr:
			selector = convertLabelMatchers(e.Matchers())
		case *syntax.LineFilterExpr:
			predicates = append(predicates, convertLineFilterExpr(e))
		case *syntax.LabelFilterExpr:
			if val, innerErr := convertLabelFilter(e.LabelFilterer); innerErr != nil {
				err = innerErr
			} else {
				predicates = append(predicates, val)
			}
		}
	})

	if err != nil {
		return nil, fmt.Errorf("failed to convert AST into logical plan: %w", err)
	}

	// MAKETABLE -> DataObjScan
	builder := NewBuilder(
		&MakeTable{
			Selector: selector,
		},
	)

	// SORT -> SortMerge
	direction := params.Direction()
	ascending := direction == logproto.FORWARD
	builder = builder.Sort(*timestampColumnRef(), ascending, false)

	// SELECT -> Filter
	start := params.Start().UnixNano()
	end := params.End().UnixNano()
	for _, value := range convertQueryRangeToPredicates(start, end) {
		builder = builder.Select(value)
	}

	for _, value := range predicates {
		builder = builder.Select(value)
	}

	// LIMIT -> Limit
	limit := params.Limit()
	builder = builder.Limit(0, limit)

	plan, err := builder.ToPlan()
	return plan, err
}
