package querytee

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
)

func TestFanOutHandler_Do_ReturnsPreferredResponse(t *testing.T) {
	// Create test backends
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond) // Slight delay
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"1"},"values":[["1000000000","log line 1"]]}]}}`))
		require.NoError(t, err)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"2"},"values":[["1000000000","log line 2"]]}]}}`))
		require.NoError(t, err)
	}))
	defer backend2.Close()

	backend1URL, _ := url.Parse(backend1.URL)
	backend2URL, _ := url.Parse(backend2.URL)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backend1URL, 5*time.Second, true),  // preferred
		NewProxyBackend("backend-2", backend2URL, 5*time.Second, false), // non-preferred
	}

	handler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:  backends,
		Codec:     queryrange.DefaultCodec,
		Logger:    log.NewNopLogger(),
		Metrics:   NewProxyMetrics(prometheus.NewRegistry()),
		RouteName: "test_route",
	})

	// Create a test request
	req := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Limit:   100,
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// The response should be from the preferred backend (backend-1)
	lokiResp, ok := resp.(*queryrange.LokiResponse)
	require.True(t, ok, "expected LokiResponse type")
	require.Equal(t, "success", lokiResp.Status)

	// Verify the response came from backend-1 by checking the stream labels
	require.Len(t, lokiResp.Data.Result, 1, "expected 1 stream in result")
	stream := lokiResp.Data.Result[0]
	require.Contains(t, stream.Labels, `backend="1"`, "expected response from backend-1")

	// Also verify the log line content
	require.Len(t, stream.Entries, 1, "expected 1 log entry")
	require.Equal(t, "log line 1", stream.Entries[0].Line, "expected log line from backend-1")
}

func TestFanOutHandler_Do_AllBackendsFail(t *testing.T) {
	// Create test backends - all fail
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend2.Close()

	backend1URL, _ := url.Parse(backend1.URL)
	backend2URL, _ := url.Parse(backend2.URL)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backend1URL, 5*time.Second, true),
		NewProxyBackend("backend-2", backend2URL, 5*time.Second, false),
	}

	handler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:   backends,
		Codec:      queryrange.DefaultCodec,
		Logger:     log.NewNopLogger(),
		Metrics:    NewProxyMetrics(prometheus.NewRegistry()),
		RouteName:  "test_route",
		EnableRace: false,
	})

	req := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Limit:   100,
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	resp, err := handler.Do(ctx, req)

	// Should return error when all backends fail
	require.Error(t, err)

	nonDecodableResp, ok := resp.(*NonDecodableResponse)
	require.True(t, ok)
	require.Equal(t, nonDecodableResp.StatusCode, 500)
}

func TestFanOutHandler_Do_WithFilter(t *testing.T) {
	requestCount := 0
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"1"},"values":[["1000000000","filtered response from backend-1"]]}]}}`))
		require.NoError(t, err)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"2"},"values":[["1000000000","filtered response from backend-2"]]}]}}`))
		require.NoError(t, err)
	}))
	defer backend2.Close()

	backend1URL, _ := url.Parse(backend1.URL)
	backend2URL, _ := url.Parse(backend2.URL)

	// Backend 2 has a filter that won't match
	backend2Proxy := NewProxyBackend("backend-2", backend2URL, 5*time.Second, false)
	backend2Proxy.filter = regexp.MustCompile("^nomatch$")

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backend1URL, 5*time.Second, true),
		backend2Proxy,
	}

	handler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:   backends,
		Codec:      queryrange.DefaultCodec,
		Logger:     log.NewNopLogger(),
		Metrics:    NewProxyMetrics(prometheus.NewRegistry()),
		RouteName:  "test_route",
		EnableRace: false,
	})

	req := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Limit:   100,
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response came from backend-1
	lokiResp, ok := resp.(*queryrange.LokiResponse)
	require.True(t, ok, "expected LokiResponse type")
	require.Equal(t, "success", lokiResp.Status)

	require.Len(t, lokiResp.Data.Result, 1, "expected 1 stream in result")
	stream := lokiResp.Data.Result[0]
	require.Contains(t, stream.Labels, `backend="1"`, "expected response from backend-1")
	require.Len(t, stream.Entries, 1, "expected 1 log entry")
	require.Equal(t, "filtered response from backend-1", stream.Entries[0].Line, "expected log line from backend-1")

	// Give time for the async goroutines to complete
	time.Sleep(50 * time.Millisecond)

	// Only backend-1 should have received a request (backend-2 filtered out)
	require.Equal(t, 1, requestCount, "expected only 1 backend to receive request due to filter")
}

func TestFanOutHandler_Do_RaceModeReturnsNonPreferredIfWithinTolerance(t *testing.T) {
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"1"},"values":[["1000000000","log line 1"]]}]}}`))
		require.NoError(t, err)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"backend":"2"},"values":[["1000000000","log line 2"]]}]}}`))
		require.NoError(t, err)
	}))
	defer backend2.Close()

	backend1URL, _ := url.Parse(backend1.URL)
	backend2URL, _ := url.Parse(backend2.URL)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backend1URL, 5*time.Second, true),
		NewProxyBackend("backend-2", backend2URL, 5*time.Second, false),
	}

	handler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:      backends,
		Codec:         queryrange.DefaultCodec,
		Logger:        log.NewNopLogger(),
		Metrics:       NewProxyMetrics(prometheus.NewRegistry()),
		RouteName:     "test_route",
		EnableRace:    true,
		RaceTolerance: 100 * time.Millisecond,
	})

	req := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Limit:   100,
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	lokiResp, ok := resp.(*queryrange.LokiResponse)
	require.True(t, ok)
	require.Equal(t, "success", lokiResp.Status)
	require.Len(t, lokiResp.Data.Result, 1)
	require.Contains(t, lokiResp.Data.Result[0].Labels, `backend="2"`)

	time.Sleep(100 * time.Millisecond)
}

func TestFanOutHandler_Do_RaceModeAllBackendsFail(t *testing.T) {
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend2.Close()

	backend1URL, _ := url.Parse(backend1.URL)
	backend2URL, _ := url.Parse(backend2.URL)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backend1URL, 5*time.Second, true),
		NewProxyBackend("backend-2", backend2URL, 5*time.Second, false),
	}

	handler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:   backends,
		Codec:      queryrange.DefaultCodec,
		Logger:     log.NewNopLogger(),
		Metrics:    NewProxyMetrics(prometheus.NewRegistry()),
		RouteName:  "test_route",
		EnableRace: true,
	})

	req := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Limit:   100,
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	resp, err := handler.Do(ctx, req)

	require.Error(t, err)
	nonDecodableResp, ok := resp.(*NonDecodableResponse)
	require.True(t, ok)
	require.Equal(t, 500, nonDecodableResp.StatusCode)
}
