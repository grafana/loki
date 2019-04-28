package extract

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// MetricNameFromLabelAdapters extracts the metric name from a list of LabelPairs.
func MetricNameFromLabelAdapters(labels []client.LabelAdapter) (string, error) {
	for _, label := range labels {
		if label.Name == model.MetricNameLabel {
			return label.Value, nil
		}
	}
	return "", fmt.Errorf("No metric name label")
}

// MetricNameFromMetric extract the metric name from a model.Metric
func MetricNameFromMetric(m model.Metric) (model.LabelValue, error) {
	if value, found := m[model.MetricNameLabel]; found {
		return value, nil
	}
	return "", fmt.Errorf("no MetricNameLabel for chunk")
}

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
