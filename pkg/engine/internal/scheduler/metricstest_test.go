package scheduler

// This file holds shared metric-assertion test helpers used across the
// scheduler package's tests.

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func histogramSampleCount(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()

	metric := metricWithLabels(t, reg, name, labels)
	histogram := metric.GetHistogram()
	require.NotNil(t, histogram, "metric %s with labels %v should be a histogram", name, labels)
	return histogram.GetSampleCount()
}

func metricWithLabels(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) *dto.Metric {
	t.Helper()

	family := metricFamily(t, reg, name)
	for _, metric := range family.GetMetric() {
		if metricLabels(metric).equals(labels) {
			return metric
		}
	}

	require.Failf(t, "metric not found", "metric %s with labels %v was not collected", name, labels)
	return nil
}

func metricFamily(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()

	for _, family := range gatherFamilies(t, reg) {
		if metricNameMatches(family.GetName(), name) {
			return family
		}
	}

	require.Failf(t, "metric family not found", "metric family %s was not collected", name)
	return nil
}

func metricNameMatches(got, want string) bool {
	return got == want || got == strings.TrimSuffix(want, "_total") || got+"_total" == want
}

func gatherFamilies(t *testing.T, reg *prometheus.Registry) []*dto.MetricFamily {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)
	return families
}

type metricLabelSet map[string]string

func metricLabels(metric *dto.Metric) metricLabelSet {
	labels := make(metricLabelSet, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}

// equals reports whether the label set is exactly want and carries no other
// labels, so an accidentally added (for example high-cardinality) label fails
// the lookup instead of being silently ignored.
func (s metricLabelSet) equals(want map[string]string) bool {
	if len(s) != len(want) {
		return false
	}
	for name, value := range want {
		if s[name] != value {
			return false
		}
	}
	return true
}

func requireMetricLabelsBounded(t *testing.T, reg *prometheus.Registry, name string, allowed map[string]map[string]struct{}) {
	t.Helper()

	family := metricFamily(t, reg, name)
	require.NotEmpty(t, family.GetMetric(), "metric %s should have at least one sample", name)
	for _, metric := range family.GetMetric() {
		labels := metricLabels(metric)
		// Reject any label name outside the allowed set so an accidentally added
		// (for example high-cardinality) label fails the test.
		require.Lenf(t, labels, len(allowed), "metric %s has unexpected labels: %v", name, labels)
		for label, values := range allowed {
			value, ok := labels[label]
			require.Truef(t, ok, "metric %s should have label %q", name, label)
			require.Containsf(t, values, value, "metric %s label %q had unbounded value %q", name, label, value)
		}
	}
}

func stringSet(values ...any) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		switch value := value.(type) {
		case string:
			set[value] = struct{}{}
		case interface{ String() string }:
			set[value.String()] = struct{}{}
		default:
			panic("unsupported stringSet value")
		}
	}
	return set
}
