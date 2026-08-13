// Command loadtest drives heavy, decomposable LogQL metric queries at a Loki cell to exercise the
// stream-first execution path. Every query is a range aggregation (count_over_time/sum/rate/bytes)
// over a broad selector that matches many streams, so with the stream-ordered-execution flag on the
// engine reads those streams per-stream rather than time-ordered.
//
// Stream-first lives in the v1 engine. A cell may also run the v2/thor engine, which the querier
// picks for any query whose window overlaps data older than query_engine.storage_lag (default a few
// hours). Such queries bypass v1 stream-first entirely. The report prints the engine and resolved
// order per query so this is visible; keep windows within the v1 range with -max-window (below the
// cell's storage_lag) to force v1.
//
// Point it at a query-frontend (e.g. via `kubectl -n loki-dev-002 port-forward svc/query-frontend
// 3199:3100`) and pass the tenant via -tenant. Defaults are deliberately modest; scale with
// -concurrency and -duration.
//
//	go run ./loadtest -url http://localhost:3199 -tenant 156331 -concurrency 8 -duration 2m
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"go.uber.org/atomic"
)

type queryDef struct {
	name  string
	kind  string // "instant" or "range"
	expr  string // %s is replaced by the selector
	inRng time.Duration
	step  time.Duration // range queries only
}

type sample struct {
	latency     time.Duration
	status      int
	bytes       int64
	streams     int64 // input cardinality: index streams matched
	series      int64 // output cardinality: series in the result
	usedV2      bool  // query ran (at least partly) on the v2/thor engine, bypassing v1 stream-first
	streamFirst int64 // v1 sub-evaluations resolved to stream-first order (0 if the build lacks the counter)
	tsFirst     int64 // v1 sub-evaluations resolved to timestamp-first order
	ok          bool
}

