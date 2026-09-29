package verification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
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
