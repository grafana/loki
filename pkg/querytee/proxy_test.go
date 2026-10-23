package querytee

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/flagext"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/querytee/comparator"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var testReadRoutes = []Route{
	{Path: constants.PathLokiQuery, RouteName: "api_v1_query", Methods: []string{"GET"}, ResponseComparator: &testComparator{}},
}

var testWriteRoutes = []Route{}

type testComparator struct{}

func (testComparator) Compare(_, _ []byte, _ time.Time) (*comparator.ComparisonSummary, error) {
	return nil, nil
}

func Test_NewProxy(t *testing.T) {
	tests := map[string]struct {
		configure     func(cfg *ProxyConfig)
		lokiCfg       *LokiParsedConfig
		expectedErr   string
		expectedRoles []BackendRole
	}{
		"returns an error when there is no backend": {
			expectedErr: errMinBackends.Error(),
		},
		"returns an error when the primary hostname is not set": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://b:80"
			},
			expectedErr: "-primary-backend.hostname flag must be set",
		},
		"returns an error when no backend has the primary hostname": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://b:80"
				cfg.PrimaryBackend.Hostname = "c"
			},
			expectedErr: "the primary backend (hostname) has not been found among the list of configured backends",
		},
		"returns an error when more than one backend has the primary hostname": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://a:81"
				cfg.PrimaryBackend.Hostname = "a"
			},
			expectedErr: "more than one configured backend has the hostname of the primary backend",
		},
		"returns an error when no backend has the secondary hostname": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://b:80"
				cfg.PrimaryBackend.Hostname = "a"
				cfg.SecondaryBackend.Hostname = "c"
			},
			expectedErr: "the secondary backend (hostname) has not been found among the list of configured backends",
		},
		"returns an error when the comparison is enabled without a secondary backend": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://b:80"
				cfg.PrimaryBackend.Hostname = "a"
				cfg.CompareResponses = true
			},
			expectedErr: "when enabling comparison of results -secondary-backend.hostname flag must be set",
		},
		"returns an error when a selection strategy is set without a secondary backend": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80"
				cfg.PrimaryBackend.Hostname = "a"
				cfg.SecondaryBackend.SelectionStrategy = selectionStrategyDataObj
			},
			expectedErr: "-secondary-backend.selection-strategy needs -secondary-backend.hostname",
		},
		"returns an error when the selection strategy is unknown": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://b:80"
				cfg.PrimaryBackend.Hostname = "a"
				cfg.SecondaryBackend.Hostname = "b"
				cfg.SecondaryBackend.SelectionStrategy = "unknown"
			},
			expectedErr: `unknown selection strategy "unknown"`,
		},
		"returns an error when the dataobj strategy has no Loki config": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://a:80,http://b:80"
				cfg.PrimaryBackend.Hostname = "a"
				cfg.SecondaryBackend.Hostname = "b"
				cfg.SecondaryBackend.SelectionStrategy = selectionStrategyDataObj
			},
			expectedErr: "the dataobj selection strategy needs the Loki config of the secondary backend: set -config.file and, for per-tenant limits, -runtime-config.file",
		},
		"assigns the roles by hostname": {
			configure: func(cfg *ProxyConfig) {
				cfg.BackendEndpoints = "http://b:80,http://c:80,http://a:80"
				cfg.PrimaryBackend.Hostname = "a"
				cfg.SecondaryBackend.Hostname = "b"
			},
			expectedRoles: []BackendRole{RoleSecondary, RoleOther, RolePrimary},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			cfg := newTestProxyConfig()
			if testData.configure != nil {
				testData.configure(&cfg)
			}

			p, err := NewProxy(cfg, log.NewNopLogger(), testReadRoutes, testWriteRoutes, nil, testData.lokiCfg)

			if testData.expectedErr != "" {
				require.EqualError(t, err, testData.expectedErr)
				require.Nil(t, p)
				return
			}

			require.NoError(t, err)
			roles := make([]BackendRole, 0, len(p.backends))
			for _, b := range p.backends {
				roles = append(roles, b.role)
			}
			require.Equal(t, testData.expectedRoles, roles)
		})
	}
}

