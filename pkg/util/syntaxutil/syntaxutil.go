// Package syntaxutil holds small, dependency-light regexp and matcher helpers
// used by the LogQL parser (pkg/logql/syntax) and pipeline (pkg/logql/log).
//
// These helpers previously lived in pkg/util. They were extracted into this
// leaf package so that the logql packages can use them without importing the
// whole of pkg/util, which transitively pulls in heavyweight dependencies
// (dskit/ring, hashicorp/memberlist, etcd, ...) via unrelated files such as
// ring_watcher.go. pkg/util re-exports these symbols for backwards
// compatibility, so existing callers of util.IsCaseInsensitive et al. are
// unaffected.
package syntaxutil

import (
	"github.com/grafana/regexp/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

func IsCaseInsensitive(reg *syntax.Regexp) bool {
	return (reg.Flags & syntax.FoldCase) != 0
}

// AllNonGreedy turns greedy quantifiers such as `.*` and `.+` into non-greedy ones. This is the same effect as writing
// `.*?` and `.+?`. This is only safe because we use `Match`. If we were to find the exact position and length of the match
// we would not be allowed to make this optimization. `Match` can return quicker because it is not looking for the longest match.
// Prepending the expression with `(?U)` or passing `NonGreedy` to the expression compiler is not enough since it will
// just negate `.*` and `.*?`.
func AllNonGreedy(regs ...*syntax.Regexp) {
	ClearCapture(regs...)
	for _, re := range regs {
		switch re.Op {
		case syntax.OpCapture, syntax.OpConcat, syntax.OpAlternate:
			AllNonGreedy(re.Sub...)
		case syntax.OpStar, syntax.OpPlus:
			re.Flags = re.Flags | syntax.NonGreedy
		default:
			continue
		}
	}
}

// ClearCapture removes capture operation as they are not used for filtering.
func ClearCapture(regs ...*syntax.Regexp) {
	for _, r := range regs {
		if r.Op == syntax.OpCapture {
			*r = *r.Sub[0]
		}
	}
}

// SplitFiltersAndMatchers splits empty matchers off, which are treated as filters, see #220
func SplitFiltersAndMatchers(allMatchers []*labels.Matcher) (filters, matchers []*labels.Matcher) {
	for _, matcher := range allMatchers {
		// If a matcher matches "", we need to fetch possible chunks where
		// there is no value and will therefore not be in our label index.
		// e.g. {foo=""} and {foo!="bar"} both match "", so we need to return
		// chunks which do not have a foo label set. When looking entries in
		// the index, we should ignore this matcher to fetch all possible chunks
		// and then filter on the matcher after the chunks have been fetched.
		if matcher.Matches("") {
			// Always skip matches that match everything
			if matcher.Type == labels.MatchRegexp && matcher.Value == ".*" {
				continue
			}
			filters = append(filters, matcher)
		} else {
			matchers = append(matchers, matcher)
		}
	}
	return
}
