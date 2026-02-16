package logical

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var (
	errUnimplemented     = errors.New("query contains unimplemented features")
	unimplementedFeature = func(s string) error { return fmt.Errorf("%w: %s", errUnimplemented, s) }
)

// BuildPlan converts a LogQL query represented as [logql.Params] into a logical [Plan].
// It may return an error as second argument in case the traversal of the AST of the query fails.
func BuildPlan(ctx context.Context, params logql.Params) (*Plan, error) {
	return BuildPlanWithDeletes(ctx, params, nil)
}

func BuildPlanWithDeletes(ctx context.Context, params logql.Params, deletes []*deletion.Request) (*Plan, error) {
	var (
		value Value
		err   error
	)

	switch e := params.GetExpression().(type) {
	case syntax.LogSelectorExpr:
		value, err = buildPlanForLogQuery(ctx, e, params, false, 0, deletes)
	case syntax.SampleExpr:
		value, err = buildPlanForSampleQuery(ctx, e, params, deletes)
	default:
		err = fmt.Errorf("unexpected expression type (%T)", e)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to convert AST into logical plan: %w", err)
	}

	builder := NewBuilder(value)

	// TODO(chaudum): Make compatibility mode configurable
	builder = builder.Compat(true)

	return builder.ToPlan()
}