func Test_Proxy_RequestsForwarding(t *testing.T) {
	const (
		querySingleMetric1 = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"cortex_build_info"},"value":[1583320883,"1"]}]}}`
		querySingleMetric2 = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"cortex_build_info"},"value":[1583320883,"2"]}]}}`
	)

	type mockedBackend struct {
		pathPrefix string
		handler    http.HandlerFunc
	}

	tests := map[string]struct {
		backends             []mockedBackend
		primaryBackendIdx    int
		expectedStatus       int
		expectedRes          string
		expectedBodyFragment string
	}{
		"one backend returning 2xx": {
			backends: []mockedBackend{
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric1)},
			},
			expectedStatus: 200,
			expectedRes:    querySingleMetric1,
		},
		"two backends without path prefix": {
			backends: []mockedBackend{
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric1)},
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric2)},
			},
			primaryBackendIdx:    0,
			expectedStatus:       200,
			expectedRes:          querySingleMetric1,
			expectedBodyFragment: `1583320883,"1"`,
		},
		"returns the response of the second backend when it is the primary": {
			backends: []mockedBackend{
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric1)},
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric2)},
			},
			primaryBackendIdx:    1,
			expectedStatus:       200,
			expectedRes:          querySingleMetric2,
			expectedBodyFragment: `1583320883,"2"`,
		},
		"two backends with the same path prefix": {
			backends: []mockedBackend{
				{
					pathPrefix: "/api/prom",
					handler:    mockQueryResponse("/api/prom"+constants.PathLokiQuery, 200, querySingleMetric1),
				},
				{
					pathPrefix: "/api/prom",
					handler:    mockQueryResponse("/api/prom"+constants.PathLokiQuery, 200, querySingleMetric2),
				},
			},
			primaryBackendIdx: 0,
			expectedStatus:    200,
			expectedRes:       querySingleMetric1,
		},
		"two backends with different path prefix": {
			backends: []mockedBackend{
				{
					pathPrefix: "/prefix-1",
					handler:    mockQueryResponse("/prefix-1"+constants.PathLokiQuery, 200, querySingleMetric1),
				},
				{
					pathPrefix: "/prefix-2",
					handler:    mockQueryResponse("/prefix-2"+constants.PathLokiQuery, 200, querySingleMetric2),
				},
			},
			primaryBackendIdx: 0,
			expectedStatus:    200,
			expectedRes:       querySingleMetric1,
		},
		"returns the 5xx of the primary backend and does not fall back to the secondary backend": {
			backends: []mockedBackend{
				{handler: mockQueryResponse(constants.PathLokiQuery, 500, "")},
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric1)},
			},
			primaryBackendIdx: 0,
			expectedStatus:    500,
			expectedRes:       "",
		},
		"secondary backend returns 5xx": {
			backends: []mockedBackend{
				{handler: mockQueryResponse(constants.PathLokiQuery, 200, querySingleMetric1)},
				{handler: mockQueryResponse(constants.PathLokiQuery, 500, "")},
			},
			primaryBackendIdx: 0,
			expectedStatus:    200,
			expectedRes:       querySingleMetric1,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			backendURLs := []string{}

			// Start backend servers.
			for _, b := range testData.backends {
				s := httptest.NewServer(b.handler)
				defer s.Close()

				backendURLs = append(backendURLs, s.URL+b.pathPrefix)
			}

			// Start the proxy.
			cfg := newTestProxyConfig()
			cfg.BackendEndpoints = strings.Join(backendURLs, ",")
			cfg.BackendReadTimeout = time.Second
			cfg.PrimaryBackend.Hostname = strconv.Itoa(testData.primaryBackendIdx)

			if len(backendURLs) == 2 {
				cfg.CompareResponses = true
				cfg.SecondaryBackend.Hostname = strconv.Itoa(1 - testData.primaryBackendIdx)
			}

			p, err := NewProxy(cfg, log.NewNopLogger(), testReadRoutes, testWriteRoutes, nil, nil)
			require.NoError(t, err)
			require.NotNil(t, p)
			defer p.Stop() //nolint:errcheck

			require.NoError(t, p.Start())

			// Send a query request to the proxy.
			req, err := http.NewRequest("GET", fmt.Sprintf("http://%s%s?query=rate({job=\"test\"}[5m])", p.Endpoint(), constants.PathLokiQuery), nil)
			require.NoError(t, err)
			req.Header.Set("X-Scope-OrgID", "test-tenant")

			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			defer res.Body.Close()
			require.NoError(t, err)

			assert.Equal(t, testData.expectedStatus, res.StatusCode)
			if testData.expectedBodyFragment != "" {
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), testData.expectedBodyFragment)
			}
		})
	}
}

