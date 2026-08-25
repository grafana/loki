package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMetrics(t *testing.T) {
	results := &parsedResults{
		TotalDurationSec: 12.5,
		Tests: []parsedTest{
			{
				Name:        "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD",
				Labels:      &testLabels{Suite: "fast", QueryFile: "basic", Kind: "metric", Direction: "FORWARD"},
				Status:      statusPass,
				DurationSec: 2.1,
			},
			{
				Name:        "TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=FORWARD",
				Labels:      &testLabels{Suite: "fast", QueryFile: "basic", Kind: "log", Direction: "FORWARD"},
				Status:      statusFail,
				DurationSec: 3.2,
			},
			{
				Name:        "TestSomethingElse/unknown",
				Labels:      nil,
				Status:      statusPass,
				DurationSec: 0.5,
			},
		},
	}

	ts := buildMetrics(results, "range", "logql-correctness")
	require.NotEmpty(t, ts)

	// Collect metric names present
	names := map[string]bool{}
	for _, s := range ts {
		for _, l := range s.Labels {
			if l.Name == "__name__" {
				names[l.Value] = true
			}
		}
	}
	assert.True(t, names["logql_correctness_tests_total"])
	assert.True(t, names["logql_correctness_test_duration_seconds"])
	assert.True(t, names["logql_correctness_run_duration_seconds"])
	assert.True(t, names["logql_correctness_run_pass_ratio"])

	// Unparseable test counted in tests_total with suite=unknown, not in test_duration_seconds
	var hasUnknownTotal, hasUnknownDuration bool
	for _, s := range ts {
		var nameLabel, suiteLabel string
		for _, l := range s.Labels {
			if l.Name == "__name__" {
				nameLabel = l.Value
			}
			if l.Name == "suite" {
				suiteLabel = l.Value
			}
		}
		if nameLabel == "logql_correctness_tests_total" && suiteLabel == "unknown" {
			hasUnknownTotal = true
		}
		if nameLabel == "logql_correctness_test_duration_seconds" && suiteLabel == "unknown" {
			hasUnknownDuration = true
		}
	}
	assert.True(t, hasUnknownTotal)
	assert.False(t, hasUnknownDuration)
}

func TestBuildMetrics_PassRatioZeroDenominator(t *testing.T) {
	results := &parsedResults{
		TotalDurationSec: 1.0,
		Tests: []parsedTest{
			{
				Name:        "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD",
				Labels:      &testLabels{Suite: "fast", QueryFile: "basic", Kind: "metric", Direction: "FORWARD"},
				Status:      statusSkip,
				DurationSec: 0,
			},
		},
	}

	ts := buildMetrics(results, "range", "logql-correctness")

	for _, s := range ts {
		for _, l := range s.Labels {
			if l.Name == "__name__" && l.Value == "logql_correctness_run_pass_ratio" {
				for _, sl := range s.Labels {
					if sl.Name == "suite" {
						assert.NotEqual(t, "fast", sl.Value, "pass_ratio should be omitted when denominator is zero")
					}
				}
			}
		}
	}
}