func main() {
	var (
		endpoint  = flag.String("url", "http://localhost:3199", "Loki query-frontend base URL")
		tenant    = flag.String("tenant", "156331", "tenant id sent as X-Scope-OrgID")
		selector  = flag.String("selector", `{service_name=~".+"}`, "broad stream selector (heavy input)")
		qtype     = flag.String("type", "range", "query type to run: range | instant")
		maxWindow = flag.Duration("max-window", 0, "cap each range query's lookback; set below the cell's query_engine.storage_lag (e.g. 2h) so windows stay on the v1 engine and hit stream-first, not the v2/thor engine (0 = no cap)")
		conc      = flag.Int("concurrency", 4, "number of parallel workers")
		duration  = flag.Duration("duration", 60*time.Second, "total run duration (ignored when -count > 0)")
		count     = flag.Int("count", 0, "run exactly this many queries then stop (0 = use -duration). Use -concurrency 1 for a fully deterministic run")
		timeout   = flag.Duration("timeout", 120*time.Second, "per-request timeout")
		userAgent = flag.String("user-agent", "loki-stream-first-loadtest", "User-Agent header, so query-stats logs can be filtered by it")
		noCache   = flag.Bool("no-cache", true, "send Cache-Control: no-cache to bypass the query-frontend results cache")
		endFlag   = flag.Int64("end", 0, "query end timestamp as unix seconds shared by every query (0 = now)")
		verbose   = flag.Bool("verbose", false, "log each query (type, eval time, latency) as it runs")
	)
	flag.Parse()

	// Every query uses the same end timestamp (now, or -end). No randomness anywhere: query selection
	// is round-robin and the time window is fixed, so a run is deterministic (given the same end).
	endAnchor := time.Now()
	if *endFlag != 0 {
		endAnchor = time.Unix(*endFlag, 0)
	}

	// Query mix — all decomposable range aggregations, so the stream-first path is eligible. The
	// selector matches many streams (heavy input); grouping stays small so results don't blow up.
	// Both instant and range variants reach up to 24h of data (the widest window is 24h).
	allQueries := []queryDef{
		// instant: a [W] range-vector at a single point, W up to 24h.
		{"instant/count_5m", "instant", `sum(count_over_time(%s[5m]))`, 5 * time.Minute, 0},
		{"instant/count_1h", "instant", `sum(count_over_time(%s[1h]))`, time.Hour, 0},
		{"instant/count_6h", "instant", `sum(count_over_time(%s[6h]))`, 6 * time.Hour, 0},
		{"instant/count_24h", "instant", `sum(count_over_time(%s[24h]))`, 24 * time.Hour, 0},
		{"instant/rate_1h", "instant", `sum(rate(%s[1h]))`, time.Hour, 0},
		{"instant/bytes_1h", "instant", `sum(bytes_over_time(%s[1h]))`, time.Hour, 0},
		{"instant/countby_level_6h", "instant", `sum by (level) (count_over_time(%s[6h]))`, 6 * time.Hour, 0},
		// high output cardinality: group by a high-cardinality label -> ~thousands of output series.
		{"instant/countby_job_6h", "instant", `sum by (job) (count_over_time(%s[6h]))`, 6 * time.Hour, 0},
		// range: a query_range spanning up to 24h, stepped so the step count stays bounded.
		{"range/count_1h_1m", "range", `sum(count_over_time(%s[5m]))`, time.Hour, time.Minute},
		{"range/count_6h_5m", "range", `sum(count_over_time(%s[5m]))`, 6 * time.Hour, 5 * time.Minute},
		{"range/count_24h_15m", "range", `sum(count_over_time(%s[5m]))`, 24 * time.Hour, 15 * time.Minute},
		{"range/rate_6h_5m", "range", `sum(rate(%s[5m]))`, 6 * time.Hour, 5 * time.Minute},
		{"range/countby_level_6h_5m", "range", `sum by (level) (count_over_time(%s[5m]))`, 6 * time.Hour, 5 * time.Minute},
		// high output cardinality: group by a high-cardinality label -> ~thousands of output series.
		{"range/countby_job_6h_5m", "range", `sum by (job) (count_over_time(%s[5m]))`, 6 * time.Hour, 5 * time.Minute},
	}
	var queries []queryDef
	for _, q := range allQueries {
		if q.kind == *qtype {
			queries = append(queries, q)
		}
	}
	if len(queries) == 0 {
		fmt.Fprintf(os.Stderr, "invalid -type %q: must be \"range\" or \"instant\"\n", *qtype)
		os.Exit(2)
	}

	client := &http.Client{Timeout: *timeout}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startRun := time.Now()
	deadline := startRun.Add(*duration)

	var (
		mu      sync.Mutex
		perType = map[string][]sample{}
		total   int
		issued  atomic.Int64 // shared round-robin/terminate counter
	)

	fmt.Printf("load test: url=%s tenant=%s selector=%q type=%s max-window=%s concurrency=%d count=%d duration=%s end=%d user-agent=%q no-cache=%t\n",
		*endpoint, *tenant, *selector, *qtype, *maxWindow, *conc, *count, *duration, endAnchor.Unix(), *userAgent, *noCache)

	// Progress ticker.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				mu.Lock()
				n := total
				mu.Unlock()
				if time.Now().After(deadline) {
					return
				}
				fmt.Printf("  ... %d queries issued (%.0fs left)\n", n, time.Until(deadline).Seconds())
			}
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				// Round-robin over the query slice via a shared 1-based sequence number, so every query
				// runs an equal number of times in a fixed rotation (deterministic order, equal
				// frequency — not random selection). At -concurrency 1 the whole run is deterministic.
				n := issued.Add(1)
				if *count > 0 && n > int64(*count) {
					return
				}
				if *count == 0 && !time.Now().Before(deadline) {
					return
				}
				q := queries[(n-1)%int64(len(queries))]
				s := runQuery(ctx, client, *endpoint, *tenant, *selector, q, endAnchor, *maxWindow, *userAgent, *noCache, *verbose)
				mu.Lock()
				perType[q.name] = append(perType[q.name], s)
				total++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	report(perType, time.Since(startRun))
}

