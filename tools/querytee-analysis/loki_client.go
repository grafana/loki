package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

const (
	maxRetries     = 3
	baseRetryDelay = 1 * time.Second
	maxRetryDelay  = 10 * time.Second
)

// LokiClient issues queries against a Loki instance and returns parsed responses.
type LokiClient struct {
	endpoint string
	client   *http.Client
}

func NewLokiClient(endpoint string) *LokiClient {
	return &LokiClient{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 2 * time.Minute},
	}
}

// QueryRangeRaw executes a query_range request and returns the raw HTTP
// response body. This is useful when the caller needs the raw bytes for
// comparison (e.g. with SamplesComparator).
func (c *LokiClient) QueryRangeRaw(ctx context.Context, tenant, query string, start, end time.Time, limit int, step time.Duration) ([]byte, error) {
	if limit <= 0 {
		limit = 5000
	}

	params := url.Values{
		"query":     {query},
		"start":     {fmt.Sprintf("%d", start.UnixNano())},
		"end":       {fmt.Sprintf("%d", end.UnixNano())},
		"direction": {"backward"},
		"limit":     {fmt.Sprintf("%d", limit)},
	}
	if step > 0 {
		params.Set("step", fmt.Sprintf("%d", int64(step.Seconds())))
	}

	reqURL := c.endpoint + "/loki/api/v1/query_range?" + params.Encode()
	level.Debug(logger).Log("msg", "issuing range query", "endpoint", c.endpoint, "query", query,
		"start", start.Format(time.RFC3339), "end", end.Format(time.RFC3339))

	return c.doWithRetry(ctx, reqURL, tenant)
}

// QueryRange executes a query_range request and returns the parsed data payload.
// step of 0 means the parameter is omitted and Loki uses its default.
func (c *LokiClient) QueryRange(ctx context.Context, tenant, query string, start, end time.Time, limit int, step time.Duration) (*loghttp.QueryResponseData, error) {
	body, err := c.QueryRangeRaw(ctx, tenant, query, start, end, limit, step)
	if err != nil {
		return nil, err
	}

	var qr loghttp.QueryResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("unmarshalling response: %w", err)
	}
	return &qr.Data, nil
}

// InstantQuery executes an instant query (/loki/api/v1/query) at the given time.
func (c *LokiClient) InstantQuery(ctx context.Context, tenant, query string, ts time.Time) (*loghttp.QueryResponseData, error) {
	params := url.Values{
		"query": {query},
		"time":  {fmt.Sprintf("%d", ts.UnixNano())},
	}

	reqURL := c.endpoint + "/loki/api/v1/query?" + params.Encode()
	level.Debug(logger).Log("msg", "issuing instant query", "endpoint", c.endpoint, "query", query,
		"time", ts.Format(time.RFC3339))

	body, err := c.doWithRetry(ctx, reqURL, tenant)
	if err != nil {
		return nil, err
	}

	var qr loghttp.QueryResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("unmarshalling response: %w", err)
	}
	return &qr.Data, nil
}

// doWithRetry executes an HTTP GET with exponential backoff retries for
// transient failures (5xx, 429, network errors).
func (c *LokiClient) doWithRetry(ctx context.Context, reqURL, tenant string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt)
			level.Warn(logger).Log("msg", "retrying request", "attempt", attempt, "delay", delay, "url", truncate(reqURL, 120), "err", lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("X-Scope-OrgID", tenant)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("querying %s: %w", c.endpoint, err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading response: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		lastErr = fmt.Errorf("status %d from %s: %s", resp.StatusCode, c.endpoint, truncate(string(body), 200))

		if !isRetryableStatus(resp.StatusCode) {
			return nil, lastErr
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					delay := time.Duration(secs) * time.Second
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(delay):
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// retryDelay returns an exponential backoff duration with jitter for the given
// attempt number (1-indexed).
func retryDelay(attempt int) time.Duration {
	delay := baseRetryDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	jitter := time.Duration(rand.Int64N(int64(delay) / 2))
	return delay + jitter
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
