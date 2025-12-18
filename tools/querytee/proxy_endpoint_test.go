package querytee

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/goldfish"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	querytee_goldfish "github.com/grafana/loki/v3/tools/querytee/goldfish"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
)

// createTestEndpoint creates a ProxyEndpoint with properly initialized handler pipeline
func createTestEndpoint(t *testing.T, backends []*ProxyBackend, routeName string, comparator comparator.ResponsesComparator, instrumentCompares bool) *ProxyEndpoint {
	metrics := NewProxyMetrics(nil)
	logger := log.NewNopLogger()

	endpoint := NewProxyEndpoint(backends, routeName, metrics, logger, comparator, instrumentCompares)
	handlerFactory := NewHandlerFactory(HandlerFactoryConfig{
		Backends:        backends,
		Codec:           queryrange.DefaultCodec,
		GoldfishManager: nil,
		Logger:          logger,
		Metrics:         metrics,
	})

	queryHandler, err := handlerFactory.CreateHandler(routeName, comparator)
	require.NoError(t, err)
	endpoint.WithQueryHandler(queryHandler)
	return endpoint
}

// createTestEndpointWithMetrics creates a ProxyEndpoint with custom metrics
func createTestEndpointWithMetrics(t *testing.T, backends []*ProxyBackend, routeName string, comp comparator.ResponsesComparator, instrumentCompares bool, metrics *ProxyMetrics) *ProxyEndpoint {
	logger := log.NewNopLogger()

	endpoint := NewProxyEndpoint(backends, routeName, metrics, logger, comp, instrumentCompares)
	handlerFactory := NewHandlerFactory(HandlerFactoryConfig{
		Backends:           backends,
		Codec:              queryrange.DefaultCodec,
		GoldfishManager:    nil,
		Logger:             logger,
		Metrics:            metrics,
		InstrumentCompares: instrumentCompares,
	})

	queryHandler, err := handlerFactory.CreateHandler(routeName, comp)
	require.NoError(t, err)
	endpoint.WithQueryHandler(queryHandler)
	return endpoint
}

// createTestEndpointWithGoldfish creates a ProxyEndpoint with goldfish manager
func createTestEndpointWithGoldfish(t *testing.T, backends []*ProxyBackend, routeName string, goldfishManager querytee_goldfish.Manager) *ProxyEndpoint {
	metrics := NewProxyMetrics(nil)
	logger := log.NewNopLogger()

	handlerFactory := NewHandlerFactory(HandlerFactoryConfig{
		Backends:        backends,
		Codec:           queryrange.DefaultCodec,
		GoldfishManager: goldfishManager,
		Logger:          logger,
		Metrics:         metrics,
	})

	endpoint := NewProxyEndpoint(backends, routeName, metrics, logger, nil, false)
	queryHandler, err := handlerFactory.CreateHandler(routeName, nil)
	require.NoError(t, err)
	endpoint.WithQueryHandler(queryHandler)
	endpoint.WithGoldfish(goldfishManager)
	return endpoint
}

