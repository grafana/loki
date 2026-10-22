package querytee

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// createTestEndpoint creates a ProxyEndpoint with properly initialized handler pipeline
func createTestEndpoint(t *testing.T, backends []*ProxyBackend, routeName string, comparator comparator.ResponsesComparator, reportMissingSeries bool) *ProxyEndpoint {
	t.Helper()

	metrics := NewProxyMetrics(nil)
	logger := log.NewNopLogger()

	endpoint := NewProxyEndpoint(backends, routeName, metrics, logger, comparator, reportMissingSeries)
	handlerFactory := NewHandlerFactory(HandlerFactoryConfig{
		Backends: backends,
		Codec:    queryrange.DefaultCodec,
		Logger:   logger,
		Metrics:  metrics,
	})

	endpoint.WithQueryHandler(handlerFactory.CreateHandler(routeName, comparator))
	return endpoint
}

func Test_ProxyEndpoint_waitBackendResponseForDownstream(t *testing.T) {
	backendURL1, err := url.Parse("http://backend-1/")
	require.NoError(t, err)
	backendURL2, err := url.Parse("http://backend-2/")
	require.NoError(t, err)

	backendPrimary, err := NewProxyBackend("backend-1", backendURL1, time.Second, RolePrimary)
	require.NoError(t, err)
	backendOther, err := NewProxyBackend("backend-2", backendURL2, time.Second, RoleOther)
	require.NoError(t, err)

	tests := map[string]struct {
		backends    []*ProxyBackend
		responses   []*BackendResponse
		expected    *ProxyBackend
		expectedErr string
	}{
		"returns the primary response when another backend answers first": {
			backends: []*ProxyBackend{backendPrimary, backendOther},
			responses: []*BackendResponse{
				{backend: backendOther, status: 200},
				{backend: backendPrimary, status: 200},
			},
			expected: backendPrimary,
		},
		"returns the primary error response when another backend succeeds": {
			backends: []*ProxyBackend{backendPrimary, backendOther},
			responses: []*BackendResponse{
				{backend: backendOther, status: 200},
				{backend: backendPrimary, status: 500},
			},
			expected: backendPrimary,
		},
		"returns an error when no backend is the primary": {
			backends: []*ProxyBackend{backendOther},
			responses: []*BackendResponse{
				{backend: backendOther, status: 200},
			},
			expectedErr: "no primary backend",
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

			actual, err := endpoint.waitBackendResponseForDownstream(resCh)
			if testData.expectedErr != "" {
				require.EqualError(t, err, testData.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testData.expected, actual.backend)
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

	proxyBackend1, err := NewProxyBackend("backend-1", backendURL1, time.Second, RolePrimary)
	require.NoError(t, err)
	proxyBackend2, err := NewProxyBackend("backend-2", backendURL2, time.Second, RoleSecondary)
	require.NoError(t, err)
	backends := []*ProxyBackend{
		proxyBackend1,
		proxyBackend2,
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
				r, err := http.NewRequest("GET", "http://test"+constants.PathLokiQueryRange+"?query={job=\"test\"}&start=1&end=2", nil)
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
				r, err := http.NewRequest("GET", "http://test"+constants.PathLokiQueryRange+"?query={job=\"test\"}&start=1&end=2", nil)
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

	proxyBackend1, err := NewProxyBackend("backend-1", backendURL1, time.Second, RolePrimary)
	require.NoError(t, err)
	proxyBackend2, err := NewProxyBackend("backend-2", backendURL2, time.Second, RoleSecondary)
	require.NoError(t, err)
	backends := []*ProxyBackend{
		proxyBackend1,
		proxyBackend2,
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
				r, err := http.NewRequest("POST", "http://test"+constants.PathLokiPush, strings.NewReader(`{"streams":[{"stream":{"job":"test"},"values":[["1","test"]]}]}`))
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
				r, err := http.NewRequest("POST", "http://test"+constants.PathLokiPush, strings.NewReader(`{"streams":[{"stream":{"job":"test"},"values":[["1","test"]]}]}`))
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
