package retention

import (
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
)

// getSeriesPerRule merges rules per seriesID, if multiple series matches a rules, the strongest weight wins.
// Otherwise the shorter retention wins.
func getSeriesPerRule(series [][]string, rules []StreamRule) map[string]StreamRule {
	res := map[string]StreamRule{}
	for i, seriesPerRules := range series {
		for _, series := range seriesPerRules {
			r, ok := res[series]
			newRule := rules[i]
			if ok {
				// we already have a rules for this series.
				if newRule.Weight > r.Weight {
					res[series] = newRule
				}
				if newRule.Weight == r.Weight && newRule.Duration < r.Duration {
					res[series] = newRule
				}
				continue
			}
			res[series] = newRule
		}
	}
	return res
}

type StreamRule struct {
	Matchers []labels.Matcher
	Duration time.Duration
	// in case a series matches multiple Rules takes the one with higher weight or the first
	Weight int
}

type Rules interface {
	PerTenant(userID string) time.Duration
	PerStream(userID string) []StreamRule
}