// buildPlanForLogQuery builds logical plan operations by traversing [syntax.LogSelectorExpr]
// isMetricQuery should be set to true if this expr is encountered when processing a [syntax.SampleExpr].
// rangeInterval should be set to a non-zero value if the query contains [$range].
func buildPlanForLogQuery(
	ctx context.Context,
	expr syntax.LogSelectorExpr,
	params logql.Params,
	isMetricQuery bool,
	rangeInterval time.Duration,
	deletes []*deletion.Request,
) (Value, error) {
	var (
		err      error
		selector Value

		// parse statements in LogQL introduce additional ambiguouity, requiring post
		// parse filters to be tracked separately, and not included in maketable predicates
		predicates       []Value
		deletePredicates []Value
		hasLogfmtParser  bool
		hasJSONParser    bool
		hasRegexParser   bool
	)

	// Do the first pass to collect the stream selector, line filters, and predicates. Only predicates listed
	// before any parse node are considered here. Position of line filters does not matter, they are all collected.
	expr.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.MatchersExpr:
			selector = convertLabelMatchers(e.Matchers())
			return true
		case *syntax.LineFilterExpr:
			predicates = append(predicates, convertLineFilterExpr(e))
			// We do not want to traverse the AST further down, because line filter expressions can be nested,
			// which would lead to multiple predicates of the same expression.
			return false // do not traverse children
		case *syntax.LogfmtParserExpr:
			hasLogfmtParser = true
			return true
		case *syntax.LineParserExpr:
			switch e.Op {
			case syntax.OpParserTypeJSON:
				hasJSONParser = true
				return true
			case syntax.OpParserTypeRegexp:
				hasRegexParser = true
				return true
			}
		case *syntax.LabelFilterExpr:
			// Collect following filters only before we met any parse stage.
			if !hasLogfmtParser && !hasJSONParser && !hasRegexParser {
				val, innerErr := convertLabelFilter(e.LabelFilterer)
				if innerErr != nil {
					err = innerErr
					return false
				}

				predicates = append(predicates, val)
			}

			return true
		}
		return true
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

	// Do we need a projection of all comlumns right after maketable?
	// builder = builder.ProjectAll(false, false)

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

	// Add all predicates as Select nodes.
	for _, value := range predicates {
		builder = builder.Select(value)
	}

	// It should be safe to append this to MakeTable predicates?
	deletePredicates, err = buildDeletePredicates(ctx, deletes, params, rangeInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to build delete predicates: %w", err)
	}

	// Adding this earlier in the pipeline to avoid expensive operations on lines that will be deleted anyway.
	for _, value := range deletePredicates {
		builder = builder.Select(value)
	}

	// Reset flags before the second pass
	hasLogfmtParser = false
	hasJSONParser = false
	hasRegexParser = false

	// TODO(chaudum): Implement a Walk function that can return an error
	expr.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.PipelineExpr:
			// [PipelineExpr] is a container for other expressions, nothing to do here.
			return true
		case *syntax.MatchersExpr:
			return true
		case *syntax.LineFilterExpr:
			return false // do not traverse children
		case *syntax.LogfmtParserExpr:
			hasLogfmtParser = true

			builder = builder.Parse(types.VariadicOpParseLogfmt, e.Strict, e.KeepEmpty)

			return true // continue traversing to find label filters
		case *syntax.LineParserExpr:
			switch e.Op {
			case syntax.OpParserTypeJSON:
				hasJSONParser = true

				// JSON has no parameters
				builder = builder.Parse(types.VariadicOpParseJSON, false, false)

				return true
			case syntax.OpParserTypeRegexp:
				hasRegexParser = true

				builder = builder.ParseRegexp(e.Param)

				return true
			case syntax.OpParserTypeUnpack, syntax.OpParserTypePattern:
				// keeping these as a distinct cases so we remember to implement them later
				err = errUnimplemented
				return false
			default:
				err = errUnimplemented
				return false
			}
		case *syntax.LabelFilterExpr:
			// Add following filters only after we met any parse stage.
			if hasLogfmtParser || hasJSONParser || hasRegexParser {
				val, innerErr := convertLabelFilter(e.LabelFilterer)
				if innerErr != nil {
					err = innerErr
					return false
				}

				builder = builder.Select(val)
			}
			return true
		case *syntax.LogfmtExpressionParserExpr, *syntax.JSONExpressionParserExpr:
			err = errUnimplemented
			return false // do not traverse children
		case *syntax.LineFmtExpr:
			err = unimplementedFeature("line_format")
			return false // do not traverse children
		case *syntax.LabelFmtExpr:
			err = unimplementedFeature("label_format")
			return false // do not traverse children
		case *syntax.KeepLabelsExpr:
			err = unimplementedFeature("keep")
			return false // do not traverse children
		case *syntax.DropLabelsExpr:
			if e.HasNamedMatchers() {
				// Example: `| drop __error__=~"Unknown Error: .*"`
				err = unimplementedFeature("drop with named matchers")
				return false // do not traverse children
			}

			dropCols := make([]Value, 0, len(e.Names()))
			for _, name := range e.Names() {
				value := NewColumnRef(name, types.ColumnTypeAmbiguous)
				dropCols = append(dropCols, value)
			}

			builder = builder.ProjectDrop(dropCols...)

			return true
		default:
			err = errUnimplemented
			return false // do not traverse children
		}
	})
	if err != nil {
		return nil, err
	}

	// Metric queries do not apply a limit.
	if !isMetricQuery {
		// We always sort DESC. ASC timestamp sorting is not supported for logs
		// queries, and metric queries do not need sorting.
		builder = builder.TopK(timestampColumnRef(), int(params.Limit()), false, false)
	}

	return builder.Value(), nil
}

