package extract

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/cortexpb"
)

var (
	errNoMetricNameLabel = fmt.Errorf("No metric name label")
)

// MetricNameFromLabelAdapters extracts the metric name from a list of LabelPairs.
// The returned metric name string is a copy of the label value.
func MetricNameFromLabelAdapters(labels []cortexpb.LabelAdapter) (string, error) {
	unsafeMetricName, err := UnsafeMetricNameFromLabelAdapters(labels)
	if err != nil {
		return "", err
	}

	// Force a string copy since LabelAdapter is often a pointer into
	// a large gRPC buffer which we don't want to keep alive on the heap.
	return string([]byte(unsafeMetricName)), nil
}

// UnsafeMetricNameFromLabelAdapters extracts the metric name from a list of LabelPairs.
// The returned metric name string is a reference to the label value (no copy).
func UnsafeMetricNameFromLabelAdapters(labels []cortexpb.LabelAdapter) (string, error) {
	for _, label := range labels {
		if label.Name == model.MetricNameLabel {
			return label.Value, nil
		}
	}
	return "", errNoMetricNameLabel
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

// MetricNameFromLabels extracts the metric name from a list of Prometheus Labels.
func MetricNameFromLabels(lbls labels.Labels) (metricName string, err error) {
	metricName = lbls.Get(model.MetricNameLabel)
	if metricName == "" {
		err = errNoMetricNameLabel
	}
	return
}
