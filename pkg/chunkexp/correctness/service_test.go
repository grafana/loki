package correctness

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type fakeQuerier struct {
	names         []string
	values        map[string][]string
	lines         []Line
	filterByRange bool

	mu         sync.Mutex
	queries    int
	appearOn   int
	sample     Line
	queryStart time.Time
	queryEnd   time.Time
}

func (f *fakeQuerier) LabelNames(context.Context, time.Time, time.Time) ([]string, error) {
	return f.names, nil
}

func (f *fakeQuerier) LabelValues(_ context.Context, name string, _, _ time.Time) ([]string, error) {
	return f.values[name], nil
}

func (f *fakeQuerier) Query(_ context.Context, _ string, start, end time.Time, limit int) ([]Line, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries++
	f.queryStart = start
	f.queryEnd = end
	if f.filterByRange {
		var out []Line
		for _, line := range f.lines {
			if line.Timestamp.Before(start) || !line.Timestamp.Before(end) {
				continue
			}
			out = append(out, line)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		return out, nil
	}
	if f.appearOn > 0 && f.queries >= f.appearOn {
		return []Line{f.sample}, nil
	}
	if f.appearOn == 0 {
		return f.lines, nil
	}
	return nil, nil
}

func requireSampleInstant(t *testing.T, exp *fakeQuerier, sample Line) {
	t.Helper()
	require.False(t, sample.Timestamp.Before(exp.queryStart))
	require.True(t, sample.Timestamp.Before(exp.queryEnd))
	require.LessOrEqual(t, exp.queryEnd.Sub(exp.queryStart), time.Millisecond)
}

func TestCycleRetryThenMatch(t *testing.T) {
	sample := Line{Timestamp: time.Unix(100, 0).UTC(), Line: "session e5e650e85685 done", Labels: `{app="browser"}`}
	stable := &fakeQuerier{
		names:  []string{"app"},
		values: map[string][]string{"app": {"browser"}},
		lines:  []Line{sample},
	}
	exp := &fakeQuerier{appearOn: 2, sample: sample}
	s := testService(t, stable, exp)
	s.cfg.LagGrace = 10 * time.Second
	s.cfg.RetryInterval = 10 * time.Second

	report, err := s.runCycle(context.Background())
	require.NoError(t, err)
	require.True(t, report.Correct)
	require.Equal(t, "e5e650e85685", report.Needle)
	require.Equal(t, `{app="browser"}`, report.Selector)
	require.GreaterOrEqual(t, exp.queries, 2)
	requireSampleInstant(t, exp, sample)
}

func TestCycleMissingIsIncorrect(t *testing.T) {
	sample := Line{Timestamp: time.Unix(100, 0).UTC(), Line: "session e5e650e85685 done", Labels: `{app="browser"}`}
	stable := &fakeQuerier{
		names:  []string{"app"},
		values: map[string][]string{"app": {"browser"}},
		lines:  []Line{sample},
	}
	exp := &fakeQuerier{}
	s := testService(t, stable, exp)
	s.cfg.LagGrace = 0

	report, err := s.runCycle(context.Background())
	require.NoError(t, err)
	require.False(t, report.Correct)
	require.Empty(t, report.SkippedReason)
	require.Equal(t, 1, exp.queries)
}

func TestCycleManyMatchesStillCorrect(t *testing.T) {
	lines := make([]Line, 150)
	base := time.Unix(200, 0).UTC()
	for i := range lines {
		lines[i] = Line{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Line:      "session e5e650e85685 done",
			Labels:    `{app="browser"}`,
		}
	}
	stable := &fakeQuerier{
		names:  []string{"app"},
		values: map[string][]string{"app": {"browser"}},
		lines:  lines,
	}
	exp := &fakeQuerier{lines: lines, filterByRange: true}
	s := testService(t, stable, exp)
	s.cfg.LogQueryLimit = 100
	s.cfg.LagGrace = 0

	report, err := s.runCycle(context.Background())
	require.NoError(t, err)
	require.True(t, report.Correct)
	require.Empty(t, report.SkippedReason)
	requireSampleInstant(t, exp, report.Sample)
	require.Equal(t, 1, exp.queries)
}

func TestCycleSkipsEmptySample(t *testing.T) {
	stable := &fakeQuerier{}
	exp := &fakeQuerier{}
	s := testService(t, stable, exp)

	report, err := s.runCycle(context.Background())
	require.NoError(t, err)
	require.Equal(t, skipNoLabels, report.SkippedReason)
	require.Equal(t, 0, exp.queries)
}

func testService(t *testing.T, stable, exp *fakeQuerier) *Service {
	t.Helper()
	cfg := Config{
		StableEndpoint: "http://stable",
		ExpEndpoint:    "http://exp",
		TenantID:       "29",
		QueryInterval:  time.Second,
		Settle:         0,
		MaxLookback:    time.Hour,
		LagGrace:       time.Minute,
		RetryInterval:  time.Second,
		RequestTimeout: time.Second,
		CycleTimeout:   time.Minute,
		LogQueryLimit:  10,
		StartedAt:      time.Unix(1, 0).UTC(),
	}
	s := newService(cfg, stable, exp, log.NewNopLogger(), prometheus.NewPedanticRegistry())
	now := time.Unix(1_000, 0).UTC()
	s.nowFn = func() time.Time { return now }
	s.sleep = func(context.Context, time.Duration) error {
		now = now.Add(s.cfg.RetryInterval)
		return nil
	}
	return s
}
