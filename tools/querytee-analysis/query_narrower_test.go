package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLogCountQuery_LogQuery(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "simple stream selector",
			query:    `{app="foo"}`,
			expected: `sum(count_over_time({app="foo"} [3600s]))`,
		},
		{
			name:     "pipeline expr",
			query:    `{app="foo"} | logfmt | level="error"`,
			expected: `sum(count_over_time({app="foo"} | logfmt | level="error" [3600s]))`,
		},
		{
			name:     "pipeline with line filter",
			query:    `{app="foo"} |= "err" | logfmt`,
			expected: `sum(count_over_time({app="foo"} |= "err" | logfmt [3600s]))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, evalTime, err := BuildLogCountQuery(tc.query, start, end)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
			assert.Equal(t, end, evalTime)
		})
	}
}

func TestBuildLogCountQuery_MetricQuery(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "count_over_time no grouping",
			query:    `count_over_time({app="foo"}[5m])`,
			expected: `sum(count_over_time({app="foo"} [3900s]))`,
		},
		{
			name:     "sum no grouping",
			query:    `sum(rate({app="foo"} | logfmt | level="error" [5m]))`,
			expected: `sum(count_over_time({app="foo"} | logfmt | level="error" [3900s]))`,
		},
		{
			name:     "sum by preserves labels",
			query:    `sum by (level) (count_over_time({app="foo"} | logfmt | level!="" [5m]))`,
			expected: `sum by (level)(count_over_time({app="foo"} | logfmt | level!="" [3900s]))`,
		},
		{
			name:     "sum by multiple labels",
			query:    `sum by (level, app) (count_over_time({app="foo"} | logfmt [5m]))`,
			expected: `sum by (level, app)(count_over_time({app="foo"} | logfmt [3900s]))`,
		},
		{
			name:     "avg by converted to sum by",
			query:    `avg by (level) (rate({app="foo"} | logfmt [5m]))`,
			expected: `sum by (level)(count_over_time({app="foo"} | logfmt [3900s]))`,
		},
		{
			name:     "rate with 10m range",
			query:    `sum(rate({app="foo"} | logfmt | level="error" [10m]))`,
			expected: `sum(count_over_time({app="foo"} | logfmt | level="error" [4200s]))`,
		},
		{
			name:     "sum without grouping",
			query:    `sum without (pod) (count_over_time({app="foo"} | logfmt [5m]))`,
			expected: `sum without (pod)(count_over_time({app="foo"} | logfmt [3900s]))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, evalTime, err := BuildLogCountQuery(tc.query, start, end)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
			assert.Equal(t, end, evalTime)
		})
	}
}

func TestBuildLogCountQuery_PreservesEvalTime(t *testing.T) {
	start := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 15, 11, 30, 0, 0, time.UTC)

	_, evalTime, err := BuildLogCountQuery(`{app="foo"}`, start, end)
	require.NoError(t, err)
	assert.Equal(t, end, evalTime)
}

func TestBuildLogCountQuery_ZeroDuration(t *testing.T) {
	now := time.Now()
	result, _, err := BuildLogCountQuery(`{app="foo"}`, now, now)
	require.NoError(t, err)
	assert.Contains(t, result, "[1s]", "zero duration should use minimum 1s")
	assert.True(t, result[:3] == "sum", "should be wrapped in sum")
}

func TestBuildLogCountQuery_InvalidQuery(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Hour)
	_, _, err := BuildLogCountQuery("not a valid query {{{", start, end)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing query")
}
