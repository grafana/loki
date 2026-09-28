package correctness

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

func TestVerifyHints_AllResultsCovered(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}
	entries := []logproto.Entry{
		{Timestamp: time.Date(2026, 2, 26, 10, 0, 51, 0, time.UTC)},
		{Timestamp: time.Date(2026, 2, 26, 10, 1, 9, 0, time.UTC)},
	}

	report := verifyHints(ranges, entries)
	require.Equal(t, 1, report.HintRanges)
	require.Equal(t, 2, report.TotalResults)
	require.Equal(t, 2, report.CoveredResults)
	require.Equal(t, 0, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.True(t, report.Correct)
}

func TestVerifyHints_PartialCoverage(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}
	entries := []logproto.Entry{
		{Timestamp: time.Date(2026, 2, 26, 10, 0, 51, 0, time.UTC)},
		{Timestamp: time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)}, // outside range
	}

	report := verifyHints(ranges, entries)
	require.Equal(t, 2, report.TotalResults)
	require.Equal(t, 1, report.CoveredResults)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_FalsePositiveRanges(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}
	entries := []logproto.Entry{
		{Timestamp: time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)}, // outside all ranges
	}

	report := verifyHints(ranges, entries)
	require.Equal(t, 1, report.TotalResults)
	require.Equal(t, 0, report.CoveredResults)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, 1, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_EmptyResults(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}

	report := verifyHints(ranges, nil)
	require.Equal(t, 1, report.HintRanges)
	require.Equal(t, 0, report.TotalResults)
	require.Equal(t, 0, report.FalseNegatives)
	require.Equal(t, 1, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_EmptyHints(t *testing.T) {
	entries := []logproto.Entry{
		{Timestamp: time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)},
	}

	report := verifyHints(nil, entries)
	require.Equal(t, 0, report.HintRanges)
	require.Equal(t, 1, report.TotalResults)
	require.Equal(t, 0, report.CoveredResults)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.False(t, report.Correct)
}
