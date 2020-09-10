package logql

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

// Expr is the root expression which can be a SampleExpr or LogSelectorExpr
type Expr interface {
	logQLExpr() // ensure it's not implemented accidentally
	fmt.Stringer
}

type QueryParams interface {
	LogSelector() (LogSelectorExpr, error)
	GetStart() time.Time
	GetEnd() time.Time
	GetShards() []string
}

// implicit holds default implementations
type implicit struct{}

func (implicit) logQLExpr() {}

// SelectParams specifies parameters passed to data selections.
type SelectLogParams struct {
	*logproto.QueryRequest
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectLogParams) LogSelector() (LogSelectorExpr, error) {
	return ParseLogSelector(s.Selector)
}

type SelectSampleParams struct {
	*logproto.SampleQueryRequest
}

// Expr returns the SampleExpr from the SelectSampleParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) Expr() (SampleExpr, error) {
	return ParseSampleExpr(s.Selector)
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) LogSelector() (LogSelectorExpr, error) {
	expr, err := ParseSampleExpr(s.Selector)
	if err != nil {
		return nil, err
	}
	return expr.Selector(), nil
}

// Querier allows a LogQL expression to fetch an EntryIterator for a
// set of matchers and filters
type Querier interface {
	SelectLogs(context.Context, SelectLogParams) (iter.EntryIterator, error)
	SelectSamples(context.Context, SelectSampleParams) (iter.SampleIterator, error)
}

// LogSelectorExpr is a LogQL expression filtering and returning logs.
type LogSelectorExpr interface {
	Filter() (LineFilter, error)
	Matchers() []*labels.Matcher
	Expr
}

type matchersExpr struct {
	matchers []*labels.Matcher
	implicit
}

func newMatcherExpr(matchers []*labels.Matcher) LogSelectorExpr {
	return &matchersExpr{matchers: matchers}
}

func (e *matchersExpr) Matchers() []*labels.Matcher {
	return e.matchers
}

func (e *matchersExpr) String() string {
	var sb strings.Builder
	sb.WriteString("{")
	for i, m := range e.matchers {
		sb.WriteString(m.String())
		if i+1 != len(e.matchers) {
			sb.WriteString(",")
		}
	}
	sb.WriteString("}")
	return sb.String()
}

func (e *matchersExpr) Filter() (LineFilter, error) {
	return nil, nil
}

type filterExpr struct {
	left  LogSelectorExpr
	ty    labels.MatchType
	match string
	implicit
}

// NewFilterExpr wraps an existing Expr with a next filter expression.
func NewFilterExpr(left LogSelectorExpr, ty labels.MatchType, match string) LogSelectorExpr {
	return &filterExpr{
		left:  left,
		ty:    ty,
		match: match,
	}
}

func (e *filterExpr) Matchers() []*labels.Matcher {
	return e.left.Matchers()
}

func (e *filterExpr) String() string {
	var sb strings.Builder
	sb.WriteString(e.left.String())
	switch e.ty {
	case labels.MatchRegexp:
		sb.WriteString("|~")
	case labels.MatchNotRegexp:
		sb.WriteString("!~")
	case labels.MatchEqual:
		sb.WriteString("|=")
	case labels.MatchNotEqual:
		sb.WriteString("!=")
	}
	sb.WriteString(strconv.Quote(e.match))
	return sb.String()
}

func (e *filterExpr) Filter() (LineFilter, error) {
	f, err := newFilter(e.match, e.ty)
	if err != nil {
		return nil, err
	}
	if nextExpr, ok := e.left.(*filterExpr); ok {
		nextFilter, err := nextExpr.Filter()
		if err != nil {
			return nil, err
		}
		if nextFilter != nil {
			f = newAndFilter(nextFilter, f)
		}
	}

	if f == TrueFilter {
		return nil, nil
	}

	return f, nil
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(newParseError(err.Error(), 0, 0))
	}
	return m
}

type logRange struct {
	left     LogSelectorExpr
	interval time.Duration
}

// impls Stringer
func (r logRange) String() string {
	var sb strings.Builder
	sb.WriteString(r.left.String())
	sb.WriteString(fmt.Sprintf("[%v]", model.Duration(r.interval)))
	return sb.String()
}

func newLogRange(left LogSelectorExpr, interval time.Duration) *logRange {
	return &logRange{
		left:     left,
		interval: interval,
	}
}

func addFilterToLogRangeExpr(left *logRange, ty labels.MatchType, match string) *logRange {
	left.left = &filterExpr{
		left:  left.left,
		ty:    ty,
		match: match,
	}
	return left
}