func walkRangeAggregation(e *syntax.RangeAggregationExpr, wc *walkContext) (Value, error) {
	// offsets are not yet supported.
	if e.Left.Offset != 0 {
		return nil, errUnimplemented
	}

	logSelectorExpr, err := e.Selector()
	if err != nil {
		return nil, err
	}

	rangeInterval := e.Left.Interval

	logQuery, err := buildPlanForLogQuery(wc.ctx, logSelectorExpr, wc.params, true, rangeInterval, wc.deletes)
	if err != nil {
		return nil, err
	}

	builder := NewBuilder(logQuery)

	// Check for unwrap in the LogRangeExpr
	if e.Left.Unwrap != nil {
		// TODO: do we need to support multiple unwraps?
		unwrapIdentifier := e.Left.Unwrap.Identifier

		var unwrapOperation types.UnaryOp
		switch e.Left.Unwrap.Operation {
		case "":
			unwrapOperation = types.UnaryOpCastFloat
		case syntax.OpConvBytes:
			unwrapOperation = types.UnaryOpCastBytes
		case syntax.OpConvDuration, syntax.OpConvDurationSeconds:
			unwrapOperation = types.UnaryOpCastDuration
		default:
			return nil, errUnimplemented
		}

		// Unwrap turns a column into numerical `value` column, and that original column should be dropped from the result.
		builder = builder.
			Cast(unwrapIdentifier, unwrapOperation).
			ProjectDrop(&ColumnRef{
				Ref: types.ColumnRef{
					Column: unwrapIdentifier,
					Type:   types.ColumnTypeAmbiguous,
				},
			})

		// Filter out rows with any errors because rows with errors have invalid numeric values.
		builder = builder.Select(
			&BinOp{
				Left: &BinOp{
					Left:  NewColumnRef(types.ColumnNameError, types.ColumnTypeGenerated),
					Right: NewLiteral(""),
					Op:    types.BinaryOpEq,
				},
				Right: &BinOp{
					Left:  NewColumnRef(types.ColumnNameErrorDetails, types.ColumnTypeGenerated),
					Right: NewLiteral(""),
					Op:    types.BinaryOpEq,
				},
				Op: types.BinaryOpAnd,
			},
		)
	}

	var rangeAggType types.RangeAggregationType
	switch e.Operation {
	case syntax.OpRangeTypeCount:
		rangeAggType = types.RangeAggregationTypeCount
	case syntax.OpRangeTypeSum:
		rangeAggType = types.RangeAggregationTypeSum
	case syntax.OpRangeTypeMax:
		rangeAggType = types.RangeAggregationTypeMax
	case syntax.OpRangeTypeMin:
		rangeAggType = types.RangeAggregationTypeMin
	case syntax.OpRangeTypeAvg:
		rangeAggType = types.RangeAggregationTypeAvg
	// case syntax.OpRangeTypeBytesRate:
	//	rangeAggType = types.RangeAggregationTypeBytes // bytes_rate is implemented as bytes_over_time/$interval
	case syntax.OpRangeTypeRate:
		if e.Left.Unwrap != nil {
			rangeAggType = types.RangeAggregationTypeSum // rate of an unwrap is implemented as sum_over_time/$interval
		} else {
			rangeAggType = types.RangeAggregationTypeCount // rate is implemented as count_over_time/$interval
		}
	default:
		return nil, errUnimplemented
	}

	builder = builder.RangeAggregation(
		convertGrouping(e.Grouping), rangeAggType, wc.params.Start(), wc.params.End(), wc.params.Step(), rangeInterval,
	)

	switch e.Operation {
	// case syntax.OpRangeTypeBytesRate:
	//	// bytes_rate is implemented as bytes_over_time/$interval
	//	builder = builder.BinOpRight(types.BinaryOpDiv, NewLiteral(rangeInterval.Seconds()))
	case syntax.OpRangeTypeRate:
		// rate is implemented as count_over_time/$interval
		builder = builder.BinOpRight(types.BinaryOpDiv, NewLiteral(rangeInterval.Seconds()))
	}

	return builder.Value(), nil
}

