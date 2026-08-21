package bench

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/lokiclient"
	"github.com/grafana/loki-query-benchmark/internal/queries"
	"github.com/grafana/loki-query-benchmark/internal/report"
)

// fakeClient returns a fixed latency and byte count, failing the runs whose
// 1-based index is in failOn.
type fakeClient struct {
	latency time.Duration
	bytes   int64
	failOn  map[int]bool
	calls   int
}

func (c *fakeClient) Run(_ context.Context, _ queries.Query, _, _ time.Time) (lokiclient.Result, error) {
	c.calls++
	if c.failOn[c.calls] {
		return lokiclient.Result{}, errors.New("boom")
	}
	return lokiclient.Result{Latency: c.latency, ProcessedBytes: c.bytes}, nil
}

// captureCall records one Capture invocation.
type captureCall struct {
	metricsScrapeTime time.Time
	window            time.Duration
	runDuration       time.Duration
}

type fakeCapturer struct {
	calls []captureCall
	value uint64
}

func (c *fakeCapturer) Capture(_ context.Context, metricsScrapeTime time.Time, window, runDuration time.Duration) report.SystemStats {
	c.calls = append(c.calls, captureCall{metricsScrapeTime, window, runDuration})
	v := c.value
	return report.SystemStats{ObjstoreRequests: &v}
}

// clock returns times that advance by step on each call, giving a run duration
// of one step per query without any real waiting.
type clock struct {
	cur  time.Time
	step time.Duration
}

func (c *clock) now() time.Time {
	t := c.cur
	c.cur = c.cur.Add(c.step)
	return t
}

func TestRun_RecordsLatenciesBytesAndMetrics(t *testing.T) {
	client := &fakeClient{latency: 250 * time.Millisecond, bytes: 1000}
	cap := &fakeCapturer{value: 42}
	var slept []time.Duration
	var saves int

	end := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	r := New(Config{
		Client:               client,
		Capturer:             cap,
		Runs:                 3,
		End:                  end,
		MetricsScrapePadding: 2 * time.Minute,
		Save:                 func(*report.Report) error { saves++; return nil },
		Now:                  (&clock{cur: time.Unix(1_000_000, 0).UTC(), step: time.Minute}).now,
		Sleep:                func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	})

	base := &report.Report{}
	qs := []queries.Query{{Name: "range/x", Type: queries.TypeRange, Expr: "sum(x)", Window: 6 * time.Hour, Step: 5 * time.Minute}}
	if err := r.Run(context.Background(), base, qs); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(base.Queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(base.Queries))
	}
	q := base.Queries[0]

	if len(q.LatenciesSeconds) != 3 {
		t.Errorf("latencies = %d, want 3", len(q.LatenciesSeconds))
	}
	for _, l := range q.LatenciesSeconds {
		if l != 0.25 {
			t.Errorf("latency = %v, want 0.25", l)
		}
	}
	if q.QueryStats.ProcessedBytes != 3000 {
		t.Errorf("ProcessedBytes = %d, want 3000", q.QueryStats.ProcessedBytes)
	}
	if q.FailedRuns != 0 {
		t.Errorf("FailedRuns = %d, want 0", q.FailedRuns)
	}

	// The query's data window ends at the anchor and spans its window.
	if !q.End.Equal(end) || !q.Start.Equal(end.Add(-6*time.Hour)) {
		t.Errorf("data range = [%s, %s], want [%s, %s]", q.Start, q.End, end.Add(-6*time.Hour), end)
	}

	// Metrics: one capture, whose window and eval time derive from the recorded
	// execution window and the padding.
	if len(cap.calls) != 1 {
		t.Fatalf("capture calls = %d, want 1", len(cap.calls))
	}
	c := cap.calls[0]
	runDur := q.ExecutionFinishedAt.Sub(q.ExecutionStartedAt)
	if c.runDuration != runDur {
		t.Errorf("capture runDuration = %s, want %s", c.runDuration, runDur)
	}
	if want := runDur + 2*2*time.Minute; c.window != want {
		t.Errorf("capture window = %s, want %s", c.window, want)
	}
	if want := q.ExecutionFinishedAt.Add(2 * time.Minute); !c.metricsScrapeTime.Equal(want) {
		t.Errorf("capture metricsScrapeTime = %s, want %s", c.metricsScrapeTime, want)
	}
	if q.SystemMetrics.ObjstoreRequests == nil || *q.SystemMetrics.ObjstoreRequests != 42 {
		t.Errorf("system metrics not stored: %v", q.SystemMetrics.ObjstoreRequests)
	}

	// Saved after the query and once more at the end; FinishedAt set.
	if saves != 2 {
		t.Errorf("saves = %d, want 2 (per-query + final)", saves)
	}
	if base.FinishedAt == nil {
		t.Error("FinishedAt not set")
	}
	if len(slept) != 1 {
		t.Errorf("sleep calls = %d, want 1", len(slept))
	}
}

