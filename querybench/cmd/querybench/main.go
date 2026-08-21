// Command querybench benchmarks a fixed set of LogQL queries against a Loki
// query-frontend over a fixed time range, and captures the backend cost of each
// query from the cell's metrics.
//
// It runs each query a fixed number of times back-to-back, then pauses to let
// the backend metrics settle and captures them for that query's run window. The
// result is written to a JSON report, rewritten after every query so a crash
// keeps the finished queries.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/bench"
	"github.com/grafana/loki-query-benchmark/internal/lokiclient"
	"github.com/grafana/loki-query-benchmark/internal/metrics"
	"github.com/grafana/loki-query-benchmark/internal/queries"
	"github.com/grafana/loki-query-benchmark/internal/report"
)

func main() {
	// Run the whole process in UTC so time.Now() (report started/execution/finished
	// times) and logs are UTC. Reports are therefore always written in UTC.
	time.Local = time.UTC
	log.SetFlags(log.LstdFlags)
	if err := run(); err != nil {
		log.Fatalf("querybench: %v", err)
	}
}

func run() error {
	var (
		url                  = flag.String("url", "http://localhost:3199", "Loki query-frontend base URL")
		tenant               = flag.String("tenant", "", "tenant id sent as X-Scope-OrgID (required)")
		runs                 = flag.Int("runs", 10, "how many times each query runs, sequentially")
		minStartFlag         = flag.String("query-min-start-time", "", "earliest time any query may read; queries reaching before it are skipped (RFC3339 or unix seconds, required)")
		endFlag              = flag.String("query-end-time", "", "end time every query ends at (RFC3339 or unix seconds, required)")
		backendNamespace     = flag.String("backend-namespace", "", "namespace of the backend Loki cell, for metric capture (required)")
		reportDir            = flag.String("report-dir", ".", "directory the JSON report is written to")
		reportDesc           = flag.String("report-description", "", "free-text description stored in the report")
		datasource           = flag.String("metrics-datasource", "2z9d6ElGk", "gcx Prometheus datasource UID for metric capture")
		metricsScrapePadding = flag.Duration("metrics-scrape-padding", 2*time.Minute, "time added before and after the run window when capturing metrics, and the wait after each query's runs before capturing, to cover scrape delay")
		queryTimeout         = flag.Duration("query-timeout", 120*time.Second, "per-query request timeout")
		queryFilter          = flag.String("query-filter", "", "only run queries whose name or expression matches this regex (default: all)")
	)
	flag.Parse()

	if *tenant == "" || *minStartFlag == "" || *endFlag == "" || *backendNamespace == "" {
		flag.Usage()
		return fmt.Errorf("-tenant, -query-min-start-time, -query-end-time and -backend-namespace are required")
	}
	if *runs < 1 {
		return fmt.Errorf("-runs must be at least 1")
	}

	// Metric capture shells out to gcx, so fail fast if it is missing rather than
	// after running every query only to log a wall of capture failures.
	if _, err := exec.LookPath("gcx"); err != nil {
		return fmt.Errorf("gcx not found on PATH (needed to capture backend metrics): %w", err)
	}

	start, err := parseTime(*minStartFlag)
	if err != nil {
		return fmt.Errorf("-query-min-start-time: %w", err)
	}
	end, err := parseTime(*endFlag)
	if err != nil {
		return fmt.Errorf("-query-end-time: %w", err)
	}

	if !end.After(start) {
		return fmt.Errorf("-query-end-time %s must be after -query-min-start-time %s", end.UTC().Format(time.RFC3339), start.UTC().Format(time.RFC3339))
	}

	qs, err := queries.FilterByRegex(queries.Default(), *queryFilter)
	if err != nil {
		return err
	}
	if len(qs) == 0 {
		return fmt.Errorf("no queries selected (-query-filter %q matched nothing)", *queryFilter)
	}
	if err := queries.Validate(qs); err != nil {
		return err
	}
	qs, skipped := queries.FilterByDataRange(qs, start, end)
	for _, q := range skipped {
		ds, _ := q.DataRange(end)
		log.Printf("skipping %s: data reaches back to %s, before -query-min-start-time %s",
			q.Name, ds.UTC().Format(time.RFC3339), start.UTC().Format(time.RFC3339))
	}
	if len(qs) == 0 {
		return fmt.Errorf("all queries skipped: every data range starts before -query-min-start-time %s; widen the query time range", start.UTC().Format(time.RFC3339))
	}

	startedAt := time.Now()
	path, err := report.Create(*reportDir, startedAt)
	if err != nil {
		return err
	}

	base := &report.Report{
		Description:      *reportDesc,
		LokiURL:          *url,
		Tenant:           *tenant,
		BackendNamespace: *backendNamespace,
		RequestedStart:   start,
		RequestedEnd:     end,
		StartedAt:        startedAt,
	}

	client := lokiclient.New(*url, *tenant, lokiclient.Options{
		Timeout: *queryTimeout,
		Logf:    log.Printf,
	})
	capturer := metrics.New(metrics.Options{
		Datasource: *datasource,
		Namespace:  *backendNamespace,
		Logf:       log.Printf,
	})
	runner := bench.New(bench.Config{
		Client:               client,
		Capturer:             capturer,
		Runs:                 *runs,
		End:                  end,
		MetricsScrapePadding: *metricsScrapePadding,
		Save:                 func(r *report.Report) error { return report.Write(path, r) },
		Logf:                 log.Printf,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("report: %s", path)
	log.Printf("queries: %d, runs each: %d, end anchor: %s, metrics scrape padding: %s",
		len(qs), *runs, end.UTC().Format(time.RFC3339), *metricsScrapePadding)

	if err := runner.Run(ctx, base, qs); err != nil {
		return fmt.Errorf("run: %w (partial report at %s)", err, path)
	}

	// A run where every execution failed still writes a report and would otherwise
	// exit 0; treat an all-failure run as an error so a broken target (wrong URL,
	// down frontend, bad tenant) is not mistaken for a successful benchmark.
	var succeeded, failed int
	for i := range base.Queries {
		succeeded += len(base.Queries[i].LatenciesSeconds)
		failed += base.Queries[i].FailedRuns
	}
	if failed > 0 {
		log.Printf("completed with %d failed query executions", failed)
	}
	if succeeded == 0 {
		return fmt.Errorf("every query execution failed (%d failures); see %s", failed, path)
	}
	log.Printf("done: %s", path)
	return nil
}

// parseTime accepts a unix-seconds integer or an RFC3339 timestamp, always
// returning the instant in UTC (an RFC3339 offset like +02:00 keeps its offset
// through time.Parse regardless of time.Local, so normalize it here).
func parseTime(s string) (time.Time, error) {
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected unix seconds or RFC3339, got %q", s)
	}
	return t.UTC(), nil
}
