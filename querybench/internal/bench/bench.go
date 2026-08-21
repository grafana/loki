// Package bench orchestrates a benchmark run: it executes each query a fixed
// number of times, waits for the backend metrics to settle, captures them, and
// persists the growing report after every query.
package bench

import (
	"context"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/lokiclient"
	"github.com/grafana/loki-query-benchmark/internal/queries"
	"github.com/grafana/loki-query-benchmark/internal/report"
)

// QueryRunner executes one query over a time range. *lokiclient.Client satisfies
// it; tests supply a fake.
type QueryRunner interface {
	Run(ctx context.Context, q queries.Query, start, end time.Time) (lokiclient.Result, error)
}

// MetricCapturer captures backend metrics for a run window. *metrics.Capturer
// satisfies it; tests supply a fake.
type MetricCapturer interface {
	Capture(ctx context.Context, metricsScrapeTime time.Time, window, runDuration time.Duration) report.SystemStats
}

// Runner drives a full benchmark run.
type Runner struct {
	client               QueryRunner
	capturer             MetricCapturer
	runs                 int
	end                  time.Time
	metricsScrapePadding time.Duration
	save                 func(*report.Report) error
	logf                 func(format string, args ...any)

	// now and sleep are injected so tests can drive time without real waiting. In
	// production they wrap time.Now and a context-aware sleep.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// Config configures a Runner.
type Config struct {
	// Client executes queries.
	Client QueryRunner
	// Capturer captures backend metrics after each query's runs.
	Capturer MetricCapturer
	// Runs is how many times each query executes, back-to-back.
	Runs int
	// End is the shared end anchor: every query's data window ends here.
	End time.Time
	// MetricsScrapePadding is added before and after the run window when capturing
	// metrics, to cover scrape delay. The tool also waits this long after a
	// query's runs before capturing, so the padded window is fully scraped.
	MetricsScrapePadding time.Duration
	// Save persists the report; it is called after every query so a crash keeps
	// the queries finished so far.
	Save func(*report.Report) error
	// Logf receives progress lines. It may be nil.
	Logf func(format string, args ...any)

	// Now and Sleep are optional test seams; production defaults are used when
	// nil.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// New returns a Runner from cfg.
func New(cfg Config) *Runner {
	logf := cfg.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = contextSleep
	}
	return &Runner{
		client:               cfg.Client,
		capturer:             cfg.Capturer,
		runs:                 cfg.Runs,
		end:                  cfg.End,
		metricsScrapePadding: cfg.MetricsScrapePadding,
		save:                 cfg.Save,
		logf:                 logf,
		now:                  now,
		sleep:                sleep,
	}
}

// Run executes every query in qs and fills base with the results, saving after
// each query. base carries the run parameters already; Run appends one entry per
// query and sets FinishedAt when the run completes.
//
// Run stops early and returns the error only when a save fails or the context is
// cancelled. A query whose executions all fail is still recorded, so a transient
// backend error never discards the rest of the run.
func (r *Runner) Run(ctx context.Context, base *report.Report, qs []queries.Query) error {
	for i, q := range qs {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.logf("[%d/%d] %s [%s]: running %d times", i+1, len(qs), q.Name, q.Type, r.runs)

		qr, runErr := r.runQuery(ctx, q)
		if runErr != nil {
			// The query was interrupted before its metrics were captured, so it is an
			// incomplete in-flight run; drop it rather than record a query with no
			// system metrics. Queries that finished earlier are already saved.
			return runErr
		}
		base.Queries = append(base.Queries, qr)
		if err := r.save(base); err != nil {
			return err
		}
	}
	end := r.now()
	base.FinishedAt = &end
	return r.save(base)
}

// runQuery executes one query r.runs times, waits for its metrics window to
// settle, and captures the backend metrics.
//
// It returns a non-nil error when the context is cancelled before the metrics
// are captured (during the runs or during the settle wait). The returned query
// is then incomplete — it carries no system metrics — so the caller drops it
// rather than recording it.
func (r *Runner) runQuery(ctx context.Context, q queries.Query) (report.Query, error) {
	reqStart, reqEnd := q.RequestRange(r.end)
	dataStart, dataEnd := q.DataRange(r.end)

	qr := report.Query{
		Name:             q.Name,
		Type:             q.Type,
		Expr:             q.Expr,
		Start:            dataStart,
		End:              dataEnd,
		StepSeconds:      q.Step.Seconds(),
		Runs:             r.runs,
		LatenciesSeconds: make([]float64, 0, r.runs),
	}

	execStart := r.now()
	qr.ExecutionStartedAt = execStart
	for i := 0; i < r.runs; i++ {
		if err := ctx.Err(); err != nil {
			qr.ExecutionFinishedAt = r.now()
			return qr, err
		}
		res, err := r.client.Run(ctx, q, reqStart, reqEnd)
		if err != nil {
			qr.FailedRuns++
			r.logf("  %s run %d/%d failed: %v", q.Name, i+1, r.runs, err)
			continue
		}
		qr.LatenciesSeconds = append(qr.LatenciesSeconds, res.Latency.Seconds())
		qr.QueryStats.ProcessedBytes += res.ProcessedBytes
	}
	execEnd := r.now()
	qr.ExecutionFinishedAt = execEnd

	runDuration := execEnd.Sub(execStart)
	metricsScrapeTime := execEnd.Add(r.metricsScrapePadding)
	window := runDuration + 2*r.metricsScrapePadding

	// Wait until the far edge of the metrics window is in the past, so the scrape
	// that covers the run end has landed before we query.
	if wait := metricsScrapeTime.Sub(r.now()); wait > 0 {
		r.logf("  %s: waiting %s for metrics to settle", q.Name, wait.Round(time.Second))
		if err := r.sleep(ctx, wait); err != nil {
			return qr, err
		}
	}
	qr.SystemMetrics = r.capturer.Capture(ctx, metricsScrapeTime, window, runDuration)
	return qr, nil
}

// contextSleep sleeps for d or until ctx is done, whichever comes first.
func contextSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