func TestRun_FailedRunsCountedNotFatal(t *testing.T) {
	client := &fakeClient{latency: time.Second, bytes: 10, failOn: map[int]bool{2: true}}
	r := New(Config{
		Client:               client,
		Capturer:             &fakeCapturer{},
		Runs:                 3,
		End:                  time.Unix(0, 0),
		MetricsScrapePadding: 0,
		Save:                 func(*report.Report) error { return nil },
		Now:                  (&clock{cur: time.Unix(0, 0), step: time.Second}).now,
		Sleep:                func(context.Context, time.Duration) error { return nil },
	})

	base := &report.Report{}
	qs := []queries.Query{{Name: "i", Type: queries.TypeInstant, Expr: "x", Window: time.Hour}}
	if err := r.Run(context.Background(), base, qs); err != nil {
		t.Fatalf("Run: %v", err)
	}
	q := base.Queries[0]
	if q.FailedRuns != 1 {
		t.Errorf("FailedRuns = %d, want 1", q.FailedRuns)
	}
	if len(q.LatenciesSeconds) != 2 {
		t.Errorf("latencies = %d, want 2 successful", len(q.LatenciesSeconds))
	}
	if q.QueryStats.ProcessedBytes != 20 {
		t.Errorf("ProcessedBytes = %d, want 20 (only successful runs)", q.QueryStats.ProcessedBytes)
	}
}

// cancelClient cancels its context on the first call, so the next loop
// iteration observes the cancellation.
type cancelClient struct{ cancel context.CancelFunc }

func (c *cancelClient) Run(_ context.Context, _ queries.Query, _, _ time.Time) (lokiclient.Result, error) {
	c.cancel()
	return lokiclient.Result{Latency: time.Second, ProcessedBytes: 1}, nil
}

func TestRun_ContextCancelledDropsInflightQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := New(Config{
		Client:   &cancelClient{cancel: cancel},
		Capturer: &fakeCapturer{},
		Runs:     3,
		End:      time.Unix(0, 0),
		Save:     func(*report.Report) error { return nil },
		Now:      (&clock{cur: time.Unix(0, 0), step: time.Second}).now,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})

	base := &report.Report{}
	qs := []queries.Query{{Name: "i", Type: queries.TypeInstant, Expr: "x", Window: time.Hour}}
	err := r.Run(ctx, base, qs)
	if err == nil {
		t.Fatal("expected a context-cancellation error")
	}
	// The query was cancelled before its metrics were captured, so it is dropped,
	// and the run does not mark itself finished.
	if len(base.Queries) != 0 {
		t.Fatalf("interrupted query must be dropped, got %d", len(base.Queries))
	}
	if base.FinishedAt != nil {
		t.Error("FinishedAt must stay nil on a cancelled run")
	}
}

