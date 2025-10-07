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
func BuildPlan(params logql.Params) (*Plan, error) {
	var (
		value Value
		err   error
	)

	switch e := params.GetExpression().(type) {
	case syntax.LogSelectorExpr:
		value, err = buildPlanForLogQuery(e, params, false, 0)
	case syntax.SampleExpr:
		value, err = buildPlanForSampleQuery(e, params)
	default:
		err = fmt.Errorf("unexpected expression type (%T)", e)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to convert AST into logical plan: %w", err)
	}

	return convertToPlan(value)
}

// buildPlanForLogQuery builds logical plan operations by traversing [syntax.LogSelectorExpr]
// isMetricQuery should be set to true if this expr is encountered when processing a [syntax.SampleExpr].
// rangeInterval should be set to a non-zero value if the query contains [$range].
func buildPlanForLogQuery(
	expr syntax.LogSelectorExpr,
	params logql.Params,
	isMetricQuery bool,
	rangeInterval time.Duration,
) (Value, error) {
	var (
		err      error
		selector Value

		// parse statements in LogQL introduce additional ambiguouity, requiring post
		// parse filters to be tracked separately, and not included in maketable predicates
		predicates          []Value
		postParsePredicates []Value
		hasLogfmtParser     bool
		hasJSONParser       bool
	)

	// TODO(chaudum): Implement a Walk function that can return an error
	expr.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.PipelineExpr:
			// [PipelineExpr] is a container for other expressions, nothing to do here.
			return true
		case *syntax.MatchersExpr:
			selector = convertLabelMatchers(e.Matchers())
			return true
		case *syntax.LineFilterExpr:
			predicates = append(predicates, convertLineFilterExpr(e))
			// We do not want to traverse the AST further down, because line filter expressions can be nested,
			// which would lead to multiple predicates of the same expression.
			return false // do not traverse children
		case *syntax.LogfmtParserExpr:
			// TODO: support --strict and --keep-empty
			if e.Strict {
				err = errUnimplemented
				return false
			}
			if e.KeepEmpty {
				err = errUnimplemented
				return false
			}

			hasLogfmtParser = true
			return true // continue traversing to find label filters
		case *syntax.LineParserExpr:
			switch e.Op {
			case syntax.OpParserTypeJSON:
				hasJSONParser = true
				return true
			case syntax.OpParserTypeRegexp, syntax.OpParserTypeUnpack, syntax.OpParserTypePattern:
				// keeping these as a distinct cases so we remember to implement them later
				err = errUnimplemented
				return false
			default:
				err = errUnimplemented
				return false
			}
		case *syntax.LabelFilterExpr:
			if val, innerErr := convertLabelFilter(e.LabelFilterer); innerErr != nil {
				err = innerErr
			} else {
				if !hasLogfmtParser && !hasJSONParser {
					predicates = append(predicates, val)
				} else {
					postParsePredicates = append(postParsePredicates, val)
				}
			}
			return true
			//TODO Support logfmt and json expression parset expressions
		case *syntax.LogfmtExpressionParserExpr, *syntax.JSONExpressionParserExpr,
			*syntax.LineFmtExpr, *syntax.LabelFmtExpr,
			*syntax.KeepLabelsExpr, *syntax.DropLabelsExpr:
			err = errUnimplemented
			return false // do not traverse children
		default:
			err = errUnimplemented
			return false // do not traverse children
		}
	})
	if err != nil {
		return nil, err
	}

	shard, err := parseShards(params.Shards())
	if err != nil {
		return nil, fmt.Errorf("failed to parse shard: %w", err)
	}

	// MAKETABLE -> DataObjScan
	builder := NewBuilder(
		&MakeTable{
			Selector:   selector,
			Predicates: predicates,
			Shard:      shard,
		},
	)

	direction := params.Direction()
	if !isMetricQuery && direction == logproto.FORWARD {
		return nil, fmt.Errorf("forward search log queries are not supported: %w", errUnimplemented)
	}

	// SELECT -> Filter
	start := params.Start()
	end := params.End()
	// extend search by rangeInterval to be able to include entries belonging to the [$range] interval.
	for _, value := range convertQueryRangeToPredicates(start.Add(-rangeInterval), end) {
		builder = builder.Select(value)
	}

	for _, value := range predicates {
		builder = builder.Select(value)
	}

	// TODO: there's a subtle bug here, as it is actually possible to have both a logfmt parser and a json parser
	// for example, the query `{app="foo"} | json | line_format "{{.nested_json}}" | json ` is valid, and will need
	// multiple parse stages. We will handle thid in a future PR.
	if hasLogfmtParser {
		builder = builder.Parse(ParserLogfmt)
	}
	if hasJSONParser {
		builder = builder.Parse(ParserJSON)
	}
	for _, value := range postParsePredicates {
		builder = builder.Select(value)
	}

	// Metric queries do not apply a limit.
	if !isMetricQuery {
		// SORT -> SortMerge
		// We always sort DESC. ASC timestamp sorting is not supported for logs
		// queries, and metric queries do not need sorting.
		builder = builder.Sort(*timestampColumnRef(), false, false)

		// LIMIT -> Limit
		limit := params.Limit()
		builder = builder.Limit(0, limit)
	}

	return builder.Value(), nil
}