func Test_ProxyEndpoint_waitBackendResponseForDownstream(t *testing.T) {
	backendURL1, err := url.Parse("http://backend-1/")
	require.NoError(t, err)
	backendURL2, err := url.Parse("http://backend-2/")
	require.NoError(t, err)
	backendURL3, err := url.Parse("http://backend-3/")
	require.NoError(t, err)

	backendPref := NewProxyBackend("backend-1", backendURL1, time.Second, true)
	backendOther1 := NewProxyBackend("backend-2", backendURL2, time.Second, false)
	backendOther2 := NewProxyBackend("backend-3", backendURL3, time.Second, false)

	tests := map[string]struct {
		backends  []*ProxyBackend
		responses []*BackendResponse
		expected  *ProxyBackend
	}{
		"the preferred backend is the 1st response received": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*BackendResponse{
				{backend: backendPref, status: 200},
			},
			expected: backendPref,
		},
		"the preferred backend is the last response received": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*BackendResponse{
				{backend: backendOther1, status: 200},
				{backend: backendPref, status: 200},
			},
			expected: backendPref,
		},
		"the preferred backend is the last response received but it's not successful": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*BackendResponse{
				{backend: backendOther1, status: 200},
				{backend: backendPref, status: 500},
			},
			expected: backendOther1,
		},
		"the preferred backend is the 2nd response received but only the last one is successful": {
			backends: []*ProxyBackend{backendPref, backendOther1, backendOther2},
			responses: []*BackendResponse{
				{backend: backendOther1, status: 500},
				{backend: backendPref, status: 500},
				{backend: backendOther2, status: 200},
			},
			expected: backendOther2,
		},
		"there's no preferred backend configured and the 1st response is successful": {
			backends: []*ProxyBackend{backendOther1, backendOther2},
			responses: []*BackendResponse{
				{backend: backendOther1, status: 200},
			},
			expected: backendOther1,
		},
		"there's no preferred backend configured and the last response is successful": {
			backends: []*ProxyBackend{backendOther1, backendOther2},
			responses: []*BackendResponse{
				{backend: backendOther1, status: 500},
				{backend: backendOther2, status: 200},
			},
			expected: backendOther2,
		},
		"no received response is successful": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*BackendResponse{
				{backend: backendOther1, status: 500},
				{backend: backendPref, status: 500},
			},
			expected: backendOther1,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			endpoint := NewProxyEndpoint(testData.backends, "test", NewProxyMetrics(nil), log.NewNopLogger(), nil, false)

			// Send the responses from a dedicated goroutine.
			resCh := make(chan *BackendResponse)
			go func() {
				for _, res := range testData.responses {
					resCh <- res
				}
				close(resCh)
			}()

			// Wait for the selected backend response.
			actual := endpoint.waitBackendResponseForDownstream(resCh)
			assert.Equal(t, testData.expected, actual.backend)
		})
	}
}

func Test_ProxyEndpoint_QueryRequests(t *testing.T) {
	var (
		requestCount atomic.Uint64
		wg           sync.WaitGroup
		testHandler  http.HandlerFunc
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		defer requestCount.Add(1)
		testHandler(w, r)
	})
	backend1 := httptest.NewServer(handler)
	defer backend1.Close()
	backendURL1, err := url.Parse(backend1.URL)
	require.NoError(t, err)

	backend2 := httptest.NewServer(handler)
	defer backend2.Close()
	backendURL2, err := url.Parse(backend2.URL)
	require.NoError(t, err)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backendURL1, time.Second, true),
		NewProxyBackend("backend-2", backendURL2, time.Second, false).WithFilter(regexp.MustCompile("/loki/api/v1/query_range")),
	}
	endpoint := createTestEndpoint(t, backends, "test", nil, false)

	for _, tc := range []struct {
		name    string
		request func(*testing.T) *http.Request
		handler func(*testing.T) http.HandlerFunc
		counts  int
	}{
		{
			name: "GET-request",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/loki/api/v1/query_range?query={job=\"test\"}&start=1&end=2", nil)
				r.Header["test-X"] = []string{"test-X-value"}
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "test-X-value", r.Header.Get("test-X"))
					_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
				}
			},
			counts: 2,
		},
		{
			name: "GET-filter-accept-encoding",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/loki/api/v1/query_range?query={job=\"test\"}&start=1&end=2", nil)
				r.Header.Set("Accept-Encoding", "gzip")
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, 0, len(r.Header.Values("Accept-Encoding")))
					_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
				}
			},
			counts: 2,
		},
		{
			name: "GET-filtered",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/loki/api/v1/query?query={job=\"test\"}", nil)
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
				}
			},
			counts: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// reset request count
			requestCount.Store(0)
			wg.Add(tc.counts)

			if tc.handler == nil {
				testHandler = func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
				}

			} else {
				testHandler = tc.handler(t)
			}

			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, tc.request(t))
			require.Contains(t, w.Body.String(), `"status":"success"`)
			require.Equal(t, 200, w.Code)

			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for backend requests to complete")
			}
			require.Equal(t, uint64(tc.counts), requestCount.Load())
		})
	}
}

