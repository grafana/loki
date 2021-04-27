package logql

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/prometheus/prometheus/pkg/labels"
)

// Filter is a line filter sent to a querier to filter out log line.
type Filter func([]byte) bool

// Expr is a LogQL expression.
type Expr interface {
	Filter() (Filter, error)
	Matchers() []*labels.Matcher
}

type matchersExpr struct {
	matchers []*labels.Matcher
}

func (e *matchersExpr) Matchers() []*labels.Matcher {
	return e.matchers
}

func (e *matchersExpr) Filter() (Filter, error) {
	return nil, nil
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
		return nil, fmt.Errorf("unknow matcher: %v", e.match)
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
