package hintprovider

import (
	"context"
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
	hints *Hints
	calls int
	from  model.Time
}

func (p *recordingProvider) ProvideHints(_ context.Context, _ string, _ syntax.Expr, from, _ model.Time) (*Hints, *QueryStats, error) {
	p.calls++
	p.from = from
	return p.hints, NewQueryStats(), nil
}

func TestRetentionHintProvider_Cutoff(t *testing.T) {
	afternoon := time.Date(2026, 11, 17, 13, 30, 0, 0, time.UTC)
	midnight := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	days90 := fixedRetention(90 * 24 * time.Hour)

	for _, tc := range []struct {
		name      string
		now       time.Time
		retention *fixedRetention
		want      time.Time
	}{
		{"nil retention", afternoon, nil, time.Time{}},
		{"zero retention", afternoon, new(fixedRetention), time.Time{}},
		{"rounds up to the next UTC day", afternoon, &days90, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)},
		{"day boundary stays", midnight, &days90, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewRetentionHintProvider(&recordingProvider{}, nil)
			if tc.retention != nil {
				p.retention = *tc.retention
			}
			p.now = func() time.Time { return tc.now }
			require.Equal(t, tc.want, p.cutoff())
		})
	}
}

func TestRetentionHintProvider_ProvideHints(t *testing.T) {
	cutoff := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC) // 90 days before the fixed now, rounded up
	live := HintTimeRange{Start: time.Date(2026, 10, 1, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 10, 1, 10, 1, 10, 0, time.UTC)}
	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	ms := func(ts time.Time) model.Time { return model.TimeFromUnixNano(ts.UnixNano()) }

	for _, tc := range []struct {
		name          string
		from, through time.Time
		wantCalls     int
		wantFrom      time.Time // what the delegate is asked, when called
		wantRanges    []HintTimeRange
	}{
		{
			name: "inside retention is untouched",
			from: cutoff.Add(time.Hour), through: live.End,
			wantCalls: 1, wantFrom: cutoff.Add(time.Hour), wantRanges: []HintTimeRange{live},
		},
		{
			name: "past retention passes through without a lookup",
			from: cutoff.Add(-60 * 24 * time.Hour), through: cutoff.Add(-time.Hour),
			wantRanges: []HintTimeRange{{End: cutoff, Source: HintSourcePastRetention}},
		},
		{
			name: "ending at the cutoff still looks up that instant",
			from: cutoff.Add(-time.Hour), through: cutoff,
			wantCalls: 1, wantFrom: cutoff, wantRanges: []HintTimeRange{{End: cutoff, Source: HintSourcePastRetention}, live},
		},
		{
			name: "spanning the cutoff unions the passthrough with the lookup of the rest",
			from: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC), through: live.End.Add(time.Minute),
			wantCalls: 1, wantFrom: cutoff, wantRanges: []HintTimeRange{{End: cutoff, Source: HintSourcePastRetention}, live},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delegate := &recordingProvider{hints: &Hints{TimeRanges: []HintTimeRange{live}}}
			p := NewRetentionHintProvider(delegate, fixedRetention(90*24*time.Hour))
			p.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }

			hints, _, err := p.ProvideHints(context.Background(), "t", expr, ms(tc.from), ms(tc.through))
			require.NoError(t, err)
			require.Equal(t, tc.wantCalls, delegate.calls)
			if tc.wantCalls > 0 {
				require.Equal(t, ms(tc.wantFrom), delegate.from)
			}
			require.Len(t, hints.TimeRanges, len(tc.wantRanges))
			for i, want := range tc.wantRanges {
				require.Equal(t, want.Start, hints.TimeRanges[i].Start)
				require.Equal(t, want.End, hints.TimeRanges[i].End)
			}
		})
	}
}
