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

type notFilter struct {
	LineFilter
}

func (n notFilter) Filter(line []byte) bool {
	return !n.LineFilter.Filter(line)
}

type andFilter struct {
	left  LineFilter
	right LineFilter
}

func (a andFilter) Filter(line []byte) bool {
	return a.left.Filter(line) && a.right.Filter(line)
}

type orFilter struct {
	left  LineFilter
	right LineFilter
}

func (a orFilter) Filter(line []byte) bool {
	return a.left.Filter(line) || a.right.Filter(line)
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
		return notFilter{literalFilter(match)}, nil

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

	// attempt to improve regex with tricks
	f, ok := simplify(reg)
	if !ok {
		return defaultRegex(re, match)
	}
	if match {
		return f, nil
	}
	return notFilter{LineFilter: f}, nil
}

func simplify(reg *syntax.Regexp) (LineFilter, bool) {
	switch reg.Op {
	case syntax.OpAlternate:
		return simplifyAlternate(reg)
	case syntax.OpConcat:
		return simplifyConcat(reg)
	case syntax.OpCapture:
		clearCapture(reg)
		return simplify(reg)
	case syntax.OpLiteral:
		return literalFilter([]byte(string(reg.Rune))), true
	}
	return nil, false
}

func clearCapture(regs ...*syntax.Regexp) {
	for _, r := range regs {
		if r.Op == syntax.OpCapture {
			*r = *r.Sub[0]
		}
	}
}

func simplifyAlternate(reg *syntax.Regexp) (LineFilter, bool) {
	clearCapture(reg.Sub...)
	// attempt to simplify the first leg
	f, ok := simplify(reg.Sub[0])
	if !ok {
		return nil, false
	}
	// merge the rest of the legs
	for i := 1; i < len(reg.Sub); i++ {
		f2, ok := simplify(reg.Sub[i])
		if !ok {
			return nil, false
		}
		f = orFilter{
			left:  f,
			right: f2,
		}
	}
	return f, true
}

func simplifyConcat(reg *syntax.Regexp) (LineFilter, bool) {
	clearCapture(reg.Sub...)
	// we can improve foo.* .*foo.* .*foo
	if len(reg.Sub) > 3 {
		return nil, false
	}
	var literal []byte
	for _, sub := range reg.Sub {
		if sub.Op == syntax.OpLiteral {
			// only one literal
			if literal != nil {
				return nil, false
			}
			literal = []byte(string(sub.Rune))
			continue
		}
		if sub.Op != syntax.OpStar {
			return nil, false
		}
	}

	return literalFilter(literal), true
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
	return notFilter{LineFilter: f}, nil
}