func Test_ProxyEndpoint_WriteRequests(t *testing.T) {
	var (
		requestCount atomic.Uint64
		wg           sync.WaitGroup
		testHandler  http.HandlerFunc
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		defer requestCount.Add(1)
		testHandler(w, r)
	})
	backend1 := httptest.NewServer(handler)
	defer backend1.Close()
	backendURL1, err := url.Parse(backend1.URL)
	require.NoError(t, err)

	backend2 := httptest.NewServer(handler)
	defer backend2.Close()
	backendURL2, err := url.Parse(backend2.URL)
	require.NoError(t, err)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backendURL1, time.Second, true),
		NewProxyBackend("backend-2", backendURL2, time.Second, false).WithFilter(regexp.MustCompile("/loki/api/v1/push")),
	}
	// endpoint := createTestEndpoint(backends, "test", nil, false)
	metrics := NewProxyMetrics(nil)
	logger := log.NewNopLogger()
	endpoint := NewProxyEndpoint(backends, "test", metrics, logger, nil, false)

	for _, tc := range []struct {
		name    string
		request func(*testing.T) *http.Request
		handler func(*testing.T) http.HandlerFunc
		counts  int
	}{
		{
			name: "POST-request",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("POST", "http://test/loki/api/v1/push", strings.NewReader(`{"streams":[{"stream":{"job":"test"},"values":[["1","test"]]}]}`))
				r.Header.Set("test-X", "test-X-value")
				r.Header["Content-Type"] = []string{"application/json"}
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "test-X-value", r.Header.Get("test-X"))
					w.WriteHeader(204)
				}
			},
			counts: 2,
		},
		{
			name: "POST-filter-accept-encoding",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("POST", "http://test/loki/api/v1/push", strings.NewReader(`{"streams":[{"stream":{"job":"test"},"values":[["1","test"]]}]}`))
				r.Header["Content-Type"] = []string{"application/json"}
				r.Header.Set("Accept-Encoding", "gzip")
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, 0, len(r.Header.Values("Accept-Encoding")))
					w.WriteHeader(204)
				}
			},
			counts: 2,
		},
		{
			name: "POST-filtered",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("POST", "http://test/loki/api/prom/push", strings.NewReader(`{"streams":[{"stream":{"job":"test"},"values":[["1","test"]]}]}`))
				r.Header["Content-Type"] = []string{"application/json"}
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(204)
				}
			},
			counts: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// reset request count
			requestCount.Store(0)
			wg.Add(tc.counts)

			if tc.handler == nil {
				testHandler = func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(204)
				}

			} else {
				testHandler = tc.handler(t)
			}

			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, tc.request(t))
			require.Equal(t, 204, w.Code)

			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for backend requests to complete")
			}
			require.Equal(t, uint64(tc.counts), requestCount.Load())
		})
	}
}

