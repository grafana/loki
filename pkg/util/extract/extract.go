package extract

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var errNoMetricNameLabel = fmt.Errorf("No metric name label")

// MetricNameMatcherFromMatchers extracts the metric name from a set of matchers
func MetricNameMatcherFromMatchers(matchers []*labels.Matcher) (*labels.Matcher, []*labels.Matcher, bool) {
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

// UnsafeMetricNameFromLabelAdapters extracts the metric name from a list of LabelPairs.
// The returned metric name string is a reference to the label value (no copy).
func UnsafeMetricNameFromLabelAdapters(labels []logproto.LabelAdapter) (string, error) {
	for _, label := range labels {
		if label.Name == model.MetricNameLabel {
			return label.Value, nil
		}
	}
	return "", errNoMetricNameLabel
}
