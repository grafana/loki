package syntax

import (
	regexpsyntax "github.com/grafana/regexp/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

// isCaseInsensitive reports whether the regexp has the FoldCase flag set.
// Copied from pkg/util to avoid pkg/util's transitive server-side dependencies.
func isCaseInsensitive(reg *regexpsyntax.Regexp) bool {
	return (reg.Flags & regexpsyntax.FoldCase) != 0
}

// splitFiltersAndMatchers splits empty matchers off, which are treated as filters.
// Copied from pkg/util to avoid pkg/util's transitive server-side dependencies.
func splitFiltersAndMatchers(allMatchers []*labels.Matcher) (filters, matchers []*labels.Matcher) {
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
