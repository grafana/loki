package querytee

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
)

func TestFanOutHandler_Do(t *testing.T) {
	const (
		routeName     = "test_route"
		tenantID      = "test-tenant"
		primaryBody   = `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"1"},"values":[["1000000000","primary line"]]}]}}`
		secondaryBody = `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"2"},"values":[["1000000000","secondary line"]]}]}}`
		otherBody     = `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"3"},"values":[["1000000000","other line"]]}]}}`
	)

	req := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Limit:   100,
	}

	newHandler := func(comp comparator.ResponsesComparator, backends ...*ProxyBackend) *FanOutHandler {
		return NewFanOutHandler(FanOutHandlerConfig{
			Backends:   backends,
			Codec:      queryrange.DefaultCodec,
			Logger:     log.NewNopLogger(),
			Metrics:    NewProxyMetrics(prometheus.NewRegistry()),
			RouteName:  routeName,
			Comparator: comp,
		})
	}

	comparedCount := func(handler *FanOutHandler, backend *ProxyBackend, result, cause string) float64 {
		return prom_testutil.ToFloat64(handler.metrics.responsesComparedTotal.WithLabelValues(backend.name, backend.Alias(), routeName, result, cause, unknownIssuer, tenantID))
	}

	// bodyWithStats returns a response body whose stats carry the bytes processed, the queue time
	// in seconds and the number of results cache entries found.
	bodyWithStats := func(bytesProcessed, queueTime, cacheEntriesFound int) string {
		return fmt.Sprintf(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"app":"test"},"values":[["1000000000","line"]]}],"stats":{"summary":{"totalBytesProcessed":%d,"queueTime":%d},"cache":{"result":{"entriesFound":%d}}}}}`, bytesProcessed, queueTime, cacheEntriesFound)
	}

	// bytesRatio returns the count and the sum of the observed ratios of the bytes processed.
	bytesRatio := func(t *testing.T, handler *FanOutHandler, queriersLoadStatus, resultCacheStatus string) (uint64, float64) {
		t.Helper()

		ratio, err := handler.metrics.secondaryBackendStatsRatio.GetMetricWithLabelValues(routeName, "bytes_processed", queriersLoadStatus, resultCacheStatus)
		require.NoError(t, err)
		var observed dto.Metric
		require.NoError(t, ratio.(prometheus.Metric).Write(&observed))
		return observed.GetHistogram().GetSampleCount(), observed.GetHistogram().GetSampleSum()
	}

	t.Run("returns the primary response when the secondary backend answers first", func(t *testing.T) {
		handler := newHandler(nil,
			newTestBackend(t, "backend-1", RolePrimary, 50*time.Millisecond, http.StatusOK, primaryBody),
			newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusOK, secondaryBody),
		)

		resp, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		lokiResp, ok := resp.(*queryrange.LokiResponse)
		require.True(t, ok)
		require.Len(t, lokiResp.Data.Result, 1)
		require.Contains(t, lokiResp.Data.Result[0].Labels, `backend="1"`)
		require.Equal(t, "primary line", lokiResp.Data.Result[0].Entries[0].Line)
	})

	t.Run("returns the primary response without waiting for a blocked secondary backend and compares the pair once it answers", func(t *testing.T) {
		release := make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		secondary := newTestBackendWithHandler(t, "backend-2", RoleSecondary, func(w http.ResponseWriter, _ *http.Request) {
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(secondaryBody))
		})
		handler := newHandler(&recordingComparator{},
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, primaryBody),
			secondary,
		)
		ctx, cancel := context.WithCancel(user.InjectOrgID(t.Context(), tenantID))
		defer cancel()

		done := make(chan error, 1)
		go func() {
			_, err := handler.Do(ctx, req)
			done <- err
		}()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("Do did not return while the secondary backend was blocked")
		}

		// The request ends before the secondary answers, as it does when the client gets its response.
		cancel()
		unblock()
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonMatch, "") == 1 }, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("compares the secondary response with the primary response and ignores the other backends", func(t *testing.T) {
		comp := &recordingComparator{}
		secondary := newTestBackend(t, "backend-2", RoleSecondary, 30*time.Millisecond, http.StatusOK, secondaryBody)
		handler := newHandler(comp,
			newTestBackend(t, "backend-3", RoleOther, 0, http.StatusOK, otherBody),
			secondary,
			newTestBackend(t, "backend-1", RolePrimary, 100*time.Millisecond, http.StatusOK, primaryBody),
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonMatch, "") == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Equal(t, []comparedBodies{{expected: primaryBody, actual: secondaryBody}}, comp.recorded())
	})

	t.Run("returns the primary error and skips the comparison when the primary backend answers with an error status", func(t *testing.T) {
		comp := &recordingComparator{}
		secondary := newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusOK, secondaryBody)
		handler := newHandler(comp,
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusInternalServerError, ""),
			secondary,
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.EqualError(t, err, "failed to decode response from backend-1: rpc error: code = Code(500) desc = ")
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonSkipped, "http_5xx") == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Empty(t, comp.recorded())
	})

	t.Run("counts a timeout of the secondary backend with its cause", func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		secondary := newTestBackendWithHandler(t, "backend-2", RoleSecondary, func(http.ResponseWriter, *http.Request) { <-release })
		secondary.timeout = 50 * time.Millisecond
		handler := newHandler(&recordingComparator{},
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, primaryBody),
			secondary,
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonFailed, "timeout") == 1 }, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("counts a match and observes the ratios of the stats when both backends answer", func(t *testing.T) {
		secondary := newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusOK, bodyWithStats(250, 0, 0))
		handler := newHandler(staticComparator{summary: &comparator.ComparisonSummary{}},
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, bodyWithStats(1000, 0, 0)),
			secondary,
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonMatch, "") == 1 }, 5*time.Second, 10*time.Millisecond)
		count, sum := bytesRatio(t, handler, "both_idle", "both_miss")
		require.Equal(t, uint64(1), count)
		require.InDelta(t, 0.25, sum, 1e-9)
	})

	for name, tc := range map[string]struct {
		primaryBody, secondaryBody  string
		expectedLoad, expectedCache string
	}{
		"labels the ratios primary_busy and primary_hit when only the primary queued the query and found a cached entry": {
			primaryBody: bodyWithStats(1000, 1, 1), secondaryBody: bodyWithStats(500, 0, 0),
			expectedLoad: "primary_busy", expectedCache: "primary_hit",
		},
		"labels the ratios secondary_busy and secondary_hit when only the secondary queued the query and found a cached entry": {
			primaryBody: bodyWithStats(1000, 0, 0), secondaryBody: bodyWithStats(500, 1, 1),
			expectedLoad: "secondary_busy", expectedCache: "secondary_hit",
		},
		"labels the ratios both_busy and both_hit when both backends queued the query and found a cached entry": {
			primaryBody: bodyWithStats(1000, 1, 1), secondaryBody: bodyWithStats(500, 1, 1),
			expectedLoad: "both_busy", expectedCache: "both_hit",
		},
	} {
		t.Run(name, func(t *testing.T) {
			secondary := newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusOK, tc.secondaryBody)
			handler := newHandler(staticComparator{summary: &comparator.ComparisonSummary{}},
				newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, tc.primaryBody),
				secondary,
			)

			_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

			require.NoError(t, err)
			require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonMatch, "") == 1 }, 5*time.Second, 10*time.Millisecond)
			count, sum := bytesRatio(t, handler, tc.expectedLoad, tc.expectedCache)
			require.Equal(t, uint64(1), count)
			require.InDelta(t, 0.5, sum, 1e-9)
		})
	}

	t.Run("counts a mismatch with the cause of the comparator when the bodies differ", func(t *testing.T) {
		secondary := newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusOK, secondaryBody)
		handler := newHandler(staticComparator{
			summary: &comparator.ComparisonSummary{MismatchCause: comparator.CauseSampleValueMismatch},
			err:     comparator.ErrComparisonMismatch,
		},
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, primaryBody),
			secondary,
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return comparedCount(handler, secondary, comparisonMismatch, comparator.CauseSampleValueMismatch) == 1
		}, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("counts a failed pair with the compare_error cause when the comparator fails", func(t *testing.T) {
		secondary := newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusOK, secondaryBody)
		handler := newHandler(staticComparator{err: errors.New("cannot compare")},
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, primaryBody),
			secondary,
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonFailed, causeCompareError) == 1 }, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("counts a failed pair with the cause of the failure when the secondary backend answers with an error status", func(t *testing.T) {
		secondary := newTestBackend(t, "backend-2", RoleSecondary, 0, http.StatusBadRequest, "")
		handler := newHandler(&recordingComparator{},
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, primaryBody),
			secondary,
		)

		_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)

		require.NoError(t, err)
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonFailed, "http_4xx") == 1 }, 5*time.Second, 10*time.Millisecond)
	})
}

func TestResponseStats(t *testing.T) {
	const query = `sum(count_over_time({app="a"}[5m]))`
	req := &queryrange.LokiRequest{Query: query, Plan: &plan.QueryPlan{AST: syntax.MustParseExpr(query)}}

	decode := func(t *testing.T, body string) *queryrange.LokiPromResponse {
		t.Helper()

		resp, err := queryrange.DefaultCodec.DecodeResponse(t.Context(), &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, req)
		require.NoError(t, err)

		promResp, ok := resp.(*queryrange.LokiPromResponse)
		require.True(t, ok, "expected LokiPromResponse type")
		return promResp
	}

	t.Run("reads the stats that the codec decodes from a frontend response", func(t *testing.T) {
		resp := decode(t, `{"status":"success","data":{"resultType":"matrix","result":[],"stats":{
			"summary":{"totalBytesProcessed":1234,"queueTime":2},
			"querier":{"store":{"dataobj":{"prePredicateDecompressedRows":3}}},
			"cache":{"result":{"entriesFound":1}}
		}}}`)

		result, ok := responseStats(resp)
		require.True(t, ok)
		require.Equal(t, int64(1234), result.Summary.TotalBytesProcessed)
		require.Equal(t, float64(2), result.Summary.QueueTime)
		require.Equal(t, int32(1), result.Caches.Result.EntriesFound)
	})

	t.Run("reads no data object work from a response without data object stats", func(t *testing.T) {
		result, ok := responseStats(decode(t, `{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		require.True(t, ok)
		require.Zero(t, result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
	})

	t.Run("reports no stats for a response of an unknown type", func(t *testing.T) {
		_, ok := responseStats(&NonDecodableResponse{})
		require.False(t, ok)
	})
}

