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

	for _, tc := range []struct {
		desc   string
		hints  []hintprovider.HintTimeRange
		start  time.Time
		end    time.Time
		cutoff time.Time
		want   []querylimits.TimeRange
	}{
		{
			desc:   "no hints and no ingester window",
			start:  queryStart,
			end:    cutoff,
			cutoff: cutoff,
			want:   nil,
		},
		{
			desc:   "no hints attaches the ingester window",
			start:  queryStart,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: cutoff, End: queryEnd},
			},
		},
		{
			desc: "store hints are clipped to the query and cutoff",
			hints: []hintprovider.HintTimeRange{
				{Start: queryStart.Add(-time.Hour), End: queryStart.Add(30 * time.Minute)},
				{Start: queryStart.Add(2 * time.Hour), End: queryEnd},
			},
			start:  queryStart,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: queryStart, End: queryStart.Add(30 * time.Minute)},
				{Start: queryStart.Add(2 * time.Hour), End: queryEnd},
			},
		},
		{
			desc: "zero-start passthrough is rewritten to query start",
			hints: []hintprovider.HintTimeRange{
				{Start: time.Time{}, End: queryStart.Add(time.Hour)},
			},
			start:  queryStart,
			end:    cutoff,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: queryStart, End: queryStart.Add(time.Hour)},
			},
		},
		{
			desc: "hint after store end is dropped",
			hints: []hintprovider.HintTimeRange{
				{Start: cutoff.Add(time.Minute), End: queryEnd},
			},
			start:  queryStart,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: cutoff, End: queryEnd},
			},
		},
		{
			desc: "store window that touches cutoff is extended through the ingester",
			hints: []hintprovider.HintTimeRange{
				{Start: queryStart.Add(2 * time.Hour), End: cutoff},
			},
			start:  queryStart,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: queryStart.Add(2 * time.Hour), End: queryEnd},
			},
		},
		{
			desc:   "entire query in the ingester window is one planned range",
			start:  cutoff,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: cutoff, End: queryEnd},
			},
		},
		{
			desc: "pre-min-date sentinel and a store hit plus the ingester window",
			hints: []hintprovider.HintTimeRange{
				{Start: time.Time{}, End: queryStart.Add(time.Hour), Source: hintprovider.HintSourcePreMinDate},
				{Start: queryStart.Add(90 * time.Minute), End: queryStart.Add(2 * time.Hour)},
			},
			start:  queryStart,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: queryStart, End: queryStart.Add(time.Hour)},
				{Start: queryStart.Add(90 * time.Minute), End: queryStart.Add(2 * time.Hour)},
				{Start: cutoff, End: queryEnd},
			},
		},
		{
			desc: "index-empty holes are omitted",
			hints: []hintprovider.HintTimeRange{
				{Start: queryStart.Add(2 * time.Hour), End: queryStart.Add(2*time.Hour + time.Minute)},
			},
			start:  queryStart,
			end:    cutoff,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: queryStart.Add(2 * time.Hour), End: queryStart.Add(2*time.Hour + time.Minute)},
			},
		},
		{
			desc: "a hint that ends before cutoff leaves a separate ingester window",
			hints: []hintprovider.HintTimeRange{
				{Start: queryStart, End: queryStart.Add(time.Hour)},
			},
			start:  queryStart,
			end:    queryEnd,
			cutoff: cutoff,
			want: []querylimits.TimeRange{
				{Start: queryStart, End: queryStart.Add(time.Hour)},
				{Start: cutoff, End: queryEnd},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := buildPlannedQueryRanges(tc.hints, tc.start, tc.end, tc.cutoff)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPlannedRangeSource_Wait_EmptyIsPresent(t *testing.T) {
	queryStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	queryEnd := queryStart.Add(24 * time.Hour)
	result := &hintPrefetchResult{
		queryStart:     queryStart,
		queryEnd:       queryEnd,
		ingesterCutoff: queryEnd,
		done:           make(chan struct{}),
	}
	result.setPlannedRanges()
	close(result.done)

	src := &plannedRangeSource{result: result, timeout: time.Second}
	got, ok := src.Wait(t.Context())
	require.True(t, ok, "empty plan must be distinct from missing")
	require.Empty(t, got)
}

func TestPlannedRangeSource_Wait_ErrorIsAbsent(t *testing.T) {
	result := &hintPrefetchResult{
		err:  context.Canceled,
		done: make(chan struct{}),
	}
	close(result.done)

	src := &plannedRangeSource{result: result, timeout: time.Second}
	_, ok := src.Wait(t.Context())
	require.False(t, ok)
}