const (
	// vector ops
	OpTypeSum     = "sum"
	OpTypeAvg     = "avg"
	OpTypeMax     = "max"
	OpTypeMin     = "min"
	OpTypeCount   = "count"
	OpTypeStddev  = "stddev"
	OpTypeStdvar  = "stdvar"
	OpTypeBottomK = "bottomk"
	OpTypeTopK    = "topk"

	// range vector ops
	OpRangeTypeCount     = "count_over_time"
	OpRangeTypeRate      = "rate"
	OpRangeTypeBytes     = "bytes_over_time"
	OpRangeTypeBytesRate = "bytes_rate"

	// binops - logical/set
	OpTypeOr     = "or"
	OpTypeAnd    = "and"
	OpTypeUnless = "unless"

	// binops - operations
	OpTypeAdd = "+"
	OpTypeSub = "-"
	OpTypeMul = "*"
	OpTypeDiv = "/"
	OpTypeMod = "%"
	OpTypePow = "^"

	// binops - comparison
	OpTypeCmpEQ = "=="
	OpTypeNEQ   = "!="
	OpTypeGT    = ">"
	OpTypeGTE   = ">="
	OpTypeLT    = "<"
	OpTypeLTE   = "<="
)

func IsComparisonOperator(op string) bool {
	switch op {
	case OpTypeCmpEQ, OpTypeNEQ, OpTypeGT, OpTypeGTE, OpTypeLT, OpTypeLTE:
		return true
	default:
		return false
	}
}

// IsLogicalBinOp tests whether an operation is a logical/set binary operation
func IsLogicalBinOp(op string) bool {
	switch op {
	case OpTypeOr, OpTypeAnd, OpTypeUnless:
		return true
	default:
		return false
	}
}

// SampleExpr is a LogQL expression filtering logs and returning metric samples.
type SampleExpr interface {
	// Selector is the LogQL selector to apply when retrieving logs.
	Selector() LogSelectorExpr
	Extractor() (SampleExtractor, error)
	// Operations returns the list of operations used in this SampleExpr
	Operations() []string
	Expr
}

type rangeAggregationExpr struct {
	left      *logRange
	operation string
	implicit
}

func newRangeAggregationExpr(left *logRange, operation string) SampleExpr {
	return &rangeAggregationExpr{
		left:      left,
		operation: operation,
	}
}

func (e *rangeAggregationExpr) Selector() LogSelectorExpr {
	return e.left.left
}

// impls Stringer
func (e *rangeAggregationExpr) String() string {
	return formatOperation(e.operation, nil, e.left.String())
}

// impl SampleExpr
func (e *rangeAggregationExpr) Operations() []string {
	return []string{e.operation}
}

type grouping struct {
	groups  []string
	without bool
}

// impls Stringer
func (g grouping) String() string {
	var sb strings.Builder
	if g.without {
		sb.WriteString(" without")
	} else if len(g.groups) > 0 {
		sb.WriteString(" by")
	}

	if len(g.groups) > 0 {
		sb.WriteString("(")
		sb.WriteString(strings.Join(g.groups, ","))
		sb.WriteString(")")
	}

	return sb.String()
}

type vectorAggregationExpr struct {
	left SampleExpr

	grouping  *grouping
	params    int
	operation string
	implicit
}

func mustNewVectorAggregationExpr(left SampleExpr, operation string, gr *grouping, params *string) SampleExpr {
	var p int
	var err error
	switch operation {
	case OpTypeBottomK, OpTypeTopK:
		if params == nil {
			panic(newParseError(fmt.Sprintf("parameter required for operation %s", operation), 0, 0))
		}
		if p, err = strconv.Atoi(*params); err != nil {
			panic(newParseError(fmt.Sprintf("invalid parameter %s(%s,", operation, *params), 0, 0))
		}

	default:
		if params != nil {
			panic(newParseError(fmt.Sprintf("unsupported parameter for operation %s(%s,", operation, *params), 0, 0))
		}
	}
	if gr == nil {
		gr = &grouping{}
	}
	return &vectorAggregationExpr{
		left:      left,
		operation: operation,
		grouping:  gr,
		params:    p,
	}
}

func (e *vectorAggregationExpr) Selector() LogSelectorExpr {
	return e.left.Selector()
}

func (e *vectorAggregationExpr) Extractor() (SampleExtractor, error) {
	return e.left.Extractor()
}

func (e *vectorAggregationExpr) String() string {
	var params []string
	if e.params != 0 {
		params = []string{fmt.Sprintf("%d", e.params), e.left.String()}
	} else {
		params = []string{e.left.String()}
	}
	return formatOperation(e.operation, e.grouping, params...)
}