func TestRun_SaveErrorStops(t *testing.T) {
	r := New(Config{
		Client:   &fakeClient{},
		Capturer: &fakeCapturer{},
		Runs:     1,
		End:      time.Unix(0, 0),
		Save:     func(*report.Report) error { return errors.New("disk full") },
		Now:      (&clock{cur: time.Unix(0, 0), step: time.Second}).now,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
	qs := []queries.Query{{Name: "i", Type: queries.TypeInstant, Expr: "x", Window: time.Hour}}
	if err := r.Run(context.Background(), &report.Report{}, qs); err == nil {
		t.Fatal("expected Run to fail when Save fails")
	}
}

func TestRun_CancelDuringSettleWaitDropsInflightKeepsCompleted(t *testing.T) {
	client := &fakeClient{latency: time.Second, bytes: 5}
	cap := &fakeCapturer{value: 9}
	// The first query's settle wait completes; the second's is cancelled.
	sleepCalls := 0
	sleep := func(context.Context, time.Duration) error {
		sleepCalls++
		if sleepCalls >= 2 {
			return context.Canceled
		}
		return nil
	}
	r := New(Config{
		Client:               client,
		Capturer:             cap,
		Runs:                 1,
		End:                  time.Unix(0, 0),
		MetricsScrapePadding: time.Minute, // forces a positive settle wait
		Save:                 func(*report.Report) error { return nil },
		Now:                  (&clock{cur: time.Unix(0, 0), step: time.Second}).now,
		Sleep:                sleep,
	})

	base := &report.Report{}
	qs := []queries.Query{
		{Name: "done", Type: queries.TypeInstant, Expr: "x", Window: time.Hour},
		{Name: "inflight", Type: queries.TypeInstant, Expr: "y", Window: time.Hour},
	}
	if err := r.Run(context.Background(), base, qs); err == nil {
		t.Fatal("expected an error when the settle wait is interrupted")
	}
	// Only the first (fully captured) query is recorded; the interrupted one is dropped.
	if len(base.Queries) != 1 || base.Queries[0].Name != "done" {
		t.Fatalf("expected only the completed query recorded, got %v", names(base.Queries))
	}
	if base.Queries[0].SystemMetrics.ObjstoreRequests == nil {
		t.Error("the completed query must keep its captured metrics")
	}
	if len(cap.calls) != 1 {
		t.Errorf("Capture should run once (for the completed query), got %d", len(cap.calls))
	}
	if base.FinishedAt != nil {
		t.Error("FinishedAt must stay nil on an interrupted run")
	}
}

func names(qs []report.Query) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = q.Name
	}
	return out
}

func TestRun_MultipleQueriesRecordedInOrder(t *testing.T) {
	// Runs=2, so calls 3 and 4 are the middle query's two runs; fail both.
	client := &fakeClient{latency: time.Second, bytes: 5, failOn: map[int]bool{3: true, 4: true}}
	cap := &fakeCapturer{value: 7}
	var saves int
	r := New(Config{
		Client:   client,
		Capturer: cap,
		Runs:     2,
		End:      time.Unix(0, 0),
		Save:     func(*report.Report) error { saves++; return nil },
		Now:      (&clock{cur: time.Unix(0, 0), step: time.Second}).now,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})

	base := &report.Report{}
	qs := []queries.Query{
		{Name: "a", Type: queries.TypeInstant, Expr: "x", Window: time.Hour},
		{Name: "b", Type: queries.TypeInstant, Expr: "x", Window: time.Hour},
		{Name: "c", Type: queries.TypeInstant, Expr: "x", Window: time.Hour},
	}
	if err := r.Run(context.Background(), base, qs); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := []string{base.Queries[0].Name, base.Queries[1].Name, base.Queries[2].Name}; got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("queries recorded out of order: %v", got)
	}
	if saves != 4 {
		t.Errorf("saves = %d, want 4 (one per query + final)", saves)
	}
	// The fully-failed middle query does not stop the run and still gets metrics.
	if base.Queries[1].FailedRuns != 2 || len(base.Queries[1].LatenciesSeconds) != 0 {
		t.Errorf("middle query: FailedRuns=%d latencies=%d, want 2 and 0", base.Queries[1].FailedRuns, len(base.Queries[1].LatenciesSeconds))
	}
	if base.Queries[2].SystemMetrics.ObjstoreRequests == nil {
		t.Error("the query after an all-failed query should still capture metrics")
	}
}
