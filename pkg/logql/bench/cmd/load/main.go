package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/bench"
)

func main() {
	var (
		addr        = flag.String("addr", "http://localhost:3100", "Loki address")
		orgID       = flag.String("org-id", "test-tenant", "Tenant ID (X-Scope-OrgID header)")
		concurrency = flag.Int("concurrency", 4, "Number of concurrent users (goroutines)")
		suite       = flag.String("suite", "fast", "Query suite: fast, regression, or exhaustive")
		dataDir     = flag.String("data-dir", bench.DefaultDataDir, "Path to generated data directory")
		queriesDir  = flag.String("queries-dir", "./queries", "Path to queries directory")
		limit       = flag.Int("limit", 100, "Max lines per query response")
	)
	flag.Parse()

	suites := parseSuites(*suite)
	cases := loadCases(*queriesDir, *dataDir, suites)
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "error: no queries loaded")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Loaded %d queries from suite %q\n", len(cases), *suite)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var stats Stats

	var wg sync.WaitGroup
	for i := range *concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, workerConfig{
				id:    i,
				addr:  *addr,
				orgID: *orgID,
				limit: *limit,
				cases: cases,
				stats: &stats,
			})
		}()
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	start := time.Now()
	var lastSnap statsSnapshot
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			elapsed := time.Since(start)
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "=== Summary ===")
			printStats(os.Stderr, stats.snapshot(), elapsed)
			return
		case <-ticker.C:
			snap := stats.snapshot()
			elapsed := time.Since(start)
			fmt.Fprintf(os.Stderr, "--- %s ---\n", elapsed.Truncate(time.Second))
			printStats(os.Stderr, snap, elapsed)
			printDelta(os.Stderr, snap, lastSnap)
			lastSnap = snap
		}
	}
}

func parseSuites(s string) []bench.Suite {
	switch s {
	case "fast":
		return []bench.Suite{bench.SuiteFast}
	case "regression":
		return []bench.Suite{bench.SuiteFast, bench.SuiteRegression}
	case "exhaustive":
		return []bench.Suite{bench.SuiteFast, bench.SuiteRegression, bench.SuiteExhaustive}
	default:
		fmt.Fprintf(os.Stderr, "error: invalid suite %q (must be fast, regression, or exhaustive)\n", s)
		os.Exit(1)
		return nil
	}
}

func loadCases(queriesDir, dataDir string, suites []bench.Suite) []bench.TestCase {
	registry := bench.NewQueryRegistry(queriesDir)
	if err := registry.Load(suites...); err != nil {
		fmt.Fprintf(os.Stderr, "error loading queries: %v\n", err)
		os.Exit(1)
	}

	metadata, err := bench.LoadMetadata(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading metadata: %v\n", err)
		os.Exit(1)
	}
	config, err := bench.LoadConfig(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	resolver := bench.NewMetadataVariableResolver(metadata, config.Seed)
	defs := registry.GetQueries(false, suites...)

	var all []bench.TestCase
	for _, def := range defs {
		expanded, err := registry.ExpandQuery(def, resolver, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping query %q: %v\n", def.Description, err)
			continue
		}
		all = append(all, expanded...)
	}
	return all
}

type workerConfig struct {
	id    int
	addr  string
	orgID string
	limit int
	cases []bench.TestCase
	stats *Stats
}

func worker(ctx context.Context, cfg workerConfig) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(cfg.id)))
	client := &http.Client{Timeout: 5 * time.Minute}

	for {
		if ctx.Err() != nil {
			return
		}

		tc := cfg.cases[rng.Intn(len(cfg.cases))]
		start := time.Now()
		status, err := executeQuery(ctx, client, cfg.addr, cfg.orgID, cfg.limit, tc)
		elapsed := time.Since(start)

		if ctx.Err() != nil {
			return
		}

		cfg.stats.record(elapsed, status, err)
	}
}

