package postings

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
)

type PredicateValue struct {
	Name  string
	Value string
}

// CompiledMatcher is a matcher with its regex compiled once
type CompiledMatcher struct {
	matcher *labels.Matcher
	regex   *labels.FastRegexMatcher // non-nil only for regex matcher types
}

// CompileMatcher compiles a matcher's regex once so it can be reused.
func CompileMatcher(m *labels.Matcher) (CompiledMatcher, error) {
	cm := CompiledMatcher{matcher: m}
	if m.Type == labels.MatchRegexp || m.Type == labels.MatchNotRegexp {
		re, err := labels.NewFastRegexMatcher(m.Value)
		if err != nil {
			return CompiledMatcher{}, fmt.Errorf("compiling regex %q: %w", m.Value, err)
		}
		cm.regex = re
	}
	return cm, nil
}

// Matcher returns the underlying label matcher.
func (cm CompiledMatcher) Matcher() *labels.Matcher { return cm.matcher }

func (cm CompiledMatcher) valuePredicate(valueCol *Column) Predicate {
	m := cm.matcher
	switch m.Type {
	case labels.MatchEqual:
		return EqualPredicate{Column: valueCol, Value: scalar.NewStringScalar(m.Value)}
	case labels.MatchNotEqual:
		return NotPredicate{Inner: EqualPredicate{Column: valueCol, Value: scalar.NewStringScalar(m.Value)}}
	case labels.MatchRegexp:
		return RegexMatchPredicate{Column: valueCol, Matcher: cm.regex}
	case labels.MatchNotRegexp:
		return NotPredicate{Inner: RegexMatchPredicate{Column: valueCol, Matcher: cm.regex}}
	default:
		return FalsePredicate{}
	}
}
