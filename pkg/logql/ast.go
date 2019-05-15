package logql

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/grafana/loki/pkg/iter"
	"github.com/prometheus/prometheus/pkg/labels"
)

// QuerierFunc implements Querier.
type QuerierFunc func([]*labels.Matcher) (iter.EntryIterator, error)

// Query implements Querier.
func (q QuerierFunc) Query(ms []*labels.Matcher) (iter.EntryIterator, error) {
	return q(ms)
}

// Querier allows a LogQL expression to fetch an EntryIterator for a
// set of matchers.
type Querier interface {
	Query([]*labels.Matcher) (iter.EntryIterator, error)
}

// Expr is a LogQL expression.
type Expr interface {
	Eval(Querier) (iter.EntryIterator, error)
}

type matchersExpr struct {
	matchers []*labels.Matcher
}

func (e *matchersExpr) Eval(q Querier) (iter.EntryIterator, error) {
	return q.Query(e.matchers)
}

type filterExpr struct {
	left  Expr
	ty    labels.MatchType
	match string
}

// NewFilterExpr wraps an existing Expr with a next filter expression.
func NewFilterExpr(left Expr, ty labels.MatchType, match string) Expr {
	return &filterExpr{
		left:  left,
		ty:    ty,
		match: match,
	}
}

func (e *filterExpr) Eval(q Querier) (iter.EntryIterator, error) {
	var f func(string) bool
	switch e.ty {
	case labels.MatchRegexp:
		re, err := regexp.Compile(e.match)
		if err != nil {
			return nil, err
		}
		f = re.MatchString

	case labels.MatchNotRegexp:
		re, err := regexp.Compile(e.match)
		if err != nil {
			return nil, err
		}
		f = func(line string) bool {
			return !re.MatchString(line)
		}

	case labels.MatchEqual:
		f = func(line string) bool {
			return strings.Contains(line, e.match)
		}

	case labels.MatchNotEqual:
		f = func(line string) bool {
			return !strings.Contains(line, e.match)
		}

	default:
		return nil, fmt.Errorf("unknow matcher: %v", e.match)
	}

	left, err := e.left.Eval(q)
	if err != nil {
		return nil, err
	}

	return iter.NewFilter(f, left), nil
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}