func Test_Proxy_RoutesRequestsToAllBackends(t *testing.T) {
	const (
		primaryIdx   = 0
		secondaryIdx = 1
		otherIdx     = 2
	)

	var (
		reads  [3]atomic.Int32
		writes [3]atomic.Int32
		served = make(chan struct{}, 16)
		comp   = &recordingComparator{}
	)

	var backendURLs []string
	for idx := range 3 {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case constants.PathLokiQuery:
				reads[idx].Add(1)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
			case constants.PathLokiPush:
				writes[idx].Add(1)
				w.WriteHeader(http.StatusNoContent)
			}
			served <- struct{}{}
		}))
		defer s.Close()

		backendURLs = append(backendURLs, s.URL)
	}

	cfg := newTestProxyConfig()
	cfg.BackendEndpoints = strings.Join(backendURLs, ",")
	cfg.BackendReadTimeout = time.Second
	cfg.CompareResponses = true
	cfg.PrimaryBackend.Hostname = strconv.Itoa(primaryIdx)
	cfg.SecondaryBackend.Hostname = strconv.Itoa(secondaryIdx)

	p, err := NewProxy(cfg, log.NewNopLogger(), []Route{{Path: constants.PathLokiQuery, RouteName: "api_v1_query", Methods: []string{"GET"}, ResponseComparator: comp}}, []Route{{Path: constants.PathLokiPush, RouteName: "api_v1_push", Methods: []string{"POST"}}}, nil, nil)
	require.NoError(t, err)
	defer p.Stop() //nolint:errcheck
	require.NoError(t, p.Start())

	send := func(t *testing.T, method, path string) {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), method, fmt.Sprintf("http://%s%s", p.Endpoint(), path), strings.NewReader(""))
		require.NoError(t, err)
		req.Header.Set("X-Scope-OrgID", "test-tenant")
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}

	waitServed := func(t *testing.T, count int) {
		t.Helper()

		for range count {
			select {
			case <-served:
			case <-time.After(5 * time.Second):
				t.Fatal("a backend did not receive the request")
			}
		}
	}

	t.Run("sends a read to every backend and compares the secondary response", func(t *testing.T) {
		send(t, http.MethodGet, constants.PathLokiQuery+`?query=rate({job="test"}[5m])`)
		waitServed(t, 3)
		require.Eventually(t, func() bool { return len(comp.recorded()) > 0 }, 5*time.Second, 10*time.Millisecond)

		require.Equal(t, int32(1), reads[primaryIdx].Load())
		require.Equal(t, int32(1), reads[secondaryIdx].Load())
		require.Equal(t, int32(1), reads[otherIdx].Load())
	})

	t.Run("sends a write to every backend", func(t *testing.T) {
		send(t, http.MethodPost, constants.PathLokiPush)
		waitServed(t, 3)

		require.Equal(t, int32(1), writes[primaryIdx].Load())
		require.Equal(t, int32(1), writes[secondaryIdx].Load())
		require.Equal(t, int32(1), writes[otherIdx].Load())
	})
}