func executeQuery(ctx context.Context, client *http.Client, addr, orgID string, limit int, tc bench.TestCase) (int, error) {
	var endpoint string
	params := url.Values{}

	if tc.Kind() == "metric" {
		endpoint = addr + "/loki/api/v1/query_range"
		params.Set("query", tc.Query)
		params.Set("start", strconv.FormatInt(tc.Start.UnixNano(), 10))
		params.Set("end", strconv.FormatInt(tc.End.UnixNano(), 10))
		params.Set("limit", strconv.Itoa(limit))
		params.Set("direction", tc.Direction.String())
		if tc.Step > 0 {
			params.Set("step", fmt.Sprintf("%d", int64(tc.Step.Seconds())))
		}
	} else {
		endpoint = addr + "/loki/api/v1/query_range"
		params.Set("query", tc.Query)
		params.Set("start", strconv.FormatInt(tc.Start.UnixNano(), 10))
		params.Set("end", strconv.FormatInt(tc.End.UnixNano(), 10))
		params.Set("limit", strconv.Itoa(limit))
		params.Set("direction", tc.Direction.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Scope-OrgID", orgID)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// Stats holds atomic counters for aggregate tracking.
type Stats struct {
	total    atomic.Int64
	success  atomic.Int64
	errors   atomic.Int64
	http4xx  atomic.Int64
	http5xx  atomic.Int64
	totalMs  atomic.Int64
	minMs    atomic.Int64
	maxMs    atomic.Int64
	initOnce sync.Once
}

func (s *Stats) record(d time.Duration, status int, err error) {
	ms := d.Milliseconds()
	s.total.Add(1)
	s.totalMs.Add(ms)

	s.initOnce.Do(func() { s.minMs.Store(ms) })

	for {
		cur := s.minMs.Load()
		if ms >= cur || s.minMs.CompareAndSwap(cur, ms) {
			break
		}
	}
	for {
		cur := s.maxMs.Load()
		if ms <= cur || s.maxMs.CompareAndSwap(cur, ms) {
			break
		}
	}

	if err != nil {
		s.errors.Add(1)
		return
	}
	switch {
	case status >= 200 && status < 300:
		s.success.Add(1)
	case status >= 400 && status < 500:
		s.http4xx.Add(1)
	case status >= 500:
		s.http5xx.Add(1)
	default:
		s.success.Add(1)
	}
}

type statsSnapshot struct {
	total, success, errors, http4xx, http5xx int64
	totalMs, minMs, maxMs                    int64
}

func (s *Stats) snapshot() statsSnapshot {
	return statsSnapshot{
		total:   s.total.Load(),
		success: s.success.Load(),
		errors:  s.errors.Load(),
		http4xx: s.http4xx.Load(),
		http5xx: s.http5xx.Load(),
		totalMs: s.totalMs.Load(),
		minMs:   s.minMs.Load(),
		maxMs:   s.maxMs.Load(),
	}
}

func printStats(w *os.File, s statsSnapshot, elapsed time.Duration) {
	var avgMs int64
	if s.total > 0 {
		avgMs = s.totalMs / s.total
	}
	qps := float64(s.success) / elapsed.Seconds()
	fmt.Fprintf(w, "  total=%-8d ok=%-8d 4xx=%-6d 5xx=%-6d err=%-6d qps=%.1f  latency min=%dms avg=%dms max=%dms\n",
		s.total, s.success, s.http4xx, s.http5xx, s.errors, qps, s.minMs, avgMs, s.maxMs)
}

func printDelta(w *os.File, cur, prev statsSnapshot) {
	dt := cur.total - prev.total
	dok := cur.success - prev.success
	d4 := cur.http4xx - prev.http4xx
	d5 := cur.http5xx - prev.http5xx
	de := cur.errors - prev.errors
	var davg int64
	if dt > 0 {
		davg = (cur.totalMs - prev.totalMs) / dt
	}
	fmt.Fprintf(w, "  delta: +%d queries (+%d ok, +%d 4xx, +%d 5xx, +%d err) avg=%dms\n",
		dt, dok, d4, d5, de, davg)
}
