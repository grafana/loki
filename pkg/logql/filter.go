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

func newNotFilter(f LineFilter) LineFilter {
	// not(a|b) = not(a) and not(b)
	if or, ok := f.(orFilter); ok {
		return newAndFilter(newNotFilter(or.left), newNotFilter(or.right))
	}
	return notFilter{LineFilter: f}
}

type andFilter struct {
	left  LineFilter
	right LineFilter
}

func newAndFilter(l LineFilter, r LineFilter) LineFilter {
	return andFilter{
		left:  l,
		right: r,
	}
}

func (a andFilter) Filter(line []byte) bool {
	return a.left.Filter(line) && a.right.Filter(line)
}

type orFilter struct {
	left  LineFilter
	right LineFilter
}

func newOrFilter(l LineFilter, r LineFilter) LineFilter {
	return orFilter{
		left:  l,
		right: r,
	}
}

func (a orFilter) Filter(line []byte) bool {
	return a.left.Filter(line) || a.right.Filter(line)
}

type regexpFilter struct {
	*regexp.Regexp
}

func newRegexpFilter(re string, match bool) (LineFilter, error) {
	reg, err := regexp.Compile(re)
	if err != nil {
		return nil, err
	}
	f := regexpFilter{reg}
	if match {
		return f, nil
	}
	return newNotFilter(f), nil
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
		return parseRegexpFilter(match, true)
	case labels.MatchNotRegexp:
		return parseRegexpFilter(match, false)
	case labels.MatchEqual:
		return literalFilter(match), nil
	case labels.MatchNotEqual:
		return newNotFilter(literalFilter(match)), nil

	default:
		return nil, fmt.Errorf("unknown matcher: %v", match)
	}
}

func parseRegexpFilter(re string, match bool) (LineFilter, error) {
	reg, err := syntax.Parse(re, syntax.Perl)
	if err != nil {
		return nil, err
	}
	reg = reg.Simplify()

	// attempt to improve regex with tricks
	f, ok := simplify(reg)
	if !ok {
		return newRegexpFilter(re, match)
	}
	if match {
		return f, nil
	}
	return newNotFilter(f), nil
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
		f = newOrFilter(f, f2)
	}
	return f, true
}

func simplifyConcat(reg *syntax.Regexp) (LineFilter, bool) {
	clearCapture(reg.Sub...)
	if len(reg.Sub) > 3 {
		return nil, false
	}
	// Concat operations are either literal and star such as foo.* .*foo.* .*foo
	// which is a literalFilter.
	// Or a literal and alternates operation, which represent a multiplication of alternates.
	// For instance foo|bar|b|buzz|zz is expressed as foo|b(ar|(?:)|uzz)|zz, (?:) being an OpEmptyMatch.
	// Anything else is rejected.
	var rootLiteral []byte
	var filters []LineFilter
	for _, sub := range reg.Sub {
		if sub.Op == syntax.OpLiteral {
			// only one literal
			if rootLiteral != nil {
				return nil, false
			}
			rootLiteral = []byte(string(sub.Rune))
			continue
		}
		if sub.Op == syntax.OpAlternate && rootLiteral != nil {
			for _, alt := range sub.Sub {
				switch alt.Op {
				case syntax.OpEmptyMatch:
					filters = append(filters, literalFilter(rootLiteral))
				case syntax.OpLiteral:
					// concat the root literal with the alternate one.
					altBytes := []byte(string(alt.Rune))
					altLiteral := make([]byte, 0, len(rootLiteral)+len(altBytes))
					altLiteral = append(altLiteral, rootLiteral...)
					altLiteral = append(altLiteral, altBytes...)
					filters = append(filters, literalFilter(altLiteral))
				case syntax.OpConcat:
					f, ok := simplifyConcat(alt)
					if !ok {
						return nil, false
					}
					filters = append(filters, f)
				case syntax.OpStar:
					if alt.Sub[0].Op != syntax.OpAnyCharNotNL {
						return nil, false
					}
					filters = append(filters, literalFilter(rootLiteral))
				default:
					return nil, false
				}
			}
			continue
		}
		if sub.Op != syntax.OpStar && sub.Sub[0].Op != syntax.OpAnyCharNotNL {
			return nil, false
		}
	}

	// we can simplify only if we found a literal.
	if rootLiteral == nil {
		return nil, false
	}

	if len(filters) != 0 {
		// unknown case
		if len(filters) == 1 {
			return nil, false
		}
		// build or filter chain
		f := newOrFilter(filters[0], filters[1])
		for i := 2; i < len(filters); i++ {
			f = newOrFilter(f, filters[i])
		}
		return f, true
	}

	return literalFilter(rootLiteral), true
}