func walkVectorAggregation(e *syntax.VectorAggregationExpr, wc *walkContext) (Value, error) {
	left, err := walk(e.Left, wc)
	if err != nil {
		return nil, err
	}

	vecAggType := convertVectorAggregationType(e.Operation)
	if vecAggType == types.VectorAggregationTypeInvalid {
		return nil, errUnimplemented
	}

	return &VectorAggregation{
		Table:     left,
		Grouping:  convertGrouping(e.Grouping),
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

func walkBinOp(e *syntax.BinOpExpr, wc *walkContext) (Value, error) {
	left, err := walk(e.SampleExpr, wc)
	if err != nil {
		return nil, err
	}
	right, err := walk(e.RHS, wc)
	if err != nil {
		return nil, err
	}

	op := convertBinaryArithmeticOp(e.Op)
	if op == types.BinaryOpInvalid {
		return nil, errUnimplemented
	}

	// this is to check that there is only one non-literal input on either side, otherwise it is not implemented yet.
	// TODO remove when inner joins on timestamp are implemented
	if hasNonMathExpressionChild(left) && hasNonMathExpressionChild(right) {
		return nil, errUnimplemented
	}

	return &BinOp{
		Left:  left,
		Right: right,
		Op:    op,
	}, nil
}

func walkLiteral(e *syntax.LiteralExpr, _ *walkContext) (Value, error) {
	return NewLiteral(e.Val), nil
}

type walkContext struct {
	ctx     context.Context
	params  logql.Params
	deletes []*deletion.Request
}

func walk(e syntax.Expr, wc *walkContext) (Value, error) {
	switch e := e.(type) {
	case *syntax.RangeAggregationExpr:
		return walkRangeAggregation(e, wc)
	case *syntax.VectorAggregationExpr:
		return walkVectorAggregation(e, wc)
	case *syntax.BinOpExpr:
		return walkBinOp(e, wc)
	case *syntax.LiteralExpr:
		return walkLiteral(e, wc)
	}

	return nil, errUnimplemented
}

func buildPlanForSampleQuery(ctx context.Context, e syntax.SampleExpr, params logql.Params, deletes []*deletion.Request) (Value, error) {
	return walk(e, &walkContext{
		ctx:     ctx,
		params:  params,
		deletes: deletes,
	})
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

func convertVectorAggregationType(op string) types.VectorAggregationType {
	switch op {
	case syntax.OpTypeSum:
		return types.VectorAggregationTypeSum
	case syntax.OpTypeCount:
		return types.VectorAggregationTypeCount
	case syntax.OpTypeMax:
		return types.VectorAggregationTypeMax
	case syntax.OpTypeMin:
		return types.VectorAggregationTypeMin
	case syntax.OpTypeAvg:
		return types.VectorAggregationTypeAvg
	default:
		return types.VectorAggregationTypeInvalid
	}
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
			Right: NewLiteral(types.Timestamp(start.UTC().UnixNano())),
			Op:    types.BinaryOpGte,
		},
		{
			Left:  timestampColumnRef(),
			Right: NewLiteral(types.Timestamp(end.UTC().UnixNano())),
			Op:    types.BinaryOpLt,
		},
	}
}

// convertGrouping converts [syntax.Grouping] structure into a list of columns and a grouping mode.
func convertGrouping(g *syntax.Grouping) Grouping {
	var columns []ColumnRef

	if g == nil {
		return Grouping{
			Columns: columns,
			Without: true,
		}
	}

	if g.Groups != nil {
		columns = make([]ColumnRef, len(g.Groups))
		for i, group := range g.Groups {
			columns[i] = *NewColumnRef(group, types.ColumnTypeAmbiguous)
		}
	}

	return Grouping{
		Columns: columns,
		Without: g.Without,
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

// buildDeletePredicates builds predicates to drop log lines matching delete requests.
// Each delete request maps to a single predicate.
//
// Predicates built here will not be pushed down during optimization, because the predicate
// contains column refs to:
// - (timestamp & label columns) from streams section
// - (timestamp, message & optionally metadata columns) from logs section
// which cannot be evaluated as a single predicate at the storage level.
//
// There is not need to explicitly signal the optimizer to not push these predicates down,
// canApplyPredicate already correctly handles this by returning an error if there is a label column ref.
func buildDeletePredicates(ctx context.Context, deletes []*deletion.Request, params logql.Params, rangeInterval time.Duration) ([]Value, error) {
	_, region := xcap.StartRegion(ctx, "buildDeletePredicates")
	defer region.End()

	var predicates []Value

	// TODO: consider offset in time range calculations when its supported.
	qStart := params.Start().Add(-rangeInterval).UnixNano()
	qEnd := params.End().UnixNano()

	for _, d := range deletes {
		if qStart > d.End || qEnd < d.Start {
			// delete request does not overlap with query time range. Skip.
			continue
		}

		// Each predicate composed from a delete request will have:
		// - time range predicate
		// - stream selector
		// - options line, label filters
		//
		// any line matching all of them should be dropped.
		// keep lines matching: !( time_range AND selector AND filters )
		// equivalent to: !(time_range) OR !(selector) OR !(filters)
		// equivalent to: ts < start OR ts > end OR !(selector) OR !(filters)

		expr, err := syntax.ParseLogSelector(d.Selector, true)
		if err != nil {
			return nil, err
		}

		var (
			selector Value
			filters  Value
		)

		// TODO: selector & filters expressions can be further optimized:
		// they are of the form  NOT ( F1 AND F2 AND ... )
		// equivalent to: NOT F1 OR NOT F2 OR ...
		// individual filters can be negated to consume the NOT operator.
		addFilter := func(f Value) {
			if filters == nil {
				filters = f
			} else {
				filters = &BinOp{
					Left:  filters,
					Right: f,
					Op:    types.BinaryOpAnd,
				}
			}
		}

		expr.Walk(func(e syntax.Expr) bool {
			switch e := e.(type) {
			case *syntax.PipelineExpr:
				// [PipelineExpr] is a container for other expressions, nothing to do here.
				return true
			case *syntax.MatchersExpr:
				selector = convertLabelMatchers(e.Matchers())
				return true
			case *syntax.LineFilterExpr:
				addFilter(convertLineFilterExpr(e))
				return true
			case *syntax.LabelFilterExpr:
				val, innerErr := convertLabelFilter(e.LabelFilterer)
				if innerErr != nil {
					err = innerErr
					return false
				}

				addFilter(val)
				return true
			default:
				// TODO: not all expressions are supported in delete selectors yet.
				// e.g: parsers, formatters, keep/drop labels, etc.
				err = unimplementedFeature("delete request with unsupported stages")
				return false
			}
		})
		if err != nil {
			return nil, err
		}

		var timeRangePredicate *BinOp
		switch {
		case d.Start <= qStart && d.End >= qEnd:
			// delete request entirely covers query time range.
			// keep line if other conditions do not match.
		case d.Start >= qStart && d.End <= qEnd:
			// delete request is entirely within query time range.
			// keep line if ts < del_req_start OR ts > del_req_end
			timeRangePredicate = &BinOp{
				Left: &BinOp{
					Left:  timestampColumnRef(),
					Right: NewLiteral(types.Timestamp(d.Start)),
					Op:    types.BinaryOpLt,
				},
				Right: &BinOp{
					Left:  timestampColumnRef(),
					Right: NewLiteral(types.Timestamp(d.End)),
					Op:    types.BinaryOpGt,
				},
				Op: types.BinaryOpOr,
			}
		case d.Start <= qStart:
			// keep line if ts > del_req_end
			timeRangePredicate = &BinOp{
				Left:  timestampColumnRef(),
				Right: NewLiteral(types.Timestamp(d.End)),
				Op:    types.BinaryOpGt,
			}
		case d.End >= qEnd:
			// keep line if ts < del_req_start
			timeRangePredicate = &BinOp{
				Left:  timestampColumnRef(),
				Right: NewLiteral(types.Timestamp(d.Start)),
				Op:    types.BinaryOpLt,
			}
		}

		// Keep if not matching selector
		var p Value = &UnaryOp{
			Op:    types.UnaryOpNot,
			Value: selector,
		}

		if timeRangePredicate != nil {
			p = &BinOp{
				Left:  timeRangePredicate,
				Right: p,
				Op:    types.BinaryOpOr,
			}
		}

		if filters != nil {
			// OR not matching filters
			p = &BinOp{
				Left: p,
				Right: &UnaryOp{
					Op:    types.UnaryOpNot,
					Value: filters,
				},
				Op: types.BinaryOpOr,
			}
		}

		predicates = append(predicates, p)
	}

	region.Record(xcap.StatDeletePredicates.Observe(int64(len(predicates))))
	return predicates, nil
}
