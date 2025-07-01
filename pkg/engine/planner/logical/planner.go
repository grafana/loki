package logical

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

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
		builder *Builder
		err     error
	)

	switch e := params.GetExpression().(type) {
	case syntax.LogSelectorExpr:
		builder, err = buildPlanForLogQuery(e, params, false, 0)
	case syntax.SampleExpr:
		builder, err = buildPlanForSampleQuery(e, params)
	default:
		err = fmt.Errorf("unexpected expression type (%T)", e)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to convert AST into logical plan: %w", err)
	}

	return builder.ToPlan()
}

// buildPlanForLogQuery builds logical plan operations by traversing [syntax.LogSelectorExpr]
// isMetricQuery should be set to true if this expr is encountered when processing a [syntax.SampleExpr].
// rangeInterval should be set to a non-zero value if the query contains [$range].
func buildPlanForLogQuery(expr syntax.LogSelectorExpr, params logql.Params, isMetricQuery bool, rangeInterval time.Duration) (*Builder, error) {
	var (
		err        error
		selector   Value
		predicates []Value
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
		case *syntax.LabelFilterExpr:
			if val, innerErr := convertLabelFilter(e.LabelFilterer); innerErr != nil {
				err = innerErr
			} else {
				predicates = append(predicates, val)
			}
			return true
		case *syntax.LineParserExpr, *syntax.LogfmtParserExpr, *syntax.LogfmtExpressionParserExpr, *syntax.JSONExpressionParserExpr,
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
			Selector: selector,
			Shard:    shard,
		},
	)

	// Metric queries currently do not expect the logs to be sorted by timestamp.
	if !isMetricQuery {
		// SORT -> SortMerge
		direction := params.Direction()
		ascending := direction == logproto.FORWARD
		builder = builder.Sort(*timestampColumnRef(), ascending, false)
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

	// Metric queries do not apply a limit.
	if !isMetricQuery {
		// LIMIT -> Limit
		limit := params.Limit()
		builder = builder.Limit(0, limit)
	}

	return builder, nil
}

func buildPlanForSampleQuery(e syntax.SampleExpr, params logql.Params) (*Builder, error) {
	if params.Step() > 0 {
		return nil, fmt.Errorf("only instant metric queries are supported: %w", errUnimplemented)
	}

	var (
		err error

		rangeAggType  types.RangeAggregationType
		rangeInterval time.Duration

		vecAggType types.VectorAggregationType
		groupBy    []ColumnRef
	)

	e.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.RangeAggregationExpr:
			// only count operation is supported for range aggregation.
			// offsets are not yet supported.
			if e.Operation != syntax.OpRangeTypeCount || e.Left.Offset != 0 {
				err = errUnimplemented
				return false
			}

			rangeAggType = types.RangeAggregationTypeCount
			rangeInterval = e.Left.Interval
			return false // do not traverse log range query

		case *syntax.VectorAggregationExpr:
			// only sum operation is supported for vector aggregation
			// grouping by atleast one label is required.
			if e.Operation != syntax.OpTypeSum ||
				e.Grouping == nil || len(e.Grouping.Groups) == 0 || e.Grouping.Without {
				err = errUnimplemented
				return false
			}

			vecAggType = types.VectorAggregationTypeSum
			groupBy = make([]ColumnRef, 0, len(e.Grouping.Groups))
			for _, group := range e.Grouping.Groups {
				groupBy = append(groupBy, *NewColumnRef(group, types.ColumnTypeAmbiguous))
			}

			return true
		default:
			err = errUnimplemented
			return false // do not traverse children
		}
	})
	if err != nil {
		return nil, err
	}

	if rangeAggType == types.RangeAggregationTypeInvalid || vecAggType == types.VectorAggregationTypeInvalid {
		return nil, errUnimplemented
	}

	logSelectorExpr, err := e.Selector()
	if err != nil {
		return nil, err
	}

	builder, err := buildPlanForLogQuery(logSelectorExpr, params, true, rangeInterval)
	if err != nil {
		return nil, err
	}

	builder = builder.RangeAggregation(
		nil, rangeAggType, params.Start(), params.End(), params.Step(), rangeInterval,
	).VectorAggregation(groupBy, vecAggType)

	return builder, nil
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
			Right: NewLiteral(start),
			Op:    types.BinaryOpGte,
		},
		{
			Left:  timestampColumnRef(),
			Right: NewLiteral(end),
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
