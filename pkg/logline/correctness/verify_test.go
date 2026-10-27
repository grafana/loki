package correctness

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/verification"
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

func TestVerifyMetricHints_EmptyResults(t *testing.T) {
	report := verifyMetricHints(metricTestExpr(t), nil, nil)
	require.Equal(t, 0, report.TotalResults)
	require.Equal(t, 0, report.FalseNegatives)
	require.False(t, report.Correct)
}

func TestVerifyMetricSamples_OverlapCoversSample(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	// The matching hint is not the first range. Any overlap covers the sample.
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-30 * time.Minute), End: step.Add(-20 * time.Minute)},
		{Start: step.Add(-2 * time.Minute), End: step.Add(-1 * time.Minute)},
	}

	report := verification.VerifySamples(metricTestExpr(t), hints, []verification.MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 1, report.CoveredEntries)
	require.Equal(t, 0, report.FalseNegatives)
	require.Empty(t, report.FalseNegativeRanges)
	require.True(t, report.Correct)
}

func TestVerifyMetricSamples_MissRecordsWholeLookback(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-30 * time.Minute), End: step.Add(-20 * time.Minute)},
	}

	report := verification.VerifySamples(metricTestExpr(t), hints, []verification.MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 0, report.CoveredEntries)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, []hintprovider.HintTimeRange{{
		Start: step.Add(-5 * time.Minute),
		End:   step,
	}}, report.FalseNegativeRanges)
	require.False(t, report.Correct)
}

func TestVerifyMetricSamples_HintEndingAtLookbackStartIsAMiss(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	lookbackStart := step.Add(-5 * time.Minute)
	hints := []hintprovider.HintTimeRange{
		{Start: lookbackStart.Add(-time.Minute), End: lookbackStart},
	}

	report := verification.VerifySamples(metricTestExpr(t), hints, []verification.MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, []hintprovider.HintTimeRange{{
		Start: lookbackStart,
		End:   step,
	}}, report.FalseNegativeRanges)
}

func TestVerifyMetricSamples_HintStartingAtLookbackEndCoversSample(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step, End: step.Add(time.Minute)},
	}

	report := verification.VerifySamples(metricTestExpr(t), hints, []verification.MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.CoveredEntries)
	require.Equal(t, 0, report.FalseNegatives)
	require.True(t, report.Correct)
}

func TestVerifyMetricSamples_NonPositiveSamplesAreLeftOut(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-5 * time.Minute), End: step},
	}

	report := verification.VerifySamples(metricTestExpr(t), hints, []verification.MetricSample{
		{Time: step, Value: 0},
		{Time: step.Add(time.Minute), Value: -1},
		{Time: step.Add(2 * time.Minute), Value: 2},
	}, time.Time{})
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 1, report.CoveredEntries)
	require.True(t, report.Correct)
}

func TestVerifyMetricSamples_EmptyReportIsCorrect(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-5 * time.Minute), End: step},
	}

	for _, samples := range [][]verification.MetricSample{nil, {}} {
		report := verification.VerifySamples(metricTestExpr(t), hints, samples, time.Time{})
		require.Equal(t, 0, report.TotalEntries)
		require.Equal(t, 0, report.FalseNegatives)
		require.Empty(t, report.FalseNegativeRanges)
		require.True(t, report.Correct)
	}
}

func TestVerifyMetricSamples_AbsentOverTimeScoresNothing(t *testing.T) {
	expr, err := syntax.ParseExpr(`sum(absent_over_time({job="api"} |= "timeout" [5m]))`)
	require.NoError(t, err)
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)

	report := verification.VerifySamples(expr, nil, []verification.MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 0, report.TotalEntries)
	require.Empty(t, report.FalseNegativeRanges)
	require.True(t, report.Correct)
}

func metricTestExpr(t *testing.T) syntax.Expr {
	t.Helper()
	hq, err := newHintQuery(hintQueryTypeLineFilter, "timeout", `{job="api"} |= "timeout"`)
	require.NoError(t, err)
	metric, err := buildMetricQuery(hintQueryTypeMetricRange, hq)
	require.NoError(t, err)
	return metric.expr
}
