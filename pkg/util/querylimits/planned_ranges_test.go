package querylimits

import (
	"context"
	"net/http"
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

func TestPlannedQueryRanges_NotSetFromHTTPHeaders(t *testing.T) {
	r, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	require.NoError(t, InjectQueryLimitsContextHTTP(r, &Context{
		Expr: `{app="foo"}`,
		From: from,
		To:   to,
	}))
	require.NoError(t, InjectQueryLimitsHTTP(r, &QueryLimits{}))

	ctx := context.Background()

	limitsCtx, err := ExtractQueryLimitsContextHTTP(r)
	require.NoError(t, err)
	require.NotNil(t, limitsCtx)
	ctx = InjectQueryLimitsContextIntoContext(ctx, *limitsCtx)

	limits, err := ExtractQueryLimitsHTTP(r)
	require.NoError(t, err)
	require.NotNil(t, limits)
	ctx = InjectQueryLimitsIntoContext(ctx, *limits)

	_, ok := ExtractPlannedQueryRanges(ctx)
	require.False(t, ok)
}
