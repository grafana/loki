package logql

import (
	"bytes"
	"fmt"
	"regexp"
	"regexp/syntax"

	"github.com/prometheus/prometheus/pkg/labels"
)

// Filter is a function to filter logs.
type LineFilter interface {
	Filter(line []byte) bool
}

type negativeFilter struct {
	LineFilter
}

type andFilter struct {
	left  LineFilter
	right LineFilter
}

func (a andFilter) Filter(line []byte) bool {
	return a.left.Filter(line) && a.right.Filter(line)
}

func (n negativeFilter) Filter(line []byte) bool {
	return !n.LineFilter.Filter(line)
}

type regexpFilter struct {
	*regexp.Regexp
}

func (r regexpFilter) Filter(line []byte) bool {
	return r.Match(line)
}

type literalFilter []byte

func (l literalFilter) Filter(line []byte) bool {
	return bytes.Contains(line, l)
}

func newFilter(match string, mt labels.MatchType) (LineFilter, error) {
	switch mt {
	case labels.MatchRegexp:
		return ParseRegex(match, true)
	case labels.MatchNotRegexp:
		return ParseRegex(match, false)
	case labels.MatchEqual:
		return literalFilter(match), nil
	case labels.MatchNotEqual:
		return negativeFilter{literalFilter(match)}, nil

	default:
		return nil, fmt.Errorf("unknown matcher: %v", match)
	}
}

func ParseRegex(re string, match bool) (LineFilter, error) {
	reg, err := syntax.Parse(re, syntax.Perl)
	if err != nil {
		return nil, err
	}
	reg = reg.Simplify()

	if f, ok := simplify(reg); ok {
		return f, nil
	}
	// if reg.Op == syntax.OpAlternate || reg.Op == syntax.OpCapture {
	// 	for _, sub := range reg.Sub {
	// 		fmt.Println(sub)
	// 	}
	// }
	// attempt to improve regex with tricks

	return defaultRegex(re, match)
}

func simplify(reg *syntax.Regexp) (LineFilter, bool) {
	switch reg.Op {
	case syntax.OpAlternate:
	case syntax.OpCapture:
	case syntax.OpConcat:
		return nil, false
	case syntax.OpLiteral:
		return literalFilter([]byte(string(reg.Rune))), true
	}
	return nil, false
}

func defaultRegex(re string, match bool) (LineFilter, error) {
	reg, err := regexp.Compile(re)
	if err != nil {
		return nil, err
	}
	f := regexpFilter{reg}
	if match {
		return f, nil
	}
	return negativeFilter{LineFilter: f}, nil
}