func TestProxy_SecondaryBackend(t *testing.T) {
	// fakeBackend answers every request with its name, and counts the requests it gets.
	type fakeBackend struct {
		url      string
		requests *atomic.Int64
	}

	startFakeBackend := func(t *testing.T, name string) *fakeBackend {
		b := &fakeBackend{requests: &atomic.Int64{}}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			b.requests.Add(1)
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"backend":%q},"values":[[1,"1"]]}]}}`, name)
		}))
		t.Cleanup(server.Close)
		b.url = server.URL
		return b
	}

	// The tenant "enabled" holds data objects from 48 hours ago until 3 hours ago.
	lokiCfg := newTestLokiParsedConfig(t, 3*time.Hour, time.Now().Add(-48*time.Hour))
	routes := []Route{{Path: constants.PathLokiQueryRange, RouteName: "api_v1_query_range", Methods: []string{"GET"}, ResponseComparator: &testComparator{}}}

	// startProxy starts a proxy with the dataobj strategy in front of a primary, a secondary and an
	// other backend.
	startProxy := func(t *testing.T) (p *Proxy, primary, secondary, other *fakeBackend) {
		t.Helper()

		primary, secondary, other = startFakeBackend(t, "primary"), startFakeBackend(t, "secondary"), startFakeBackend(t, "other")
		cfg := newTestProxyConfig()
		cfg.BackendEndpoints = primary.url + "," + secondary.url + "," + other.url
		cfg.BackendReadTimeout = time.Second
		cfg.CompareResponses = true
		cfg.PrimaryBackend.Hostname = "0"
		cfg.SecondaryBackend.Hostname = "1"
		cfg.SecondaryBackend.SelectionStrategy = selectionStrategyDataObj

		p, err := NewProxy(cfg, log.NewNopLogger(), routes, nil, nil, lokiCfg)
		require.NoError(t, err)
		require.NoError(t, p.Start())
		t.Cleanup(func() { _ = p.Stop() })
		return p, primary, secondary, other
	}

	// query sends a range query and returns the body that the client gets.
	query := func(t *testing.T, p *Proxy, logql string) string {
		t.Helper()

		start, end := time.Now().Add(-24*time.Hour), time.Now().Add(-12*time.Hour)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("http://%s%s", p.Endpoint(), constants.PathLokiQueryRange), nil)
		require.NoError(t, err)
		q := req.URL.Query()
		q.Set("query", logql)
		q.Set("start", fmt.Sprint(start.UnixNano()))
		q.Set("end", fmt.Sprint(end.UnixNano()))
		q.Set("step", "60")
		req.URL.RawQuery = q.Encode()
		req.Header.Set("X-Scope-OrgID", "enabled")

		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)

		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		return string(body)
	}

	t.Run("sends a query that data objects can serve to both backends", func(t *testing.T) {
		p, primary, secondary, other := startProxy(t)

		body := query(t, p, `sum(count_over_time({job="test"}[5m]))`)

		require.Contains(t, body, `"backend":"primary"`)
		require.NotContains(t, body, `"backend":"secondary"`)
		require.Equal(t, int64(1), primary.requests.Load())
		require.Eventually(t, func() bool { return secondary.requests.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Eventually(t, func() bool { return other.requests.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Equal(t, 1.0, prom_testutil.ToFloat64(p.metrics.secondaryBackendDecisions.WithLabelValues("api_v1_query_range", decisionSent)))
	})

	t.Run("sends a query that the strategy declines to every backend but the secondary backend", func(t *testing.T) {
		p, primary, secondary, other := startProxy(t)

		body := query(t, p, `rate({job="test"}[5m])`)

		require.Contains(t, body, `"backend":"primary"`)
		require.Equal(t, int64(1), primary.requests.Load())
		require.Eventually(t, func() bool { return other.requests.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Never(t, func() bool { return secondary.requests.Load() > 0 }, 200*time.Millisecond, 20*time.Millisecond)
		require.Equal(t, 1.0, prom_testutil.ToFloat64(p.metrics.secondaryBackendDecisions.WithLabelValues("api_v1_query_range", "unsupported_query")))
	})
}

func TestProxyConfig_RegisterFlags(t *testing.T) {
	newFlagSet := func() (*flag.FlagSet, *ProxyConfig) {
		cfg := &ProxyConfig{}
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		cfg.RegisterFlags(fs)
		return fs, cfg
	}

	t.Run("sets the hostnames of the primary and the secondary backend", func(t *testing.T) {
		fs, cfg := newFlagSet()

		require.NoError(t, fs.Parse([]string{"-primary-backend.hostname=a", "-secondary-backend.hostname=b"}))

		require.Equal(t, "a", cfg.PrimaryBackend.Hostname)
		require.Equal(t, "b", cfg.SecondaryBackend.Hostname)
	})

	t.Run("sets the all selection strategy of the secondary backend by default", func(t *testing.T) {
		fs, cfg := newFlagSet()

		require.NoError(t, fs.Parse(nil))

		require.Equal(t, selectionStrategyAll, cfg.SecondaryBackend.SelectionStrategy)
	})

	t.Run("sets the selection strategy of the secondary backend", func(t *testing.T) {
		fs, cfg := newFlagSet()

		require.NoError(t, fs.Parse([]string{"-secondary-backend.selection-strategy=dataobj"}))

		require.Equal(t, selectionStrategyDataObj, cfg.SecondaryBackend.SelectionStrategy)
	})
}

func TestProxy_Passthrough(t *testing.T) {
	type route struct {
		path, response string
	}

	type mockedBackend struct {
		routes []route
	}

	type query struct {
		path               string
		expectedRes        string
		expectedStatusCode int
	}

	const (
		pathCommon = "/common" // common path implemented by both backends

		pathZero = "/zero" // only implemented by backend at index 0
		pathOne  = "/one"  // only implemented by backend at index 1

		// responses by backend at index 0
		responseCommon0 = "common-0"
		responseZero    = "zero"

		// responses by backend at index 1
		responseCommon1 = "common-1"
		responseOne     = "one"
	)

	backends := []mockedBackend{
		{
			routes: []route{
				{
					path:     pathCommon,
					response: responseCommon0,
				},
				{
					path:     pathZero,
					response: responseZero,
				},
			},
		},
		{
			routes: []route{
				{
					path:     pathCommon,
					response: responseCommon1,
				},
				{
					path:     pathOne,
					response: responseOne,
				},
			},
		},
	}

	tests := map[string]struct {
		primaryBackendIdx int
		queries           []query
	}{
		"first backend primary": {
			primaryBackendIdx: 0,
			queries: []query{
				{
					path:               pathCommon,
					expectedRes:        responseCommon0,
					expectedStatusCode: 200,
				},
				{
					path:               pathZero,
					expectedRes:        responseZero,
					expectedStatusCode: 200,
				},
				{
					path:               pathOne,
					expectedRes:        "404 page not found\n",
					expectedStatusCode: 404,
				},
			},
		},
		"second backend primary": {
			primaryBackendIdx: 1,
			queries: []query{
				{
					path:               pathCommon,
					expectedRes:        responseCommon1,
					expectedStatusCode: 200,
				},
				{
					path:               pathOne,
					expectedRes:        responseOne,
					expectedStatusCode: 200,
				},
				{
					path:               pathZero,
					expectedRes:        "404 page not found\n",
					expectedStatusCode: 404,
				},
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			backendURLs := []string{}

			// Start backend servers.
			for _, b := range backends {
				router := mux.NewRouter()
				for _, route := range b.routes {
					router.Handle(route.path, mockQueryResponse(route.path, 200, route.response))
				}
				s := httptest.NewServer(router)
				defer s.Close()

				backendURLs = append(backendURLs, s.URL)
			}

			// Start the proxy.
			cfg := newTestProxyConfig()
			cfg.BackendEndpoints = strings.Join(backendURLs, ",")
			cfg.BackendReadTimeout = time.Second
			cfg.PassThroughNonRegisteredRoutes = true
			cfg.PrimaryBackend.Hostname = strconv.Itoa(testData.primaryBackendIdx)

			p, err := NewProxy(cfg, log.NewNopLogger(), testReadRoutes, testWriteRoutes, nil, nil)
			require.NoError(t, err)
			require.NotNil(t, p)
			defer p.Stop() //nolint:errcheck

			require.NoError(t, p.Start())

			for _, query := range testData.queries {

				// Send a query request to the proxy.
				res, err := http.Get(fmt.Sprintf("http://%s%s", p.Endpoint(), query.path))
				require.NoError(t, err)

				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)

				assert.Equal(t, query.expectedStatusCode, res.StatusCode)
				assert.Equal(t, query.expectedRes, string(body))
			}
		})
	}
}

func mockQueryResponse(path string, status int, res string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ensure the path is the expected one.
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Send back the mocked response.
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(res))
		}
	}
}

// newTestProxyConfig returns the config with the default values of the flags. The proxy listens on
// a random port.
func newTestProxyConfig() ProxyConfig {
	var cfg ProxyConfig
	flagext.DefaultValues(&cfg)
	cfg.ServerServicePort = 0
	return cfg
}
