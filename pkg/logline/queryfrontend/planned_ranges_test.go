package queryfrontend

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/querylimits"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

func TestBuildPlannedQueryRanges(t *testing.T) {
	queryStart := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	queryEnd := queryStart.Add(4 * time.Hour)
	cutoff := queryStart.Add(3 * time.Hour)

	got := buildPlannedQueryRanges(
		[]hintprovider.HintTimeRange{
			{Start: queryStart.Add(-time.Hour), End: queryStart.Add(30 * time.Minute)},
			{Start: queryStart.Add(2 * time.Hour), End: queryEnd},
		},
		queryStart, queryEnd, cutoff,
	)
	require.Equal(t, []querylimits.TimeRange{
		{Start: queryStart, End: queryStart.Add(30 * time.Minute)},
		{Start: queryStart.Add(2 * time.Hour), End: queryEnd},
	}, got)
}

func TestBuildPlannedQueryRanges_PreMinDateRewritesToQueryStart(t *testing.T) {
	queryStart := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	queryEnd := queryStart.Add(2 * time.Hour)
	minDate := queryStart.Add(time.Hour)

	got := buildPlannedQueryRanges(
		[]hintprovider.HintTimeRange{
			{Start: time.Time{}, End: minDate, Source: hintprovider.HintSourcePreMinDate},
		},
		queryStart, queryEnd, queryEnd,
	)
	require.Equal(t, []querylimits.TimeRange{
		{Start: queryStart, End: minDate},
	}, got)
}

func TestInjectPlannedQueryRanges_SkipsWhenLookupFailed(t *testing.T) {
	ctx := injectPlannedQueryRanges(context.Background(), &hintPrefetchResult{
		err:  context.Canceled,
		done: closedDone(),
	})
	_, ok := querylimits.ExtractPlannedQueryRanges(ctx)
	require.False(t, ok)
}

func TestInjectPlannedQueryRanges_SkipsWhenStillInFlight(t *testing.T) {
	ctx := injectPlannedQueryRanges(context.Background(), &hintPrefetchResult{
		done: make(chan struct{}),
	})
	_, ok := querylimits.ExtractPlannedQueryRanges(ctx)
	require.False(t, ok)
}

func TestInjectPlannedQueryRanges_EmptyPlanIsPresent(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	ctx := injectPlannedQueryRanges(context.Background(), &hintPrefetchResult{
		queryStart:     start,
		queryEnd:       end,
		ingesterCutoff: end,
		done:           closedDone(),
	})
	got, ok := querylimits.ExtractPlannedQueryRanges(ctx)
	require.True(t, ok)
	require.Empty(t, got)
}

func closedDone() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
