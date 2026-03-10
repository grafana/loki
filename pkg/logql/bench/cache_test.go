package bench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

// countingExecutor wraps *engine.Engine and counts how many times Execute is called.
type countingExecutor struct {
	inner *engine.Engine
	n     atomic.Int64
}

func (e *countingExecutor) Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error) {
	e.n.Add(1)
	return e.inner.Execute(ctx, params)
}

// benchCacheLimits implements engine.Limits for the cache integration test.
// It embeds querier_limits.Limits and engine.RetentionLimits as nil interfaces
// (same pattern as mockLimits in handler_test.go) and explicitly implements
// only the methods that are actually invoked.
type benchCacheLimits struct {
	querier_limits.Limits
	engine.RetentionLimits
}

// Methods called by queryHandler validation:
func (*benchCacheLimits) MaxEntriesLimitPerQuery(_ context.Context, _ string) int    { return 0 }
func (*benchCacheLimits) MaxQueryLookback(_ context.Context, _ string) time.Duration { return 0 }
func (*benchCacheLimits) MaxQueryLength(_ context.Context, _ string) time.Duration   { return 0 }
func (*benchCacheLimits) MaxQueryRange(_ context.Context, _ string) time.Duration    { return 0 }
func (*benchCacheLimits) RequiredLabels(_ context.Context, _ string) []string        { return nil }
func (*benchCacheLimits) RequiredNumberLabels(_ context.Context, _ string) int       { return 0 }
func (*benchCacheLimits) QueryTimeout(_ context.Context, _ string) time.Duration     { return time.Hour }

// Methods called by cache middleware:
func (*benchCacheLimits) MaxCacheFreshness(_ context.Context, _ string) time.Duration { return 0 }
func (*benchCacheLimits) MaxQueryParallelism(_ context.Context, _ string) int         { return 4 }
func (*benchCacheLimits) EngineResultsCacheTimeBucketInterval(_ string) time.Duration {
	return 24 * time.Hour
}

// encodeTestCaseAsHTTPRequest converts a TestCase into an *http.Request using
// DefaultCodec, so the handler's codec stack can decode it correctly.
func encodeTestCaseAsHTTPRequest(t *testing.T, tc TestCase) *http.Request {
	t.Helper()
	ctx := user.InjectOrgID(context.Background(), testTenant)
	lokiReq := &queryrange.LokiRequest{
		Query:     tc.Query,
		StartTs:   tc.Start,
		EndTs:     tc.End,
		Step:      tc.Step.Milliseconds(),
		Direction: tc.Direction,
		Limit:     1000,
	}
	req, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
	require.NoError(t, err)
	return req
}

