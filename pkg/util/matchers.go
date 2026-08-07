package util //nolint:revive

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/util/syntaxutil"
)

// SplitFiltersAndMatchers splits empty matchers off, which are treated as filters, see #220
//
// This was moved to pkg/util/syntaxutil and is re-exported here so existing
// callers of util.SplitFiltersAndMatchers keep working unchanged.
func SplitFiltersAndMatchers(allMatchers []*labels.Matcher) (filters, matchers []*labels.Matcher) {
	return syntaxutil.SplitFiltersAndMatchers(allMatchers)
}