// impl SampleExpr
func (e *vectorAggregationExpr) Operations() []string {
	return append(e.left.Operations(), e.operation)
}

type BinOpOptions struct {
	ReturnBool bool
}

type binOpExpr struct {
	SampleExpr
	RHS  SampleExpr
	op   string
	opts BinOpOptions
}

func (e *binOpExpr) String() string {
	if e.opts.ReturnBool {
		return fmt.Sprintf("%s %s bool %s", e.SampleExpr.String(), e.op, e.RHS.String())
	}
	return fmt.Sprintf("%s %s %s", e.SampleExpr.String(), e.op, e.RHS.String())
}

// impl SampleExpr
func (e *binOpExpr) Operations() []string {
	ops := append(e.SampleExpr.Operations(), e.RHS.Operations()...)
	return append(ops, e.op)
}

func mustNewBinOpExpr(op string, opts BinOpOptions, lhs, rhs Expr) SampleExpr {
	left, ok := lhs.(SampleExpr)
	if !ok {
		panic(newParseError(fmt.Sprintf(
			"unexpected type for left leg of binary operation (%s): %T",
			op,
			lhs,
		), 0, 0))
	}

	right, ok := rhs.(SampleExpr)
	if !ok {
		panic(newParseError(fmt.Sprintf(
			"unexpected type for right leg of binary operation (%s): %T",
			op,
			rhs,
		), 0, 0))
	}

	leftLit, lOk := left.(*literalExpr)
	rightLit, rOk := right.(*literalExpr)

	if IsLogicalBinOp(op) {
		if lOk {
			panic(newParseError(fmt.Sprintf(
				"unexpected literal for left leg of logical/set binary operation (%s): %f",
				op,
				leftLit.value,
			), 0, 0))
		}

		if rOk {
			panic(newParseError(fmt.Sprintf(
				"unexpected literal for right leg of logical/set binary operation (%s): %f",
				op,
				rightLit.value,
			), 0, 0))
		}
	}

	// map expr like (1+1) -> 2
	if lOk && rOk {
		return reduceBinOp(op, leftLit, rightLit)
	}

	return &binOpExpr{
		SampleExpr: left,
		RHS:        right,
		op:         op,
		opts:       opts,
	}
}

// Reduces a binary operation expression. A binop is reducible if both of its legs are literal expressions.
// This is because literals need match all labels, which is currently difficult to encode into StepEvaluators.
// Therefore, we ensure a binop can be reduced/simplified, maintaining the invariant that it does not have two literal legs.
func reduceBinOp(op string, left, right *literalExpr) *literalExpr {
	merged := mergeBinOp(
		op,
		&promql.Sample{Point: promql.Point{V: left.value}},
		&promql.Sample{Point: promql.Point{V: right.value}},
		false,
		false,
	)
	return &literalExpr{value: merged.V}
}

type literalExpr struct {
	value float64
	implicit
}

func mustNewLiteralExpr(s string, invert bool) *literalExpr {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(newParseError(fmt.Sprintf("unable to parse literal as a float: %s", err.Error()), 0, 0))
	}

	if invert {
		n = -n
	}

	return &literalExpr{
		value: n,
	}
}

func (e *literalExpr) String() string {
	return fmt.Sprintf("%f", e.value)
}

// literlExpr impls SampleExpr & LogSelectorExpr mainly to reduce the need for more complicated typings
// to facilitate sum types. We'll be type switching when evaluating them anyways
// and they will only be present in binary operation legs.
func (e *literalExpr) Selector() LogSelectorExpr           { return e }
func (e *literalExpr) Operations() []string                { return nil }
func (e *literalExpr) Filter() (LineFilter, error)         { return nil, nil }
func (e *literalExpr) Matchers() []*labels.Matcher         { return nil }
func (e *literalExpr) Extractor() (SampleExtractor, error) { return nil, nil }

// helper used to impl Stringer for vector and range aggregations
// nolint:interfacer
func formatOperation(op string, grouping *grouping, params ...string) string {
	nonEmptyParams := make([]string, 0, len(params))
	for _, p := range params {
		if p != "" {
			nonEmptyParams = append(nonEmptyParams, p)
		}
	}

	var sb strings.Builder
	sb.WriteString(op)
	if grouping != nil {
		sb.WriteString(grouping.String())
	}
	sb.WriteString("(")
	sb.WriteString(strings.Join(nonEmptyParams, ","))
	sb.WriteString(")")
	return sb.String()
}
