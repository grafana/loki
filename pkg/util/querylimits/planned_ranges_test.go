package querylimits

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlannedQueryRanges_AbsentVsEmpty(t *testing.T) {
	_, ok := ExtractPlannedQueryRanges(context.Background())
	require.False(t, ok)

	ctx := InjectPlannedQueryRanges(context.Background(), nil)
	ranges, ok := ExtractPlannedQueryRanges(ctx)
	require.True(t, ok)
	require.Empty(t, ranges)

	ctx = InjectPlannedQueryRanges(context.Background(), []TimeRange{})
	ranges, ok = ExtractPlannedQueryRanges(ctx)
	require.True(t, ok)
	require.Empty(t, ranges)
}

func TestPlannedQueryRanges_InjectCopies(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	original := []TimeRange{{Start: start, End: start.Add(time.Hour)}}

	ctx := InjectPlannedQueryRanges(context.Background(), original)
	original[0].End = start

	got, ok := ExtractPlannedQueryRanges(ctx)
	require.True(t, ok)
	require.Equal(t, start.Add(time.Hour), got[0].End)
}