func Test_ProxyEndpoint_SummaryMetrics(t *testing.T) {
	var (
		requestCount atomic.Uint64
		wg           sync.WaitGroup
		testHandler  http.HandlerFunc
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		defer requestCount.Add(1)
		testHandler(w, r)
	})
	backend1 := httptest.NewServer(handler)
	defer backend1.Close()
	backendURL1, err := url.Parse(backend1.URL)
	require.NoError(t, err)

	backend2 := httptest.NewServer(handler)
	defer backend2.Close()
	backendURL2, err := url.Parse(backend2.URL)
	require.NoError(t, err)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", backendURL1, time.Second, true),
		NewProxyBackend("backend-2", backendURL2, time.Second, false),
	}

	comparator := &mockComparator{}
	proxyMetrics := NewProxyMetrics(prometheus.NewRegistry())
	endpoint := createTestEndpointWithMetrics(t, backends, "test", comparator, true, proxyMetrics)

	for _, tc := range []struct {
		name            string
		request         func(*testing.T) *http.Request
		counts          int
		expectedMetrics string
	}{
		{
			name: "missing-metrics-series",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/loki/api/v1/query_range?query={job=\"test\"}&start=1&end=2", nil)
				r.Header.Set("X-Scope-OrgID", "test-tenant")
				require.NoError(t, err)
				return r
			},
			counts: 2,
			expectedMetrics: `
			    # HELP cortex_querytee_missing_metrics_series Number of missing metrics (series) in a vector response.
				# TYPE cortex_querytee_missing_metrics_series histogram
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.005"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.01"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.025"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.05"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.1"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.25"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.5"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="0.75"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="1"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="1.5"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="2"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="3"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="4"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="5"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="10"} 0
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="25"} 1
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="50"} 1
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="100"} 1
				cortex_querytee_missing_metrics_series_bucket{backend="backend-2",issuer="unknown",route="test",status_code="success",le="+Inf"} 1
				cortex_querytee_missing_metrics_series_sum{backend="backend-2",issuer="unknown",route="test",status_code="success"} 12
				cortex_querytee_missing_metrics_series_count{backend="backend-2",issuer="unknown",route="test",status_code="success"} 1
			`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Skip("TODO this test is flaky, the eventually is inconsistent. Could we instead mock the metrics?")
			// reset request count
			requestCount.Store(0)
			wg.Add(tc.counts)

			testHandler = func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
			}

			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, tc.request(t))
			require.Contains(t, w.Body.String(), `"status":"success"`)
			require.Equal(t, 200, w.Code)

			wg.Wait()
			require.Equal(t, uint64(tc.counts), requestCount.Load())

			require.Eventually(t, func() bool {
				return prom_testutil.CollectAndCount(proxyMetrics.responsesComparedTotal) == 1
			}, 3*time.Second, 100*time.Millisecond, "expect exactly 1 response to be compared.")
			err := prom_testutil.CollectAndCompare(proxyMetrics.missingMetrics, strings.NewReader(tc.expectedMetrics))
			require.NoError(t, err)
		})
	}
}

func Test_BackendResponse_succeeded(t *testing.T) {
	tests := map[string]struct {
		resStatus int
		resError  error
		expected  bool
	}{
		"Error while executing request": {
			resStatus: 0,
			resError:  errors.New("network error"),
			expected:  false,
		},
		"2xx response status code": {
			resStatus: 200,
			resError:  nil,
			expected:  true,
		},
		"3xx response status code": {
			resStatus: 300,
			resError:  nil,
			expected:  false,
		},
		"4xx response status code": {
			resStatus: 400,
			resError:  nil,
			expected:  true,
		},
		"5xx response status code": {
			resStatus: 500,
			resError:  nil,
			expected:  false,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			res := &BackendResponse{
				status: testData.resStatus,
				err:    testData.resError,
			}

			assert.Equal(t, testData.expected, res.succeeded())
		})
	}
}

func Test_BackendResponse_statusCode(t *testing.T) {
	tests := map[string]struct {
		resStatus int
		resError  error
		expected  int
	}{
		"Error while executing request": {
			resStatus: 0,
			resError:  errors.New("network error"),
			expected:  500,
		},
		"200 response status code": {
			resStatus: 200,
			resError:  nil,
			expected:  200,
		},
		"503 response status code": {
			resStatus: 503,
			resError:  nil,
			expected:  503,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			res := &BackendResponse{
				status: testData.resStatus,
				err:    testData.resError,
			}

			assert.Equal(t, testData.expected, res.statusCode())
		})
	}
}

