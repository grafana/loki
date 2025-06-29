package logical

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var errUnimplemented = errors.New("query contains unimplemented features")

// BuildPlan converts a LogQL query represented as [logql.Params] into a logical [Plan].
// It may return an error as second argument in case the traversal of the AST of the query fails.
func BuildPlan(query logql.Params) (*Plan, error) {
	var selector Value
	var predicates []Value

	// TODO(chaudum): Implement a Walk function that can return an error
	var err error

	expr := query.GetExpression()
	expr.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case syntax.SampleExpr:
			err = errUnimplemented
			return false // do not traverse children
		case *syntax.LineParserExpr, *syntax.LogfmtParserExpr, *syntax.LogfmtExpressionParserExpr, *syntax.JSONExpressionParserExpr:
			err = errUnimplemented
			return false // do not traverse children
		case *syntax.LineFmtExpr, *syntax.LabelFmtExpr:
			err = errUnimplemented
			return false // do not traverse children
		case *syntax.KeepLabelsExpr, *syntax.DropLabelsExpr:
			err = errUnimplemented
			return false // do not traverse children
		case *syntax.MatchersExpr:
			selector = convertLabelMatchers(e.Matchers())
		case *syntax.LineFilterExpr:
			predicates = append(predicates, convertLineFilterExpr(e))
			// We do not want to traverse the AST further down, because line filter expressions can be nested,
			// which would lead to multiple predicates of the same expression.
			return false
		case *syntax.LabelFilterExpr:
			if val, innerErr := convertLabelFilter(e.LabelFilterer); innerErr != nil {
				err = innerErr
			} else {
				predicates = append(predicates, val)
			}
		}
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to convert AST into logical plan: %w", err)
	}

	shard, err := parseShards(query.Shards())
	if err != nil {
		return nil, fmt.Errorf("failed to parse shard: %w", err)
	}

	// MAKETABLE -> DataObjScan
	builder := NewBuilder(
		&MakeTable{
			Selector: selector,
			Shard:    shard,
		},
	)

	// SORT -> SortMerge
	direction := query.Direction()
	ascending := direction == logproto.FORWARD
	builder = builder.Sort(*timestampColumnRef(), ascending, false)

	// SELECT -> Filter
	start := query.Start()
	end := query.End()
	for _, value := range convertQueryRangeToPredicates(start, end) {
		builder = builder.Select(value)
	}

	for _, value := range predicates {
		builder = builder.Select(value)
	}

	// LIMIT -> Limit
	limit := query.Limit()
	builder = builder.Limit(0, limit)

	plan, err := builder.ToPlan()
	return plan, err
}

func convertLabelMatchers(matchers []*labels.Matcher) Value {
	var value *BinOp

	for i, matcher := range matchers {
		expr := &BinOp{
			Left:  NewColumnRef(matcher.Name, types.ColumnTypeLabel),
			Right: NewLiteral(matcher.Value),
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

func convertLineFilter(filter syntax.LineFilter) Value {
	return &BinOp{
		Left:  lineColumnRef(),
		Right: NewLiteral(filter.Match),
		Op:    convertLineMatchType(filter.Ty),
	}
}

func convertLineMatchType(op log.LineMatchType) types.BinaryOp {
	switch op {
	case log.LineMatchEqual:
		return types.BinaryOpMatchSubstr
	case log.LineMatchNotEqual:
		return types.BinaryOpNotMatchSubstr
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

func timestampColumnRef() *ColumnRef {
	return NewColumnRef(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin)
}

func lineColumnRef() *ColumnRef {
	return NewColumnRef(types.ColumnNameBuiltinMessage, types.ColumnTypeBuiltin)
}

func convertLabelMatchType(op labels.MatchType) types.BinaryOp {
	switch op {
	case labels.MatchEqual:
		return types.BinaryOpEq
	case labels.MatchNotEqual:
		return types.BinaryOpNeq
	case labels.MatchRegexp:
		return types.BinaryOpMatchRe
	case labels.MatchNotRegexp:
		return types.BinaryOpNotMatchRe
	default:
		panic("invalid match type")
	}
}

func convertLabelFilter(expr log.LabelFilterer) (Value, error) {
	switch e := expr.(type) {
	case *log.BinaryLabelFilter:
		op := types.BinaryOpOr
		if e.And {
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
			Left:  NewColumnRef(m.Name, types.ColumnTypeAmbiguous),
			Right: NewLiteral(m.Value),
			Op:    convertLabelMatchType(m.Type),
		}, nil
	case *log.LineFilterLabelFilter:
		m := e.Matcher
		return &BinOp{
			Left:  NewColumnRef(m.Name, types.ColumnTypeAmbiguous),
			Right: NewLiteral(m.Value),
			Op:    convertLabelMatchType(m.Type),
		}, nil
	}
	return nil, fmt.Errorf("invalid label filter %T", expr)
}

func convertQueryRangeToPredicates(start, end time.Time) []*BinOp {
	return []*BinOp{
		{
			Left:  timestampColumnRef(),
			Right: NewLiteral(datatype.Timestamp(start.UTC().UnixNano())),
			Op:    types.BinaryOpGte,
		},
		{
			Left:  timestampColumnRef(),
			Right: NewLiteral(datatype.Timestamp(end.UTC().UnixNano())),
			Op:    types.BinaryOpLt,
		},
	}
}

func parseShards(shards []string) (*ShardInfo, error) {
	if len(shards) == 0 {
		return noShard, nil
	}
	parsed, variant, err := logql.ParseShards(shards)
	if err != nil {
		return noShard, err
	}
	if len(parsed) == 0 {
		return noShard, nil
	}
	if variant != logql.PowerOfTwoVersion {
		return noShard, fmt.Errorf("unsupported shard variant: %s", variant)
	}
	return NewShard(parsed[0].PowerOfTwo.Shard, parsed[0].PowerOfTwo.Of), nil
}
