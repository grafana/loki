package logql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

// Expr is the root expression which can be a SampleExpr or LogSelectorExpr
type Expr interface {
	logQLExpr() // ensure it's not implemented accidentally
	fmt.Stringer
}

// SelectParams specifies parameters passed to data selections.
type SelectParams struct {
	*logproto.QueryRequest
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectParams) LogSelector() (LogSelectorExpr, error) {
	return ParseLogSelector(s.Selector)
}

// QuerierFunc implements Querier.
type QuerierFunc func(context.Context, SelectParams) (iter.EntryIterator, error)

// Select implements Querier.
func (q QuerierFunc) Select(ctx context.Context, p SelectParams) (iter.EntryIterator, error) {
	return q(ctx, p)
}

// Querier allows a LogQL expression to fetch an EntryIterator for a
// set of matchers and filters
type Querier interface {
	Select(context.Context, SelectParams) (iter.EntryIterator, error)
}

// Filter is a function to filter logs.
type Filter func(line []byte) bool

// LogSelectorExpr is a LogQL expression filtering and returning logs.
type LogSelectorExpr interface {
	Filter() (Filter, error)
	Matchers() []*labels.Matcher
	Expr
}

type matchersExpr struct {
	matchers []*labels.Matcher
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

func (e *matchersExpr) Filter() (Filter, error) {
	return nil, nil
}

// impl Expr
func (e *matchersExpr) logQLExpr() {}

type filterExpr struct {
	left  LogSelectorExpr
	ty    labels.MatchType
	match string
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

func (e *filterExpr) Filter() (Filter, error) {
	var f func([]byte) bool
	switch e.ty {
	case labels.MatchRegexp:
		re, err := regexp.Compile(e.match)
		if err != nil {
			return nil, err
		}
		f = re.Match

	case labels.MatchNotRegexp:
		re, err := regexp.Compile(e.match)
		if err != nil {
			return nil, err
		}
		f = func(line []byte) bool {
			return !re.Match(line)
		}

	case labels.MatchEqual:
		mb := []byte(e.match)
		f = func(line []byte) bool {
			return bytes.Contains(line, mb)
		}

	case labels.MatchNotEqual:
		mb := []byte(e.match)
		f = func(line []byte) bool {
			return !bytes.Contains(line, mb)
		}

	default:
		return nil, fmt.Errorf("unknown matcher: %v", e.match)
	}
	next, ok := e.left.(*filterExpr)
	if ok {
		nextFilter, err := next.Filter()
		if err != nil {
			return nil, err
		}
		return func(line []byte) bool {
			return nextFilter(line) && f(line)
		}, nil
	}
	return f, nil
}

// impl Expr
func (e *filterExpr) logQLExpr() {}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
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
	sb.WriteString("(")
	sb.WriteString(r.left.String())
	sb.WriteString(")")
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
	OpTypeSum           = "sum"
	OpTypeAvg           = "avg"
	OpTypeMax           = "max"
	OpTypeMin           = "min"
	OpTypeCount         = "count"
	OpTypeStddev        = "stddev"
	OpTypeStdvar        = "stdvar"
	OpTypeBottomK       = "bottomk"
	OpTypeTopK          = "topk"
	OpTypeCountOverTime = "count_over_time"
	OpTypeRate          = "rate"
)

// SampleExpr is a LogQL expression filtering logs and returning metric samples.
type SampleExpr interface {
	// Selector is the LogQL selector to apply when retrieving logs.
	Selector() LogSelectorExpr
	Expr
}

// StepEvaluator evaluate a single step of a query.
type StepEvaluator interface {
	Next() (bool, int64, promql.Vector)
	// Close all resources used.
	Close() error
}

type stepEvaluator struct {
	fn    func() (bool, int64, promql.Vector)
	close func() error
}

func newStepEvaluator(fn func() (bool, int64, promql.Vector), close func() error) (StepEvaluator, error) {
	if fn == nil {
		return nil, errors.New("nil step evaluator fn")
	}

	if close == nil {
		close = func() error { return nil }
	}

	return &stepEvaluator{
		fn:    fn,
		close: close,
	}, nil
}

func (e *stepEvaluator) Next() (bool, int64, promql.Vector) {
	return e.fn()
}

func (e *stepEvaluator) Close() error {
	return e.close()
}

type rangeAggregationExpr struct {
	left      *logRange
	operation string
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

// impl Expr
func (e *rangeAggregationExpr) logQLExpr() {}

// impls Stringer
func (e *rangeAggregationExpr) String() string {
	return formatOperation(e.operation, nil, e.left.String())
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

// impl Expr
func (e *vectorAggregationExpr) logQLExpr() {}

func (e *vectorAggregationExpr) String() string {
	var params []string
	if e.params != 0 {
		params = []string{fmt.Sprintf("%d", e.params), e.left.String()}
	} else {
		params = []string{e.left.String()}
	}
	return formatOperation(e.operation, e.grouping, params...)
}

// helper used to impl Stringer for vector and range aggregations
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