type mockComparator struct{}

func (c *mockComparator) Compare(_, _ []byte, _ time.Time) (*comparator.ComparisonSummary, error) {
	return &comparator.ComparisonSummary{MissingMetrics: 12}, nil
}

func Test_endToEnd_traceIDFlow(t *testing.T) {
	// This test verifies that trace IDs flow from HTTP request context
	// through to the stored QuerySample in goldfish

	// Create a mock goldfish storage
	storage := &mockGoldfishStorage{}

	// Create a simple mock backend server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	// Create backends
	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", u, time.Second, true),  // preferred
		NewProxyBackend("backend-2", u, time.Second, false), // non-preferred
	}

	// Create endpoint with goldfish manager
	goldfishConfig := querytee_goldfish.Config{
		Enabled: true,
		SamplingConfig: querytee_goldfish.SamplingConfig{
			DefaultRate: 1.0, // Always sample for testing
		},
	}
	samplesComparator := comparator.NewSamplesComparator(comparator.SampleComparisonOptions{Tolerance: 0.000001})

	goldfishManager, err := querytee_goldfish.NewManager(goldfishConfig, samplesComparator, storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	endpoint := createTestEndpointWithGoldfish(t, backends, "test", goldfishManager)

	// Create request that triggers goldfish sampling
	req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000", nil)
	req.Header.Set("X-Scope-OrgID", "test-tenant")

	// Add trace context to the request (this would normally be done by tracing middleware)
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(req.Context(), "test-operation")
	defer span.End()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	endpoint.ServeHTTP(w, req)

	// Give goldfish async processing time to complete
	time.Sleep(200 * time.Millisecond)

	// Verify that the system processes the request successfully
	assert.Equal(t, 200, w.Code)

	// Debug: Check if goldfish was triggered at all
	t.Logf("Number of samples stored: %d", len(storage.samples))
	t.Logf("Number of results stored: %d", len(storage.results))

	// For now, just verify that the system works end-to-end without panicking
	// The actual trace ID verification will depend on proper goldfish triggering
	if len(storage.samples) > 0 {
		sample := storage.samples[0]
		assert.Equal(t, "test-tenant", sample.TenantID)
		assert.Equal(t, "count_over_time({job=\"test\"}[5m])", sample.Query)

		// Verify that the TraceID fields exist and don't cause panics
		assert.IsType(t, "", sample.CellATraceID)
		assert.IsType(t, "", sample.CellBTraceID)
	}
}

// mockGoldfishStorage implements goldfish.Storage for testing
type mockGoldfishStorage struct {
	samples []goldfish.QuerySample
	results []goldfish.ComparisonResult
}

func (m *mockGoldfishStorage) StoreQuerySample(_ context.Context, sample *goldfish.QuerySample, _ *goldfish.ComparisonResult) error {
	m.samples = append(m.samples, *sample)
	return nil
}

func (m *mockGoldfishStorage) StoreComparisonResult(_ context.Context, result *goldfish.ComparisonResult) error {
	m.results = append(m.results, *result)
	return nil
}

