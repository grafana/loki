package util

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

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
			filters = append(filters, matcher)
		} else {
			matchers = append(matchers, matcher)
		}
	}
	return
}

// ExtractMetricNameFromMetric extract the metric name from a model.Metric
func ExtractMetricNameFromMetric(m model.Metric) (model.LabelValue, error) {
	for name, value := range m {
		if name == model.MetricNameLabel {
			return value, nil
		}
	}
	return "", fmt.Errorf("no MetricNameLabel for chunk")
}

// ExtractMetricNameMatcherFromMatchers extracts the metric name from a set of matchers
func ExtractMetricNameMatcherFromMatchers(matchers []*labels.Matcher) (*labels.Matcher, []*labels.Matcher, bool) {
	// Handle the case where there is no metric name and all matchers have been
	// filtered out e.g. {foo=""}.
	if len(matchers) == 0 {
		return nil, matchers, false
	}

	outMatchers := make([]*labels.Matcher, len(matchers)-1)
	for i, matcher := range matchers {
		if matcher.Name != model.MetricNameLabel {
			continue
		}

		// Copy other matchers, excluding the found metric name matcher
		copy(outMatchers, matchers[:i])
		copy(outMatchers[i:], matchers[i+1:])
		return matcher, outMatchers, true
	}
	// Return all matchers if none are metric name matchers
	return nil, matchers, false
}
