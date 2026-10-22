package querytee

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

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

	comparedCount := func(handler *FanOutHandler, backend *ProxyBackend, result string) float64 {
		return prom_testutil.ToFloat64(handler.metrics.responsesComparedTotal.WithLabelValues(backend.name, backend.Alias(), routeName, result, unknownIssuer, tenantID))
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

	t.Run("returns the primary response without waiting for a blocked secondary backend", func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		handler := newHandler(nil,
			newTestBackend(t, "backend-1", RolePrimary, 0, http.StatusOK, primaryBody),
			newTestBackendWithHandler(t, "backend-2", RoleSecondary, func(http.ResponseWriter, *http.Request) { <-release }),
		)

		done := make(chan error, 1)
		go func() {
			_, err := handler.Do(user.InjectOrgID(t.Context(), tenantID), req)
			done <- err
		}()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("Do did not return while the secondary backend was blocked")
		}
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
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonMatch) == 1 }, 5*time.Second, 10*time.Millisecond)
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
		require.Eventually(t, func() bool { return comparedCount(handler, secondary, comparisonSkipped) == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Empty(t, comp.recorded())
	})
}

func TestTenantLabel(t *testing.T) {
	newRequest := func(t *testing.T, orgID string) *http.Request {
		t.Helper()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if orgID != "" {
			req.Header.Set(user.OrgIDHeaderName, orgID)
		}
		return req
	}

	t.Run("returns the tenant ID of a single-tenant request", func(t *testing.T) {
		label, err := tenantLabel(newRequest(t, "tenant1"))

		require.NoError(t, err)
		require.Equal(t, "tenant1", label)
	})

	t.Run("returns the sorted tenant IDs without duplicates, joined by a pipe, for a multi-tenant request", func(t *testing.T) {
		label, err := tenantLabel(newRequest(t, "tenant2|tenant1|tenant2"))

		require.NoError(t, err)
		require.Equal(t, "tenant1|tenant2", label)
	})

	t.Run("returns an error when the request has no tenant ID", func(t *testing.T) {
		_, err := tenantLabel(newRequest(t, ""))

		require.ErrorIs(t, err, user.ErrNoOrgID)
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