func walkRangeAggregation(e *syntax.RangeAggregationExpr, params logql.Params) (Value, error) {
	// offsets are not yet supported.
	if e.Left.Offset != 0 {
		return nil, errUnimplemented
	}

	logSelectorExpr, err := e.Selector()
	if err != nil {
		return nil, err
	}

	rangeInterval := e.Left.Interval

	logQuery, err := buildPlanForLogQuery(logSelectorExpr, params, true, rangeInterval)
	if err != nil {
		return nil, err
	}

	var rangeAggType types.RangeAggregationType
	switch e.Operation {
	case syntax.OpRangeTypeCount:
		rangeAggType = types.RangeAggregationTypeCount
	case syntax.OpRangeTypeRate:
		rangeAggType = types.RangeAggregationTypeCount
	case syntax.OpRangeTypeSum:
		rangeAggType = types.RangeAggregationTypeSum
	//case syntax.OpRangeTypeMax:
	//	rangeAggType = types.RangeAggregationTypeMax
	//case syntax.OpRangeTypeMin:
	//	rangeAggType = types.RangeAggregationTypeMin
	default:
		return nil, errUnimplemented
	}

	rangeAggregation := &RangeAggregation{
		Table: logQuery,

		Operation:     rangeAggType,
		PartitionBy:   nil,
		Start:         params.Start(),
		End:           params.End(),
		Step:          params.Step(),
		RangeInterval: rangeInterval,
	}

	switch e.Operation {
	case syntax.OpRangeTypeCount, syntax.OpRangeTypeSum, syntax.OpRangeTypeMax, syntax.OpRangeTypeMin:
		return rangeAggregation, nil
	case syntax.OpRangeTypeRate:
		return &BinOp{
			Left: rangeAggregation,
			Right: &Literal{
				Literal: NewLiteral(params.Interval().Seconds()),
			},
			Op: types.BinaryOpDiv,
		}, nil

	default:
		return nil, errUnimplemented
	}
}

func walkVectorAggregation(e *syntax.VectorAggregationExpr, params logql.Params) (Value, error) {
	// grouping by at least one label is required.
	if e.Grouping == nil || len(e.Grouping.Groups) == 0 || e.Grouping.Without {
		return nil, errUnimplemented
	}

	left, err := walk(e.Left, params)
	if err != nil {
		return nil, err
	}

	vecAggType := types.VectorAggregationTypeSum

	groupBy := make([]ColumnRef, 0, len(e.Grouping.Groups))
	for _, group := range e.Grouping.Groups {
		groupBy = append(groupBy, *NewColumnRef(group, types.ColumnTypeAmbiguous))
	}

	return &VectorAggregation{
		Table:     left,
		GroupBy:   groupBy,
		Operation: vecAggType,
	}, nil
}

func hasNonMathExpressionChild(n Value) bool {
	if _, ok := n.(*VectorAggregation); ok {
		return true
	}

	if _, ok := n.(*RangeAggregation); ok {
		return true
	}

	if b, ok := n.(*BinOp); ok {
		return hasNonMathExpressionChild(b.Left) || hasNonMathExpressionChild(b.Right)
	}

	return false
}

func walkBinOp(e *syntax.BinOpExpr, params logql.Params) (Value, error) {
	left, err := walk(e.SampleExpr, params)
	if err != nil {
		return nil, err
	}
	right, err := walk(e.RHS, params)
	if err != nil {
		return nil, err
	}

	op := convertBinaryArithmeticOp(e.Op)
	if op == types.BinaryOpInvalid {
		return nil, errUnimplemented
	}

	// this is to check that there is only one non-literal input on either side, otherwise it is not implemented yet.
	// TODO remove when all MathExpression pipeline can read multiple inputs
	if hasNonMathExpressionChild(left) && hasNonMathExpressionChild(right) {
		return nil, errUnimplemented
	}

	return &BinOp{
		Left:  left,
		Right: right,
		Op:    op,
	}, nil
}

func walkLiteral(e *syntax.LiteralExpr, params logql.Params) (Value, error) {
	return &Literal{
		NewLiteral(e.Val),
	}, nil
}

func walk(e syntax.Expr, params logql.Params) (Value, error) {
	switch e := e.(type) {
	case *syntax.RangeAggregationExpr:
		return walkRangeAggregation(e, params)
	case *syntax.VectorAggregationExpr:
		return walkVectorAggregation(e, params)
	case *syntax.BinOpExpr:
		return walkBinOp(e, params)
	case *syntax.LiteralExpr:
		return walkLiteral(e, params)
	}

	return nil, errUnimplemented
}

func buildPlanForSampleQuery(e syntax.SampleExpr, params logql.Params) (Value, error) {
	val, err := walk(e, params)

	// this is to check that there are both range and vector aggregations, otherwise it is not implemented yet.
	// TODO remove when all permutations of vector aggregations and range aggregations are implemented.
	hasRangeAgg := false
	hasVecAgg := false
	e.Walk(func(e syntax.Expr) bool {
		switch e.(type) {
		case *syntax.RangeAggregationExpr:
			hasRangeAgg = true
			return false
		case *syntax.VectorAggregationExpr:
			hasVecAgg = true
		}
		return true
	})
	if !hasRangeAgg || !hasVecAgg {
		return nil, errUnimplemented
	}

	return val, err
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

func convertBinaryArithmeticOp(op string) types.BinaryOp {
	switch op {
	case syntax.OpTypeAdd:
		return types.BinaryOpAdd
	case syntax.OpTypeSub:
		return types.BinaryOpSub
	case syntax.OpTypeMul:
		return types.BinaryOpMul
	case syntax.OpTypeDiv:
		return types.BinaryOpDiv
	case syntax.OpTypeMod:
		return types.BinaryOpMod
	case syntax.OpTypePow:
		return types.BinaryOpPow
	default:
		return types.BinaryOpInvalid
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
