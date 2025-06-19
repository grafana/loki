package v1

import (
	regexsyn "github.com/grafana/regexp/syntax"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
)

// Simplifiable regexp expressions can quickly expand into very high
// cardinality; we limit the number of matchers to prevent this. However,
// since bloom tests are relatively cheap to test, we can afford to be a little
// generous while still preventing excessive cardinality.
//
// For example, the regex `[0-9]` expands to 10 matchers (0, 1, .. 9), while
// `[0-9][0-9][0-9]` expands to 1000 matchers (000, 001, .., 999).
const maxRegexMatchers = 200

// LabelMatcher represents bloom tests for key-value pairs, mapped from
// LabelFilterExprs from the AST.
type LabelMatcher interface{ isLabelMatcher() }

// UnsupportedLabelMatcher represents a label matcher which could not be
// mapped. Bloom tests for UnsupportedLabelMatchers must always pass.
type UnsupportedLabelMatcher struct{}

// KeyValueMatcher represents a direct key-value matcher. Bloom tests must only
// pass if the key-value pair exists in the bloom.
type KeyValueMatcher struct{ Key, Value string }

// KeyMatcher represents a key matcher. Bloom tests must only pass if the key
// exists in the bloom.
type KeyMatcher struct{ Key string }

// OrLabelMatcher represents a logical OR test. Bloom tests must only pass if
// one of the Left or Right label matcher bloom tests pass.
type OrLabelMatcher struct{ Left, Right LabelMatcher }

// AndLabelMatcher represents a logical AND test. Bloom tests must only pass
// if both of the Left and Right label matcher bloom tests pass.
type AndLabelMatcher struct{ Left, Right LabelMatcher }

// ExtractTestableLabelMatchers extracts label matchers from the label filters
// in an expression. The resulting label matchers can then be used for testing
// against bloom filters. Only label matchers before the first parse stage are
// included.
//
// Unsupported LabelFilterExprs map to an UnsupportedLabelMatcher, for which
// bloom tests should always pass.
func ExtractTestableLabelMatchers(expr syntax.Expr) []LabelMatcher {
	if expr == nil {
		return nil
	}
	filters := syntax.ExtractLabelFiltersBeforeParser(expr)
	return buildLabelMatchers(filters)
}

func buildLabelMatchers(exprs []*syntax.LabelFilterExpr) []LabelMatcher {
	matchers := make([]LabelMatcher, 0, len(exprs))
	for _, expr := range exprs {
		matchers = append(matchers, buildLabelMatcher(expr.LabelFilterer))
	}
	return matchers
}

func buildLabelMatcher(filter log.LabelFilterer) LabelMatcher {
	switch filter := filter.(type) {

	case *log.LineFilterLabelFilter:
		if filter.Type == labels.MatchEqual {
			return KeyValueMatcher{
				Key:   filter.Name,
				Value: filter.Value,
			}
		} else if filter.Type == labels.MatchRegexp {
			reg, err := regexsyn.Parse(filter.Value, regexsyn.Perl)
			if err != nil {
				return UnsupportedLabelMatcher{}
			}
			return buildSimplifiedRegexMatcher(filter.Name, reg.Simplify())
		}

		return UnsupportedLabelMatcher{}

	case *log.StringLabelFilter:
		if filter.Type != labels.MatchEqual {
			return UnsupportedLabelMatcher{}
		}

		return KeyValueMatcher{
			Key:   filter.Name,
			Value: filter.Value,
		}

	case *log.BinaryLabelFilter:
		var (
			left  = buildLabelMatcher(filter.Left)
			right = buildLabelMatcher(filter.Right)
		)

		if filter.And {
			return AndLabelMatcher{Left: left, Right: right}
		}
		return OrLabelMatcher{Left: left, Right: right}

	default:
		return UnsupportedLabelMatcher{}
	}
}