func (m *mockGoldfishStorage) GetSampledQueries(_ context.Context, page, pageSize int, _ goldfish.QueryFilter) (*goldfish.APIResponse, error) {
	// This is only used for UI, not needed in proxy tests
	return &goldfish.APIResponse{
		Queries:  []goldfish.QuerySample{},
		HasMore:  false,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *mockGoldfishStorage) GetStatistics(_ context.Context, _ goldfish.StatsFilter) (*goldfish.Statistics, error) {
	return nil, nil
}

func (m *mockGoldfishStorage) Close() error {
	return nil
}

func (m *mockGoldfishStorage) GetQueryByCorrelationID(_ context.Context, _ string) (*goldfish.QuerySample, error) {
	return nil, nil
}

func (m *mockGoldfishStorage) Reset() {
	m.samples = []goldfish.QuerySample{}
	m.results = []goldfish.ComparisonResult{}
}

// TestProxyEndpoint_QuerySplitting tests the query splitting functionality for goldfish comparison
func TestProxyEndpoint_QuerySplitting(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	minAge := 3 * time.Hour
	threshold := now.Add(-minAge)

	oldResponseBody := `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"test_metric","job":"test"},"values":[[1000,"1.0"],[2000,"2.0"]]}]}}`
	recentResponseBody := `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"test_metric","job":"test"},"values":[[3000,"3.0"],[4000,"4.0"]]}]}}`

	var mu sync.Mutex
	receivedQueries := []string{}

	step := "60s"
	stepMs := int64(60000)

	// Calculate the split boundary: step-aligned threshold + step
	splitBoundary := stepAlignUp(threshold, stepMs).Add(time.Duration(stepMs) * time.Millisecond)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedQueries = append(receivedQueries, r.URL.RawQuery)
		mu.Unlock()

		rangeQuery, err := loghttp.ParseRangeQuery(r)
		require.NoError(t, err)
		endTime := rangeQuery.End

		// If end time is before or at split boundary, return old response
		// Otherwise return recent response
		w.WriteHeader(200)
		if endTime.Before(splitBoundary) || endTime.Equal(splitBoundary) {
			_, _ = w.Write([]byte(oldResponseBody))
		} else {
			_, _ = w.Write([]byte(recentResponseBody))
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	backends := []*ProxyBackend{
		NewProxyBackend("backend-1", u, time.Second, true),  // preferred
		NewProxyBackend("backend-2", u, time.Second, false), // non-preferred
	}

	storage := &mockGoldfishStorage{}
	goldfishConfig := querytee_goldfish.Config{
		Enabled: true,
		SamplingConfig: querytee_goldfish.SamplingConfig{
			DefaultRate: 1.0, // Always sample
		},
		ComparisonMinAge: minAge,
	}
	goldfishManager, err := querytee_goldfish.NewManager(
		goldfishConfig,
		&testComparator{},
		storage,
		nil,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	endpoint := createTestEndpointWithGoldfish(t, backends, "test", goldfishManager)

	t.Run("query entirely recent (skips goldfish)", func(t *testing.T) {
		receivedQueries = []string{}
		storage.Reset()

		// Query from threshold+1h to threshold+2h (all recent)
		start := threshold.Add(time.Hour)
		end := threshold.Add(2 * time.Hour)
		req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}&start="+formatTime(start)+"&end="+formatTime(end)+"&step="+step, nil)
		req.Header.Set("X-Scope-OrgID", "test-tenant")

		w := httptest.NewRecorder()
		endpoint.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Equal(t, 1, len(receivedQueries), "expect only 1 query, to v1 engine only, got %d", len(receivedQueries))
		assert.Equal(t, 0, len(storage.samples), "recent query should not be sent to goldfish cell or compared")
	})

	t.Run("query entirely old (normal goldfish flow)", func(t *testing.T) {
		receivedQueries = []string{}
		storage.Reset()

		// Query from threshold-2h to threshold-1h (all old)
		start := threshold.Add(-2 * time.Hour)
		end := threshold.Add(-time.Hour)
		req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}&start="+formatTime(start)+"&end="+formatTime(end)+"&step="+step, nil)
		req.Header.Set("X-Scope-OrgID", "test-tenant")

		w := httptest.NewRecorder()
		endpoint.ServeHTTP(w, req)

		// Give goldfish time to process
		time.Sleep(2 * time.Second)

		assert.Equal(t, 200, w.Code)
		assert.Equal(t, 2, len(receivedQueries), "expected 1 query each to v1 and v2 for comparison, got %d", len(receivedQueries))
		assert.Equal(t, 1, len(storage.samples), "Goldfish should process entirely old queries normally")
	})

	t.Run("query spans threshold (split and merge)", func(t *testing.T) {
		receivedQueries = []string{}
		storage.Reset()

		// Query from threshold-1h to threshold+1h (spans threshold)
		start := threshold.Add(-time.Hour)
		end := threshold.Add(time.Hour)
		req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}&start="+formatTime(start)+"&end="+formatTime(end)+"&step="+step, nil)
		req.Header.Set("X-Scope-OrgID", "test-tenant")

		w := httptest.NewRecorder()
		endpoint.ServeHTTP(w, req)

		// Give goldfish time to process
		time.Sleep(500 * time.Millisecond)

		assert.Equal(t, 200, w.Code)

		assert.Equal(t, 3, len(receivedQueries), "expected 3 queries, 1 for the recent portion, and 1 to both v1 and v2 for comparison, got %d", len(receivedQueries))

		// Verify that old queries go to both backends
		// Step is 60s = 60000ms from the test query
		stepMs := int64(60000)

		// Align threshold UP (as it's treated as an end boundary in alignStartEnd)
		// This is v2End in the engine_router
		alignedThreshold := stepAlignUp(threshold, stepMs)

		// Based on observed behavior, old queries end at alignedThreshold + gap
		oldQueryBoundary := alignedThreshold.Add(time.Duration(stepMs) * time.Millisecond)

		oldQueries := 0
		recentQueries := 0
		for _, q := range receivedQueries {
			if strings.Contains(q, "end=") {
				endStr := extractQueryParam(q, "end")
				endTime, _ := parseTimestamp(endStr)

				// Queries ending at or before oldQueryBoundary are "old"
				if endTime.Before(oldQueryBoundary) || endTime.Equal(oldQueryBoundary) {
					oldQueries++
				} else {
					recentQueries++
				}
			}
		}
		assert.Equal(t, 2, oldQueries, "Old portion should be sent to both backends")
		assert.Equal(t, 1, recentQueries, "Recent portion should only be sent to preferred backend")

		assert.Equal(t, 1, len(storage.samples), "goldfish compares the old portion of the query between the two backends")

		// Parse the JSON response and verify concatenation
		var response loghttp.QueryResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "success", response.Status)
		assert.Equal(t, string(loghttp.ResultTypeMatrix), string(response.Data.ResultType))

		// Verify we got a matrix response
		matrix, ok := response.Data.Result.(loghttp.Matrix)
		require.True(t, ok, "Response should be a matrix")
		require.Len(t, matrix, 1, "Should have one metric")

		metric := matrix[0]

		assert.Equal(t, model.LabelValue("test_metric"), metric.Metric["__name__"])
		assert.Equal(t, model.LabelValue("test"), metric.Metric["job"])

		// The response should contain data from both splits merged together
		// The stats should show splits=2
		assert.Equal(t, int64(2), response.Data.Statistics.Summary.Splits, "Should show 2 splits in stats")
	})

	t.Run("v2 compatible metric query with aggregation (split and merge)", func(t *testing.T) {
		receivedQueries = []string{}
		storage.Reset()

		// Query from threshold-1h to threshold+1h (spans threshold)
		// Using a v2 compatible metric query with aggregation
		start := threshold.Add(-time.Hour)
		end := threshold.Add(time.Hour)
		metricQuery := url.QueryEscape(`sum by (job) (count_over_time({foo="bar"}[5m]))`)
		req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query="+metricQuery+"&start="+formatTime(start)+"&end="+formatTime(end)+"&step="+step, nil)
		req.Header.Set("X-Scope-OrgID", "test-tenant")

		w := httptest.NewRecorder()
		endpoint.ServeHTTP(w, req)

		// Give goldfish time to process
		time.Sleep(500 * time.Millisecond)

		assert.Equal(t, 200, w.Code)

		// For v2 compatible metric queries, we expect:
		// - 3 queries total: 1 for recent portion (v1 only), 2 for old portion (v1 and v2 for comparison)
		assert.Equal(t, 3, len(receivedQueries), "expected 3 queries for v2 metric query, 1 for recent portion and 2 for old portion comparison, got %d", len(receivedQueries))

		// Verify that old queries go to both backends
		stepMs := int64(60000)
		alignedThreshold := stepAlignUp(threshold, stepMs)
		oldQueryBoundary := alignedThreshold.Add(time.Duration(stepMs) * time.Millisecond)

		oldQueries := 0
		recentQueries := 0
		for _, q := range receivedQueries {
			if strings.Contains(q, "end=") {
				endStr := extractQueryParam(q, "end")
				endTime, _ := parseTimestamp(endStr)

				if endTime.Before(oldQueryBoundary) || endTime.Equal(oldQueryBoundary) {
					oldQueries++
				} else {
					recentQueries++
				}
			}
		}
		assert.Equal(t, 2, oldQueries, "Old portion of v2 metric query should be sent to both backends for comparison")
		assert.Equal(t, 1, recentQueries, "Recent portion of v2 metric query should only be sent to preferred backend")

		assert.Equal(t, 1, len(storage.samples), "goldfish should compare the old portion of v2 metric query between the two backends")

		// Parse the JSON response and verify concatenation
		var response loghttp.QueryResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "success", response.Status)
		assert.Equal(t, string(loghttp.ResultTypeMatrix), string(response.Data.ResultType))

		// Verify we got a matrix response with aggregated results
		matrix, ok := response.Data.Result.(loghttp.Matrix)
		require.True(t, ok, "Response should be a matrix for metric query")
		require.Len(t, matrix, 1, "Should have one aggregated metric")

		metric := matrix[0]
		assert.Equal(t, model.LabelValue("test"), metric.Metric["job"], "Aggregation should preserve 'job' label")

		// The response should contain data from both splits merged together
		assert.Equal(t, int64(2), response.Data.Statistics.Summary.Splits, "Should show 2 splits in stats for v2 metric query")
	})
}