// TestEngineCachingCorrectness verifies that:
// 1. The cache is populated on the first pass (engine is invoked).
// 2. The cache is hit on the second pass (engine is NOT invoked again).
// 3. Cached responses are byte-for-byte identical to original responses.
//
// Run with: go test ./pkg/logql/bench/ -run=TestEngineCachingCorrectness -v -slow-tests
func TestEngineCachingCorrectness(t *testing.T) {
	if !*slowTests {
		t.Skip("skipping cache correctness test; use -slow-tests to run")
	}

	store, err := NewDataObjV2EngineStore(DefaultDataDir, testTenant)
	require.NoError(t, err)

	counting := &countingExecutor{inner: (*engine.Engine)(store.engine.(*engineAdapter))}

	cfg := engine.Config{
		ResultsCache: queryrangebase.ResultsCacheConfig{
			Config: resultscache.Config{
				CacheConfig: cache.Config{
					EmbeddedCache: cache.EmbeddedCacheConfig{
						Enabled:      true,
						MaxSizeItems: 10_000,
						TTL:          time.Hour,
					},
				},
			},
		},
	}
	limits := &benchCacheLimits{}
	reg := prometheus.NewRegistry()
	handler, err := engine.HandlerFromExecutor(cfg, log.NewNopLogger(), counting, limits, reg)
	require.NoError(t, err)

	cases := loadTestCases(t, &defaultGeneratorConfig)

	// Filter to metric queries only: the log cache only caches empty results,
	// and all bench test queries return non-empty data.
	var metricCases []TestCase
	for _, tc := range cases {
		if tc.Kind() == "metric" {
			metricCases = append(metricCases, tc)
		}
	}
	require.NotEmpty(t, metricCases, "expected at least one metric test case")

	run := func(tc TestCase) *http.Response {
		req := encodeTestCaseAsHTTPRequest(t, tc)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Result()
	}

	// Pass 1: populate the cache; skip queries unsupported by the engine (501).
	type result struct {
		tc   TestCase
		resp *http.Response
	}
	var supported []result
	for _, tc := range metricCases {
		resp := run(tc)
		if resp.StatusCode == http.StatusNotImplemented {
			t.Logf("skipping unsupported query: %s", tc.Query)
			continue
		}
		require.Equal(t, http.StatusOK, resp.StatusCode, "query %q returned unexpected status", tc.Query)
		supported = append(supported, result{tc, resp})
	}
	require.NotEmpty(t, supported, "all metric queries were unsupported")
	t.Logf("supported metric queries: %d out of: %d", len(supported), len(metricCases))

	countAfterPass1 := counting.n.Load()
	require.Greater(t, countAfterPass1, int64(0), "engine was never called on first pass")

	// Pass 2: all results should be served from cache.
	var results2 []*http.Response
	for _, r := range supported {
		resp := run(r.tc)
		require.Equal(t, http.StatusOK, resp.StatusCode, "query %q returned unexpected status on second pass", r.tc.Query)
		results2 = append(results2, resp)
	}
	require.Equal(t, countAfterPass1, counting.n.Load(),
		"engine should not be called on second pass (cache should be hit)")

	// Verify correctness: the data.result payload is identical.
	// We intentionally skip statistics (execution time, bytes/sec, etc.) which
	// legitimately differ between original and cached responses.
	type lokiResponse struct {
		Data struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	extractResult := func(body []byte) (string, string) {
		var resp lokiResponse
		require.NoError(t, json.Unmarshal(body, &resp))
		return resp.Data.ResultType, string(resp.Data.Result)
	}

	for i, r := range supported {
		b1, err := io.ReadAll(r.resp.Body)
		require.NoError(t, err)
		b2, err := io.ReadAll(results2[i].Body)
		require.NoError(t, err)
		rt1, result1 := extractResult(b1)
		rt2, result2 := extractResult(b2)
		require.Equal(t, rt1, rt2, "resultType differs for query %q", r.tc.Query)
		require.JSONEq(t, result1, result2, "cached result differs for query %q", r.tc.Query)
	}

	// --- Log cache: only caches empty results ---
	// Identify queries that are guaranteed to return empty results.
	var logEmptyCases []TestCase
	for _, tc := range cases {
		if tc.Kind() == "log" && slices.Contains(tc.Tags, "empty-result") {
			logEmptyCases = append(logEmptyCases, tc)
		}
	}
	require.NotEmpty(t, logEmptyCases, "expected at least one log query with impossible filter")

	countBeforeLogTest := counting.n.Load()

	// Pass 1: engine is called; empty results are stored in the log cache.
	for _, tc := range logEmptyCases {
		resp := run(tc)
		require.Equal(t, http.StatusOK, resp.StatusCode, "log query %q failed", tc.Query)
	}
	countAfterLogPass1 := counting.n.Load()
	require.Greater(t, countAfterLogPass1, countBeforeLogTest,
		"engine should have been called for empty log queries on first pass")

	// Pass 2: log cache returns the cached empty result; engine is not called.
	for _, tc := range logEmptyCases {
		resp := run(tc)
		require.Equal(t, http.StatusOK, resp.StatusCode, "log query %q failed on second pass", tc.Query)
	}
	require.Equal(t, countAfterLogPass1, counting.n.Load(),
		"engine should not be called for empty log queries on second pass (log cache should be hit)")
}