// runQuery executes one query with its end pinned to endAnchor and returns its outcome.
func runQuery(ctx context.Context, c *http.Client, base, tenant, selector string, q queryDef, endAnchor time.Time, maxWindow time.Duration, userAgent string, noCache, verbose bool) sample {
	expr := fmt.Sprintf(q.expr, selector)
	// Every query ends at endAnchor (now, or -end). The range vector / query_range window reaches
	// back from there, so instant [24h] and range 24h both cover up to 24h ending at endAnchor.
	evalTime := endAnchor

	u, _ := url.Parse(base)
	vals := url.Values{"query": {expr}}
	if q.kind == "range" {
		end := evalTime
		// Cap the range span so its window stays on the v1 engine. Instant queries carry their window
		// in the [W] literal of the expression, so -max-window applies to range queries only.
		inRng := q.inRng
		if maxWindow > 0 && inRng > maxWindow {
			inRng = maxWindow
		}
		start := end.Add(-inRng)
		u.Path = "/loki/api/v1/query_range"
		vals.Set("start", strconv.FormatInt(start.Unix(), 10))
		vals.Set("end", strconv.FormatInt(end.Unix(), 10))
		vals.Set("step", strconv.FormatInt(int64(q.step.Seconds()), 10))
	} else {
		u.Path = "/loki/api/v1/query"
		vals.Set("time", strconv.FormatInt(evalTime.Unix(), 10))
	}
	u.RawQuery = vals.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("X-Scope-OrgID", tenant)
	req.Header.Set("User-Agent", userAgent)
	if noCache {
		// The query-frontend disables the results cache for a request only when Cache-Control is
		// exactly "no-cache" (see codec.go). "no-store" is checked on responses, not requests.
		req.Header.Set("Cache-Control", "no-cache")
	}

	t0 := time.Now()
	resp, err := c.Do(req)
	lat := time.Since(t0)
	var s sample
	if err != nil {
		s = sample{latency: lat, ok: false}
	} else {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		s = sample{latency: lat, status: resp.StatusCode, ok: resp.StatusCode == http.StatusOK}
		if s.ok {
			st := queryStats(body)
			s.bytes, s.streams, s.series = st.bytes, st.streams, st.series
			s.usedV2, s.streamFirst, s.tsFirst = st.usedV2, st.streamFirst, st.tsFirst
		}
	}
	if verbose {
		fmt.Printf("  %-24s eval=%s http=%d lat=%s engine=%s order=%s streams=%d series=%d bytes=%.2fGB\n",
			q.name, evalTime.UTC().Format(time.RFC3339), s.status, s.latency.Round(time.Millisecond),
			engineLabel(boolToInt(s.usedV2), 1), orderLabel(s.streamFirst, s.tsFirst), s.streams, s.series, float64(s.bytes)/1e9)
	}
	return s
}

// statsResult holds the fields extracted from a query response's stats block.
type statsResult struct {
	bytes       int64
	streams     int64
	series      int64
	usedV2      bool
	streamFirst int64
	tsFirst     int64
}