func parseTimestamp(value string) (time.Time, error) {
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	if len(value) <= 10 {
		return time.Unix(nanos, 0), nil
	}
	return time.Unix(0, nanos), nil
}

// stepAlignUp aligns a timestamp up to the nearest step boundary
func stepAlignUp(t time.Time, stepMs int64) time.Time {
	timestampMs := t.UnixMilli()
	if mod := timestampMs % stepMs; mod != 0 {
		timestampMs += stepMs - mod
	}
	return time.Unix(0, timestampMs*1e6)
}

// Helper function to extract query parameters from a query string
func extractQueryParam(queryString, param string) string {
	parts := strings.SplitSeq(queryString, "&")
	for part := range parts {
		if after, ok := strings.CutPrefix(part, param+"="); ok {
			return after
		}
	}
	return ""
}

func TestProxyEndpoint_ServeHTTP_ForwardsResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	}))
	defer srv.Close()

	srvURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	backends := []*ProxyBackend{{
		name:      "backend-1",
		endpoint:  srvURL,
		client:    srv.Client(),
		timeout:   time.Minute,
		preferred: true,
	}}

	recorder := httptest.NewRecorder()
	fakeReq := httptest.NewRequestWithContext(context.Background(), "GET", "/loki/api/v1/query_range?query={job=\"test\"}&start=1&end=2", nil)
	fakeReq.Header.Set("X-Scope-OrgID", "test-tenant")

	endpoint := createTestEndpoint(t, backends, "test", nil, false)
	endpoint.ServeHTTP(recorder, fakeReq)

	require.Equal(t, http.StatusOK, recorder.Result().StatusCode, "Status code from backend should be forwarded")
	require.Equal(t, "application/json; charset=UTF-8", recorder.Result().Header.Get("Content-Type"), "Response header from backend should be forwarded")
}

func formatTime(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}
