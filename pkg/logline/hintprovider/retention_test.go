package hintprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

type fixedRetention time.Duration

func (r fixedRetention) RetentionPeriod() time.Duration { return time.Duration(r) }

// recordingProvider returns canned hints and records the window it was asked for.
type recordingProvider struct {
	hints         *Hints
	err           error
	calls         int
	from, through model.Time
	minDate       time.Time
}

func (p *recordingProvider) ProvideHints(_ context.Context, _ string, _ syntax.Expr, from, through model.Time) (*Hints, *QueryStats, error) {
	p.calls++
	p.from, p.through = from, through
	return p.hints, NewQueryStats(), p.err
}

func (p *recordingProvider) MinDate() time.Time { return p.minDate }

func TestRetentionHintProvider_Cutoff(t *testing.T) {
	afternoon := time.Date(2026, 11, 17, 13, 30, 0, 0, time.UTC)
	midnight := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	days90 := fixedRetention(90 * 24 * time.Hour)

	for _, tc := range []struct {
		name      string
		now       time.Time
		retention fixedRetention
		want      time.Time
	}{
		{"zero retention keeps every index", afternoon, 0, time.Time{}},
		// The cutoff is 2026-08-19T13:30, so indexes of that day may be gone.
		{"cutoff rounds up to the next UTC day", afternoon, days90, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)},
		{"cutoff on a day boundary stays", midnight, days90, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewRetentionHintProvider(&recordingProvider{}, tc.retention)
			p.now = func() time.Time { return tc.now }
			require.Equal(t, tc.want, p.cutoff())
		})
	}

	t.Run("nil retention keeps every index", func(t *testing.T) {
		require.True(t, NewRetentionHintProvider(&recordingProvider{}, nil).cutoff().IsZero())
	})
}

func TestRetentionHintProvider_ProvideHints(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC) // 90 days back is Aug 19 12:00, rounded up
	live := HintTimeRange{Start: time.Date(2026, 10, 1, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 10, 1, 10, 1, 10, 0, time.UTC)}
	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	ms := func(ts time.Time) model.Time { return model.TimeFromUnixNano(ts.UnixNano()) }

	newProvider := func(delegate *recordingProvider) *RetentionHintProvider {
		p := NewRetentionHintProvider(delegate, fixedRetention(90*24*time.Hour))
		p.now = func() time.Time { return now }
		return p
	}

	t.Run("query inside retention is untouched", func(t *testing.T) {
		d := &recordingProvider{hints: &Hints{TimeRanges: []HintTimeRange{live}}}
		from, through := ms(cutoff.Add(time.Hour)), ms(live.End)
		hints, _, err := newProvider(d).ProvideHints(context.Background(), "t", expr, from, through)
		require.NoError(t, err)
		require.Equal(t, []HintTimeRange{live}, hints.TimeRanges)
		require.Equal(t, from, d.from)
	})

	t.Run("query past retention passes through without a lookup", func(t *testing.T) {
		d := &recordingProvider{}
		hints, _, err := newProvider(d).ProvideHints(context.Background(), "t", expr,
			ms(cutoff.Add(-60*24*time.Hour)), ms(cutoff.Add(-24*time.Hour)))
		require.NoError(t, err)
		require.Zero(t, d.calls)
		require.Len(t, hints.TimeRanges, 1)
		require.True(t, hints.TimeRanges[0].IsPassthrough())
		require.Equal(t, cutoff, hints.TimeRanges[0].End)
		require.Equal(t, HintSourcePastRetention, hints.TimeRanges[0].Source)
	})

	t.Run("query ending at the cutoff still looks up that instant", func(t *testing.T) {
		d := &recordingProvider{hints: &Hints{}}
		_, _, err := newProvider(d).ProvideHints(context.Background(), "t", expr, ms(cutoff.Add(-time.Hour)), ms(cutoff))
		require.NoError(t, err)
		require.Equal(t, 1, d.calls)
	})

	t.Run("query spanning the cutoff unions passthrough with the lookup of the rest", func(t *testing.T) {
		d := &recordingProvider{hints: &Hints{TimeRanges: []HintTimeRange{live}}}
		hints, _, err := newProvider(d).ProvideHints(context.Background(), "t", expr,
			ms(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)), ms(live.End.Add(time.Minute)))
		require.NoError(t, err)
		// The delegate is only asked about the part the index covers.
		require.Equal(t, ms(cutoff), d.from)
		require.Len(t, hints.TimeRanges, 2)
		require.True(t, hints.TimeRanges[0].IsPassthrough())
		require.Equal(t, cutoff, hints.TimeRanges[0].End)
		require.Equal(t, live.Start, hints.TimeRanges[1].Start)
		require.Equal(t, live.End, hints.TimeRanges[1].End)
	})

	t.Run("delegate error is returned", func(t *testing.T) {
		boom := errors.New("boom")
		d := &recordingProvider{err: boom}
		_, _, err := newProvider(d).ProvideHints(context.Background(), "t", expr,
			ms(cutoff.Add(-time.Hour)), ms(cutoff.Add(time.Hour)))
		require.ErrorIs(t, err, boom)
	})

	t.Run("no retention leaves hints unchanged", func(t *testing.T) {
		d := &recordingProvider{hints: &Hints{TimeRanges: []HintTimeRange{live}}}
		p := NewRetentionHintProvider(d, nil)
		hints, _, err := p.ProvideHints(context.Background(), "t", expr, ms(cutoff.Add(-24*time.Hour)), ms(live.End))
		require.NoError(t, err)
		require.Equal(t, []HintTimeRange{live}, hints.TimeRanges)
	})
}

func TestRetentionHintProvider_MinDateIsTheDelegates(t *testing.T) {
	minDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := NewRetentionHintProvider(&recordingProvider{minDate: minDate}, fixedRetention(24*time.Hour))
	require.Equal(t, minDate, p.MinDate())
}