// queryStats extracts, from a query response (zero if absent): bytes processed, the number of streams
// touched (input cardinality = index streams matched), the number of result series (output
// cardinality = length of data.result, same for a vector or a matrix), whether the v2/thor engine
// ran the query, and the v1 stream-first / timestamp-first sub-evaluation counts. The last two only
// appear when the deployed build carries the stream-first stats change.
func queryStats(body []byte) statsResult {
	var r struct {
		Data struct {
			Result []json.RawMessage `json:"result"`
			Stats  struct {
				Summary struct {
					TotalBytesProcessed      int64 `json:"totalBytesProcessed"`
					StreamFirstSubqueries    int64 `json:"streamFirstSubqueries"`
					TimestampFirstSubqueries int64 `json:"timestampFirstSubqueries"`
				} `json:"summary"`
				Index struct {
					TotalStreams int64 `json:"totalStreams"`
				} `json:"index"`
				Querier  engineStat `json:"querier"`
				Ingester engineStat `json:"ingester"`
			} `json:"stats"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &r)
	sm := r.Data.Stats.Summary
	return statsResult{
		bytes:       sm.TotalBytesProcessed,
		streams:     r.Data.Stats.Index.TotalStreams,
		series:      int64(len(r.Data.Result)),
		usedV2:      r.Data.Stats.Querier.Store.QueryUsedV2Engine || r.Data.Stats.Ingester.Store.QueryUsedV2Engine,
		streamFirst: sm.StreamFirstSubqueries,
		tsFirst:     sm.TimestampFirstSubqueries,
	}
}

// engineStat mirrors the store.queryUsedV2Engine marker under stats.querier and stats.ingester.
type engineStat struct {
	Store struct {
		QueryUsedV2Engine bool `json:"queryUsedV2Engine"`
	} `json:"store"`
}

func report(perType map[string][]sample, dur time.Duration) {
	names := make([]string, 0, len(perType))
	for n := range perType {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Printf("\n%-26s %6s %6s %9s %9s %9s %9s %12s %11s %8s %6s %6s\n", "query", "count", "errors", "p50", "p90", "p99", "max", "avg_streams", "avg_series", "avg_GB", "engine", "order")
	var allLat []time.Duration
	var allCount, allErr, allOK, anyV2, hasOrder int
	var allBytes, allStreams, allSeries int64
	for _, n := range names {
		ss := perType[n]
		// Only successful queries feed latency and the averages; a failed query has no stats and a
		// meaningless (often timed-out) latency, so counting it would distort every metric.
		lats := make([]time.Duration, 0, len(ss))
		var errs, okCount, v2Count int
		var bytesSum, streamsSum, seriesSum, streamFirstSum, tsFirstSum int64
		for _, s := range ss {
			if !s.ok {
				errs++
				continue
			}
			okCount++
			lats = append(lats, s.latency)
			allLat = append(allLat, s.latency)
			if s.usedV2 {
				v2Count++
			}
			bytesSum += s.bytes
			streamsSum += s.streams
			seriesSum += s.series
			streamFirstSum += s.streamFirst
			tsFirstSum += s.tsFirst
		}
		allCount += len(ss)
		allErr += errs
		allOK += okCount
		anyV2 += v2Count
		hasOrder += int(streamFirstSum + tsFirstSum)
		allBytes += bytesSum
		allStreams += streamsSum
		allSeries += seriesSum
		avgGB := 0.0
		var avgStreams, avgSeries int64
		if okCount > 0 {
			avgGB = float64(bytesSum) / float64(okCount) / 1e9
			avgStreams = streamsSum / int64(okCount)
			avgSeries = seriesSum / int64(okCount)
		}
		fmt.Printf("%-26s %6d %6d %9s %9s %9s %9s %12d %11d %8.2f %6s %6s\n", n, len(ss), errs,
			pct(lats, 50), pct(lats, 90), pct(lats, 99), pct(lats, 100), avgStreams, avgSeries, avgGB,
			engineLabel(v2Count, okCount), orderLabel(streamFirstSum, tsFirstSum))
	}
	qps := float64(allCount) / dur.Seconds()
	var avgStreamsAll, avgSeriesAll int64
	if allOK > 0 {
		avgStreamsAll = allStreams / int64(allOK)
		avgSeriesAll = allSeries / int64(allOK)
	}
	fmt.Printf("\nTOTAL: %d queries, %d errors, %.1f q/s, p50=%s p99=%s, avg %d streams/query -> %d series/query, ~%.1f GB processed total\n",
		allCount, allErr, qps, pct(allLat, 50), pct(allLat, 99), avgStreamsAll, avgSeriesAll, float64(allBytes)/1e9)

	if anyV2 > 0 {
		fmt.Printf("WARNING: %d queries ran on the v2/thor engine (engine=v2/mix), bypassing v1 stream-first.\n"+
			"         Lower -max-window below the cell's query_engine.storage_lag so windows stay on v1.\n", anyV2)
	}
	if hasOrder == 0 && anyV2 == 0 && allOK > 0 {
		fmt.Printf("WARNING: no stream-first/timestamp-first counters in any response (order=none everywhere).\n" +
			"         The deployed build likely lacks the stream-first stats change.\n")
	}
}

// engineLabel classifies a query's executions by engine: v1 (none used v2), v2 (all did), or mix.
func engineLabel(v2, total int) string {
	switch {
	case total == 0 || v2 == 0:
		return "v1"
	case v2 == total:
		return "v2"
	default:
		return "mix"
	}
}

// orderLabel classifies the resolved v1 sample order from the sub-evaluation counts. "none" means the
// query never hit the v1 decision point (ran on v2, was not a decomposable range aggregation, or the
// build lacks the counters).
func orderLabel(streamFirst, tsFirst int64) string {
	switch {
	case streamFirst > 0 && tsFirst > 0:
		return "mixed"
	case streamFirst > 0:
		return "stream"
	case tsFirst > 0:
		return "ts"
	default:
		return "none"
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// pct returns the pth-percentile latency, formatted (p=100 is the max).
func pct(lats []time.Duration, p int) string {
	if len(lats) == 0 {
		return "-"
	}
	s := append([]time.Duration(nil), lats...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := (p * (len(s) - 1)) / 100
	return s[idx].Round(time.Millisecond).String()
}
