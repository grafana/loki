// Package lokiclient issues LogQL instant and range queries against a Loki
// query-frontend and extracts the statistics the benchmark records.
package lokiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/queries"
)

// userAgent is sent on every request so query-stats logs can be filtered to the
// benchmark's traffic.
const userAgent = "querybench"

// Client issues queries to one Loki query-frontend for one tenant.
//
// It is safe to reuse across queries but the benchmark drives it sequentially,
// one request at a time, so latency reflects a single query in isolation.
type Client struct {
	http    *http.Client
	baseURL string
	tenant  string
	logf    func(format string, args ...any)
}

// Options configure a Client.
type Options struct {
	// Timeout bounds a single query request. Zero means no timeout.
	Timeout time.Duration
	// Logf receives a warning when a 200 response's stats cannot be parsed, so a
	// response-schema change surfaces instead of silently zeroing the
	// processed-bytes figure. It may be nil.
	Logf func(format string, args ...any)
}

// New returns a Client for baseURL and tenant.
func New(baseURL, tenant string, opts Options) *Client {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Client{
		http:    &http.Client{Timeout: opts.Timeout},
		baseURL: baseURL,
		tenant:  tenant,
		logf:    logf,
	}
}

// Result is the outcome of one query execution.
type Result struct {
	Latency        time.Duration
	StatusCode     int
	ProcessedBytes int64
}

// Run executes q once over [start, end] and returns its latency and statistics.
//
// An instant query is sent to /loki/api/v1/query at time end; a range query is
// sent to /loki/api/v1/query_range over [start, end] with q.Step. A non-200
// response is returned as an error, with the measured latency still set on the
// returned Result so callers can log it.
func (c *Client) Run(ctx context.Context, q queries.Query, start, end time.Time) (Result, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return Result{}, fmt.Errorf("parse base url %q: %w", c.baseURL, err)
	}
	vals := url.Values{"query": {q.Expr}}
	switch q.Type {
	case queries.TypeRange:
		u.Path = "/loki/api/v1/query_range"
		vals.Set("start", strconv.FormatInt(start.Unix(), 10))
		vals.Set("end", strconv.FormatInt(end.Unix(), 10))
		vals.Set("step", strconv.FormatInt(int64(q.Step.Seconds()), 10))
	case queries.TypeInstant:
		u.Path = "/loki/api/v1/query"
		vals.Set("time", strconv.FormatInt(end.Unix(), 10))
	default:
		return Result{}, fmt.Errorf("unknown query type %q", q.Type)
	}
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Scope-OrgID", c.tenant)
	req.Header.Set("User-Agent", userAgent)
	// Always bypass the results cache so latency and backend cost reflect the
	// query doing real work, not a cache hit. The query-frontend skips its results
	// cache only when Cache-Control is exactly "no-cache"; "no-store" is a response
	// directive, not a request one.
	req.Header.Set("Cache-Control", "no-cache")

	t0 := time.Now()
	resp, err := c.http.Do(req)
	latency := time.Since(t0)
	if err != nil {
		return Result{Latency: latency}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{Latency: latency, StatusCode: resp.StatusCode}, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{Latency: latency, StatusCode: resp.StatusCode},
			fmt.Errorf("status %d: %s", resp.StatusCode, truncate(body, 200))
	}
	bytesProcessed, err := processedBytes(body)
	if err != nil {
		// The query ran (HTTP 200) but its stats did not parse. Log it rather than
		// silently reporting zero for the benchmark's headline metric, which would
		// hide a response-schema change behind a plausible-looking zero.
		c.logf("query %s: parse response stats: %v", q.Name, err)
	}
	return Result{
		Latency:        latency,
		StatusCode:     resp.StatusCode,
		ProcessedBytes: bytesProcessed,
	}, nil
}

// processedBytes returns totalBytesProcessed from a query response's stats
// summary. It returns a parse error (so the caller can surface a malformed or
// changed response) but treats a merely absent field as zero.
func processedBytes(body []byte) (int64, error) {
	var r struct {
		Data struct {
			Stats struct {
				Summary struct {
					TotalBytesProcessed int64 `json:"totalBytesProcessed"`
				} `json:"summary"`
			} `json:"stats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, err
	}
	return r.Data.Stats.Summary.TotalBytesProcessed, nil
}

// truncate returns b as a string, capped at n bytes, for error messages.
func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