func TestTenantLabel(t *testing.T) {
	t.Run("returns the tenant ID of a single-tenant request", func(t *testing.T) {
		ctx := user.InjectOrgID(t.Context(), "tenant1")

		require.Equal(t, "tenant1", tenantLabel(ctx))
	})

	t.Run("returns the sorted tenant IDs without duplicates, joined by a pipe, for a multi-tenant request", func(t *testing.T) {
		ctx := user.InjectOrgID(t.Context(), "tenant2|tenant1|tenant2")

		require.Equal(t, "tenant1|tenant2", tenantLabel(ctx))
	})

	t.Run("returns the unknown tenant when the context has no tenant ID", func(t *testing.T) {
		require.Equal(t, unknownTenant, tenantLabel(t.Context()))
	})
}

func newTestBackend(t *testing.T, name string, role BackendRole, delay time.Duration, status int, body string) *ProxyBackend {
	t.Helper()

	return newTestBackendWithHandler(t, name, role, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func newTestBackendWithHandler(t *testing.T, name string, role BackendRole, handler http.HandlerFunc) *ProxyBackend {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	backend, err := NewProxyBackend(name, serverURL, 5*time.Second, role)
	require.NoError(t, err)
	return backend
}

type recordingComparator struct {
	mu    sync.Mutex
	calls []comparedBodies
}

type comparedBodies struct {
	expected string
	actual   string
}

func (c *recordingComparator) Compare(expected, actual []byte, _ time.Time) (*comparator.ComparisonSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls = append(c.calls, comparedBodies{expected: string(expected), actual: string(actual)})
	return &comparator.ComparisonSummary{}, nil
}

func (c *recordingComparator) recorded() []comparedBodies {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.calls)
}

// staticComparator returns a fixed comparison outcome.
type staticComparator struct {
	summary *comparator.ComparisonSummary
	err     error
}

func (c staticComparator) Compare(_, _ []byte, _ time.Time) (*comparator.ComparisonSummary, error) {
	return c.summary, c.err
}