// buildSimplifiedRegexMatcher builds a simplified label matcher from a regex.
// reg may be mutated.
func buildSimplifiedRegexMatcher(key string, reg *regexsyn.Regexp) LabelMatcher {
	switch reg.Op {
	case regexsyn.OpAlternate:
		util.ClearCapture(reg)

		left := buildSimplifiedRegexMatcher(key, reg.Sub[0])
		if len(reg.Sub) == 1 {
			// This shouldn't be possible (even `warn|` has two subexpressions, where
			// the latter matches an empty string), but we have a length check here
			// anyway just to avoid a potential panic.
			return left
		}
		for _, sub := range reg.Sub[1:] {
			right := buildSimplifiedRegexMatcher(key, sub)
			left = OrLabelMatcher{Left: left, Right: right}
		}
		return left

	case regexsyn.OpConcat:
		// OpConcat checks for the concatenation of two or more subexpressions. For
		// example, value1|value2 simplifies to value[12], with the two
		// subexpressions value and [12].
		//
		// We expand subexpressions back out into full matchers where possible, so
		// value[12] becomes value1 OR value2, and value[1-9] becomes value1 OR
		// value2 .. OR value9.
		util.ClearCapture(reg)

		matchers, ok := expandSubexpr(reg)
		if !ok || len(matchers) == 0 {
			return UnsupportedLabelMatcher{}
		}

		var left LabelMatcher = KeyValueMatcher{Key: key, Value: matchers[0]}
		for _, matcher := range matchers[1:] {
			right := KeyValueMatcher{Key: key, Value: matcher}
			left = OrLabelMatcher{Left: left, Right: right}
		}
		return left

	case regexsyn.OpCapture:
		util.ClearCapture(reg)
		return buildSimplifiedRegexMatcher(key, reg)

	case regexsyn.OpLiteral:
		return KeyValueMatcher{
			Key:   key,
			Value: string(reg.Rune),
		}

	case regexsyn.OpPlus:
		if reg.Sub[0].Op == regexsyn.OpAnyChar || reg.Sub[0].Op == regexsyn.OpAnyCharNotNL { // .+
			return KeyMatcher{Key: key}
		}

		return UnsupportedLabelMatcher{}

	default:
		return UnsupportedLabelMatcher{}
	}
}

func expandSubexpr(reg *regexsyn.Regexp) (prefixes []string, ok bool) {
	switch reg.Op {
	case regexsyn.OpAlternate:
		util.ClearCapture(reg)

		for _, sub := range reg.Sub {
			subPrefixes, ok := expandSubexpr(sub)
			if !ok {
				return nil, false
			} else if len(prefixes)+len(subPrefixes) > maxRegexMatchers {
				return nil, false
			}
			prefixes = append(prefixes, subPrefixes...)
		}
		return prefixes, true

	case regexsyn.OpCharClass:
		// OpCharClass stores ranges of characters, so [12] is the range of bytes
		// []rune('1', '2'), while [15] is represented as []rune('1', '1', '5',
		// '5').
		//
		// To expand OpCharClass, we iterate over each pair of runes.
		if len(reg.Rune)%2 != 0 {
			// Invalid regexp; sequences should be even.
			return nil, false
		}

		for i := 0; i < len(reg.Rune); i += 2 {
			start, end := reg.Rune[i+0], reg.Rune[i+1]
			for r := start; r <= end; r++ {
				prefixes = append(prefixes, string(r))
				if len(prefixes) > maxRegexMatchers {
					return nil, false
				}
			}
		}

		return prefixes, true

	case regexsyn.OpConcat:
		if len(reg.Sub) == 0 {
			return nil, false
		}

		// We get the prefixes for each subexpression and then iteratively combine
		// them together.
		//
		// For the regexp [12][34]value (which concatenates [12], [34], and value):
		//
		// 1. We get the prefixes for [12], which are 1 and 2.
		// 2. We get the prefixes for [34], which are 3 and 4.
		// 3. We add the prefixes together to get 13, 14, 23, and 24.
		// 4. We get the prerfixes for value, which is value.
		// 5. Finally, we add the prefixes together to get 13value, 14value, 23value, and 24value.
		curPrefixes, ok := expandSubexpr(reg.Sub[0])
		if !ok {
			return nil, false
		}

		for _, sub := range reg.Sub[1:] {
			subPrefixes, ok := expandSubexpr(sub)
			if !ok {
				return nil, false
			} else if len(curPrefixes)*len(subPrefixes) > maxRegexMatchers {
				return nil, false
			}

			newPrefixes := make([]string, 0, len(curPrefixes)*len(subPrefixes))

			for _, curPrefix := range curPrefixes {
				for _, subPrefix := range subPrefixes {
					newPrefixes = append(newPrefixes, curPrefix+subPrefix)
				}
			}

			curPrefixes = newPrefixes
		}

		return curPrefixes, true

	case regexsyn.OpCapture:
		util.ClearCapture(reg)
		return expandSubexpr(reg)

	case regexsyn.OpLiteral:
		prefixes = append(prefixes, string(reg.Rune))
		return prefixes, true

	default:
		return nil, false
	}
}

//
// Implement marker types:
//

func (UnsupportedLabelMatcher) isLabelMatcher() {}
func (KeyValueMatcher) isLabelMatcher()         {}
func (KeyMatcher) isLabelMatcher()              {}
func (OrLabelMatcher) isLabelMatcher()          {}
func (AndLabelMatcher) isLabelMatcher()         {}
