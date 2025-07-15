package querytee

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

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
		responses []*backendResponse
		expected  *ProxyBackend
	}{
		"the preferred backend is the 1st response received": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*backendResponse{
				{backend: backendPref, status: 200},
			},
			expected: backendPref,
		},
		"the preferred backend is the last response received": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*backendResponse{
				{backend: backendOther1, status: 200},
				{backend: backendPref, status: 200},
			},
			expected: backendPref,
		},
		"the preferred backend is the last response received but it's not successful": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*backendResponse{
				{backend: backendOther1, status: 200},
				{backend: backendPref, status: 500},
			},
			expected: backendOther1,
		},
		"the preferred backend is the 2nd response received but only the last one is successful": {
			backends: []*ProxyBackend{backendPref, backendOther1, backendOther2},
			responses: []*backendResponse{
				{backend: backendOther1, status: 500},
				{backend: backendPref, status: 500},
				{backend: backendOther2, status: 200},
			},
			expected: backendOther2,
		},
		"there's no preferred backend configured and the 1st response is successful": {
			backends: []*ProxyBackend{backendOther1, backendOther2},
			responses: []*backendResponse{
				{backend: backendOther1, status: 200},
			},
			expected: backendOther1,
		},
		"there's no preferred backend configured and the last response is successful": {
			backends: []*ProxyBackend{backendOther1, backendOther2},
			responses: []*backendResponse{
				{backend: backendOther1, status: 500},
				{backend: backendOther2, status: 200},
			},
			expected: backendOther2,
		},
		"no received response is successful": {
			backends: []*ProxyBackend{backendPref, backendOther1},
			responses: []*backendResponse{
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
			resCh := make(chan *backendResponse)
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

func Test_ProxyEndpoint_Requests(t *testing.T) {
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
		NewProxyBackend("backend-2", backendURL2, time.Second, false).WithFilter(regexp.MustCompile("/test/api")),
	}
	endpoint := NewProxyEndpoint(backends, "test", NewProxyMetrics(nil), log.NewNopLogger(), nil, false)

	for _, tc := range []struct {
		name    string
		request func(*testing.T) *http.Request
		handler func(*testing.T) http.HandlerFunc
		counts  int
	}{
		{
			name: "GET-request",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/api/v1/test", nil)
				r.Header["test-X"] = []string{"test-X-value"}
				require.NoError(t, err)
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "test-X-value", r.Header.Get("test-X"))
					_, _ = w.Write([]byte("ok"))
				}
			},
			counts: 2,
		},
		{
			name: "GET-filter-accept-encoding",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/api/v1/test", nil)
				r.Header.Set("Accept-Encoding", "gzip")
				require.NoError(t, err)
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, 0, len(r.Header.Values("Accept-Encoding")))
					_, _ = w.Write([]byte("ok"))
				}
			},
			counts: 2,
		},
		{
			name: "GET-filtered",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://will/not/pass/api/v1/test", nil)
				require.NoError(t, err)
				return r
			},
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}
			},
			counts: 1,
		},
		{
			name: "POST-request-with-body",
			request: func(t *testing.T) *http.Request {
				strings := strings.NewReader("this-is-some-payload")
				r, err := http.NewRequest("POST", "http://test/api/v1/test", strings)
				require.NoError(t, err)
				r.Header["test-X"] = []string{"test-X-value"}
				return r
			},
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					require.Equal(t, "this-is-some-payload", string(body))
					require.NoError(t, err)
					require.Equal(t, "test-X-value", r.Header.Get("test-X"))

					_, _ = w.Write([]byte("ok"))
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
					_, _ = w.Write([]byte("ok"))
				}

			} else {
				testHandler = tc.handler(t)
			}

			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, tc.request(t))
			require.Equal(t, "ok", w.Body.String())
			require.Equal(t, 200, w.Code)

			wg.Wait()
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
	endpoint := NewProxyEndpoint(backends, "test", proxyMetrics, log.NewNopLogger(), comparator, true)

	for _, tc := range []struct {
		name            string
		request         func(*testing.T) *http.Request
		counts          int
		expectedMetrics string
	}{
		{
			name: "missing-metrics-series",
			request: func(t *testing.T) *http.Request {
				r, err := http.NewRequest("GET", "http://test/api/v1/test", nil)
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
			// reset request count
			requestCount.Store(0)
			wg.Add(tc.counts)

			testHandler = func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("ok"))
			}

			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, tc.request(t))
			require.Equal(t, "ok", w.Body.String())
			require.Equal(t, 200, w.Code)

			wg.Wait()
			require.Equal(t, uint64(tc.counts), requestCount.Load())

			require.Eventually(t, func() bool {
				return prom_testutil.ToFloat64(proxyMetrics.responsesComparedTotal) == 1
			}, 2*time.Second, 100*time.Millisecond, "expect exactly 1 response to be compared.")
			err := prom_testutil.CollectAndCompare(proxyMetrics.missingMetrics, strings.NewReader(tc.expectedMetrics))
			require.NoError(t, err)
		})
	}
}

func Test_backendResponse_succeeded(t *testing.T) {
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
			res := &backendResponse{
				status: testData.resStatus,
				err:    testData.resError,
			}

			assert.Equal(t, testData.expected, res.succeeded())
		})
	}
}

func Test_backendResponse_statusCode(t *testing.T) {
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
			res := &backendResponse{
				status: testData.resStatus,
				err:    testData.resError,
			}

			assert.Equal(t, testData.expected, res.statusCode())
		})
	}
}

type mockComparator struct{}

func (c *mockComparator) Compare(_, _ []byte, _ time.Time) (*ComparisonSummary, error) {
	return &ComparisonSummary{missingMetrics: 12}, nil
}
