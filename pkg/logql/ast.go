package logql

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/grafana/loki/pkg/iter"
	"github.com/prometheus/prometheus/pkg/labels"
)

// Filter is a line filter sent to a querier to filter out log line.
type Filter func([]byte) bool

// QuerierFunc implements Querier.
type QuerierFunc func([]*labels.Matcher, Filter) (iter.EntryIterator, error)

// Query implements Querier.
func (q QuerierFunc) Query(ms []*labels.Matcher, entryFilter Filter) (iter.EntryIterator, error) {
	return q(ms, entryFilter)
}

// Querier allows a LogQL expression to fetch an EntryIterator for a
// set of matchers.
type Querier interface {
	Query([]*labels.Matcher, Filter) (iter.EntryIterator, error)
}

// Expr is a LogQL expression.
type Expr interface {
	Eval(Querier) (iter.EntryIterator, error)
	Matchers() []*labels.Matcher
}

type matchersExpr struct {
	matchers []*labels.Matcher
}

func (e *matchersExpr) Eval(q Querier) (iter.EntryIterator, error) {
	return q.Query(e.matchers, nil)
}

func (e *matchersExpr) Matchers() []*labels.Matcher {
	return e.matchers
}

type filterExpr struct {
	left  Expr
	ty    labels.MatchType
	match string
}

func (e *filterExpr) Matchers() []*labels.Matcher {
	return e.left.Matchers()
}

// NewFilterExpr wraps an existing Expr with a next filter expression.
func NewFilterExpr(left Expr, ty labels.MatchType, match string) Expr {
	return &filterExpr{
		left:  left,
		ty:    ty,
		match: match,
	}
}

func (e *filterExpr) filter() (func([]byte) bool, error) {
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
		return nil, fmt.Errorf("unknow matcher: %v", e.match)
	}
	next, ok := e.left.(*filterExpr)
	if ok {
		nextFilter, err := next.filter()
		if err != nil {
			return nil, err
		}
		return func(line []byte) bool {
			return nextFilter(line) && f(line)
		}, nil
	}
	return f, nil
}

func (e *filterExpr) Eval(q Querier) (iter.EntryIterator, error) {
	f, err := e.filter()
	if err != nil {
		return nil, err
	}
	next, err := q.Query(e.left.Matchers(), f)
	if err != nil {
		return nil, err
	}
	return next, nil
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}
