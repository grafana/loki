package constants

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsInternalStream(t *testing.T) {
	for _, tc := range []struct {
		labels   []string
		expected bool
	}{
		{labels: []string{"app"}, expected: false},
		{labels: []string{"app", TimeShardLabel}, expected: false},
		{labels: []string{"app", AggregatedMetricLabel}, expected: true},
		{labels: []string{"app", PatternLabel}, expected: true},
	} {
		has := func(name string) bool { return slices.Contains(tc.labels, name) }
		require.Equal(t, tc.expected, IsInternalStream(has), "labels %v", tc.labels)
		require.Equal(t, tc.expected, IsPatternIngesterStream(has), "labels %v", tc.labels)
	}
}

func TestIsInternalLabel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected bool
	}{
		{name: "app", expected: false},
		{name: BackfillLabel, expected: false},
		{name: AggregatedMetricLabel, expected: true},
		{name: PatternLabel, expected: true},
		{name: TimeShardLabel, expected: true},
		{name: StreamShardLabel, expected: true},
	} {
		require.Equal(t, tc.expected, IsInternalLabel(tc.name), "label %s", tc.name)
	}
}
