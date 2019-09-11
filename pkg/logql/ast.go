package logql

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

// Expr is the root expression which can be a SampleExpr or LogSelectorExpr
type Expr interface{}

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
	fmt.Stringer
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
		f = func(line []byte) bool {
			return bytes.Contains(line, []byte(e.match))
		}

	case labels.MatchNotEqual:
		f = func(line []byte) bool {
			return !bytes.Contains(line, []byte(e.match))
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

func mustNewRange(left LogSelectorExpr, interval time.Duration) *logRange {
	return &logRange{
		left:     left,
		interval: interval,
	}
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
	// Evaluator returns a `StepEvaluator` that can evaluate the expression step by step
	Evaluator() StepEvaluator
	// Close all resources used.
	Close() error
}

// StepEvaluator evaluate a single step of a query.
type StepEvaluator interface {
	Next() (bool, int64, promql.Vector)
}

// StepEvaluatorFn is a function to chain multiple `StepEvaluator`.
type StepEvaluatorFn func() (bool, int64, promql.Vector)

// Next implements `StepEvaluator`
func (s StepEvaluatorFn) Next() (bool, int64, promql.Vector) {
	return s()
}

type rangeAggregationExpr struct {
	left      *logRange
	operation string

	iterator RangeVectorIterator
}

func newRangeAggregationExpr(left *logRange, operation string) SampleExpr {
	return &rangeAggregationExpr{
		left:      left,
		operation: operation,
	}
}

func (e *rangeAggregationExpr) Close() error {
	if e.iterator == nil {
		return nil
	}
	return e.iterator.Close()
}

func (e *rangeAggregationExpr) Selector() LogSelectorExpr {
	return e.left.left
}

type grouping struct {
	groups  []string
	without bool
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

func (v *vectorAggregationExpr) Close() error {
	return v.left.Close()
}

func (v *vectorAggregationExpr) Selector() LogSelectorExpr {
	return v.left.Selector()
}
