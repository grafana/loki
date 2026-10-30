package verification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var base = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func TestRangeCoversTimestamp(t *testing.T) {
	r := hintprovider.HintTimeRange{Start: base, End: base.Add(10 * time.Minute)}

	require.True(t, RangeCoversTimestamp(r, base), "start is inclusive")
	require.True(t, RangeCoversTimestamp(r, base.Add(5*time.Minute)), "middle is covered")
	require.True(t, RangeCoversTimestamp(r, base.Add(10*time.Minute)), "end boundary is covered")
	require.False(t, RangeCoversTimestamp(r, base.Add(-1*time.Minute)), "before start")
	require.False(t, RangeCoversTimestamp(r, base.Add(11*time.Minute)), "after end")
}

func TestTimestampCovered(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
		{Start: base.Add(20 * time.Minute), End: base.Add(30 * time.Minute)},
	}

	require.True(t, TimestampCovered(ranges, base.Add(5*time.Minute)))
	require.True(t, TimestampCovered(ranges, base.Add(25*time.Minute)))
	require.False(t, TimestampCovered(ranges, base.Add(15*time.Minute)), "gap between ranges")
	require.False(t, TimestampCovered(nil, base), "nil ranges")
}

func TestVerifyEntries_AllCovered(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
	}
	timestamps := []time.Time{base.Add(2 * time.Minute), base.Add(5 * time.Minute)}
	r := VerifyEntries(ranges, timestamps)

	require.Equal(t, 2, r.TotalEntries)
	require.Equal(t, 2, r.CoveredEntries)
	require.Equal(t, 0, r.FalseNegatives)
	require.True(t, r.Correct())
	require.Empty(t, r.FalseNegativeTimestamps)
}

func TestVerifyEntries_WithFalseNegatives(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
	}
	uncovered := base.Add(20 * time.Minute)
	timestamps := []time.Time{base.Add(5 * time.Minute), uncovered}
	r := VerifyEntries(ranges, timestamps)

	require.Equal(t, 2, r.TotalEntries)
	require.Equal(t, 1, r.CoveredEntries)
	require.Equal(t, 1, r.FalseNegatives)
	require.False(t, r.Correct())
	require.Equal(t, []time.Time{uncovered}, r.FalseNegativeTimestamps)
}

func TestVerifyEntries_EmptyEntries(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
	}
	r := VerifyEntries(ranges, nil)

	require.Equal(t, 0, r.TotalEntries)
	require.Equal(t, 0, r.FalseNegatives)
	require.True(t, r.Correct())
}

func TestCountFalsePositives(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
		{Start: base.Add(20 * time.Minute), End: base.Add(30 * time.Minute)},
	}
	timestamps := []time.Time{base.Add(5 * time.Minute)}
	fp := CountFalsePositives(ranges, timestamps)
	require.Equal(t, 1, fp, "second range has no entries")
}

func TestVerifySamples_OverlapCoversSample(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	// The matching hint is not the first range. Any overlap covers the sample.
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-30 * time.Minute), End: step.Add(-20 * time.Minute)},
		{Start: step.Add(-2 * time.Minute), End: step.Add(-1 * time.Minute)},
	}

	report := VerifySamples(metricExpr(t), hints, []MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 1, report.CoveredEntries)
	require.Equal(t, 0, report.FalseNegatives)
	require.Empty(t, report.FalseNegativeRanges)
	require.True(t, report.Correct())
}

func TestVerifySamples_MissRecordsWholeLookback(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-30 * time.Minute), End: step.Add(-20 * time.Minute)},
	}

	report := VerifySamples(metricExpr(t), hints, []MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 0, report.CoveredEntries)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, []hintprovider.HintTimeRange{{
		Start: step.Add(-5 * time.Minute),
		End:   step,
	}}, report.FalseNegativeRanges)
	require.False(t, report.Correct())
}

func TestVerifySamples_HintEndingAtLookbackStartIsAMiss(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	lookbackStart := step.Add(-5 * time.Minute)
	hints := []hintprovider.HintTimeRange{
		{Start: lookbackStart.Add(-time.Minute), End: lookbackStart},
	}

	report := VerifySamples(metricExpr(t), hints, []MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, []hintprovider.HintTimeRange{{
		Start: lookbackStart,
		End:   step,
	}}, report.FalseNegativeRanges)
}

func TestVerifySamples_HintStartingAtLookbackEndCoversSample(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step, End: step.Add(time.Minute)},
	}

	report := VerifySamples(metricExpr(t), hints, []MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 1, report.CoveredEntries)
	require.Equal(t, 0, report.FalseNegatives)
	require.True(t, report.Correct())
}

func TestVerifySamples_NonPositiveSamplesAreLeftOut(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-5 * time.Minute), End: step.Add(3 * time.Minute)},
	}

	report := VerifySamples(metricExpr(t), hints, []MetricSample{
		{Time: step, Value: 0},
		{Time: step.Add(time.Minute), Value: -1},
		{Time: step.Add(2 * time.Minute), Value: 2},
	}, time.Time{})
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 1, report.CoveredEntries)
	require.True(t, report.Correct())
}

func TestVerifySamples_AbsentOverTimeScoresNothing(t *testing.T) {
	expr, err := syntax.ParseExpr(`sum(absent_over_time({job="api"} |= "timeout" [5m]))`)
	require.NoError(t, err)
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)

	report := VerifySamples(expr, nil, []MetricSample{{Time: step, Value: 1}}, time.Time{})
	require.Equal(t, 0, report.TotalEntries)
	require.Empty(t, report.FalseNegativeRanges)
	require.True(t, report.Correct())
}

func TestVerifySamples_LookbackEndingAfterEligibleEndIsSkipped(t *testing.T) {
	step := time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{
		{Start: step.Add(-5 * time.Minute), End: step.Add(time.Minute)},
	}

	report := VerifySamples(metricExpr(t), hints, []MetricSample{
		{Time: step, Value: 1},
		{Time: step.Add(time.Minute), Value: 1},
	}, step)
	require.Equal(t, 1, report.TotalEntries)
	require.Equal(t, 1, report.CoveredEntries)
	require.Empty(t, report.FalseNegativeRanges)
	require.True(t, report.Correct())
}

func metricExpr(t *testing.T) syntax.Expr {
	t.Helper()
	expr, err := syntax.ParseExpr(`sum(count_over_time({job="api"} |= "timeout" [5m]))`)
	require.NoError(t, err)
	return expr
}
