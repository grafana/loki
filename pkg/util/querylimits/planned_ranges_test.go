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

func TestTimeRange_Intersect(t *testing.T) {
	base := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	r := func(start, end time.Duration) TimeRange {
		return TimeRange{Start: base.Add(start), End: base.Add(end)}
	}

	for _, tc := range []struct {
		name        string
		a, b        TimeRange
		want        TimeRange
		wantOverlap bool
	}{
		{
			name:        "overlap",
			a:           r(0, 2*time.Hour),
			b:           r(time.Hour, 3*time.Hour),
			want:        r(time.Hour, 2*time.Hour),
			wantOverlap: true,
		},
		{
			name:        "contained",
			a:           r(0, 3*time.Hour),
			b:           r(time.Hour, 2*time.Hour),
			want:        r(time.Hour, 2*time.Hour),
			wantOverlap: true,
		},
		{
			name:        "identical",
			a:           r(0, time.Hour),
			b:           r(0, time.Hour),
			want:        r(0, time.Hour),
			wantOverlap: true,
		},
		{
			name:        "adjacent half-open do not overlap",
			a:           r(0, time.Hour),
			b:           r(time.Hour, 2*time.Hour),
			wantOverlap: false,
		},
		{
			name:        "disjoint",
			a:           r(0, time.Hour),
			b:           r(2*time.Hour, 3*time.Hour),
			wantOverlap: false,
		},
		{
			name:        "empty range",
			a:           r(0, 0),
			b:           r(0, time.Hour),
			wantOverlap: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.a.Intersect(tc.b)
			require.Equal(t, tc.wantOverlap, ok)
			if tc.wantOverlap {
				require.Equal(t, tc.want, got)
			}
		})
	}
}
