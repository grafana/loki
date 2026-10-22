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
		cfg           ProxyConfig
		expectedErr   string
		expectedRoles []BackendRole
	}{
		"returns an error when there is no backend": {
			cfg:         ProxyConfig{},
			expectedErr: errMinBackends.Error(),
		},
		"returns an error when the primary hostname is not set": {
			cfg:         ProxyConfig{BackendEndpoints: "http://a:80,http://b:80"},
			expectedErr: "-primary-backend.hostname flag must be set",
		},
		"returns an error when no backend has the primary hostname": {
			cfg: ProxyConfig{
				BackendEndpoints: "http://a:80,http://b:80",
				PrimaryBackend:   PrimaryBackendConfig{Hostname: "c"},
			},
			expectedErr: "the primary backend (hostname) has not been found among the list of configured backends",
		},
		"returns an error when more than one backend has the primary hostname": {
			cfg: ProxyConfig{
				BackendEndpoints: "http://a:80,http://a:81",
				PrimaryBackend:   PrimaryBackendConfig{Hostname: "a"},
			},
			expectedErr: "more than one configured backend has the hostname of the primary backend",
		},
		"returns an error when no backend has the secondary hostname": {
			cfg: ProxyConfig{
				BackendEndpoints: "http://a:80,http://b:80",
				PrimaryBackend:   PrimaryBackendConfig{Hostname: "a"},
				SecondaryBackend: SecondaryBackendConfig{Hostname: "c"},
			},
			expectedErr: "the secondary backend (hostname) has not been found among the list of configured backends",
		},
		"returns an error when the comparison is enabled without a secondary backend": {
			cfg: ProxyConfig{
				BackendEndpoints: "http://a:80,http://b:80",
				PrimaryBackend:   PrimaryBackendConfig{Hostname: "a"},
				CompareResponses: true,
			},
			expectedErr: "when enabling comparison of results -secondary-backend.hostname flag must be set",
		},
		"assigns the roles by hostname": {
			cfg: ProxyConfig{
				BackendEndpoints: "http://b:80,http://c:80,http://a:80",
				PrimaryBackend:   PrimaryBackendConfig{Hostname: "a"},
				SecondaryBackend: SecondaryBackendConfig{Hostname: "b"},
			},
			expectedRoles: []BackendRole{RoleSecondary, RoleOther, RolePrimary},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			p, err := NewProxy(testData.cfg, log.NewNopLogger(), testReadRoutes, testWriteRoutes, nil)

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
			cfg := ProxyConfig{
				BackendEndpoints:   strings.Join(backendURLs, ","),
				ServerServicePort:  0,
				BackendReadTimeout: time.Second,
				PrimaryBackend:     PrimaryBackendConfig{Hostname: strconv.Itoa(testData.primaryBackendIdx)},
			}

			if len(backendURLs) == 2 {
				cfg.CompareResponses = true
				cfg.SecondaryBackend.Hostname = strconv.Itoa(1 - testData.primaryBackendIdx)
			}

			p, err := NewProxy(cfg, log.NewNopLogger(), testReadRoutes, testWriteRoutes, nil)
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

	p, err := NewProxy(ProxyConfig{
		BackendEndpoints:   strings.Join(backendURLs, ","),
		BackendReadTimeout: time.Second,
		CompareResponses:   true,
		PrimaryBackend:     PrimaryBackendConfig{Hostname: strconv.Itoa(primaryIdx)},
		SecondaryBackend:   SecondaryBackendConfig{Hostname: strconv.Itoa(secondaryIdx)},
	}, log.NewNopLogger(), []Route{{Path: constants.PathLokiQuery, RouteName: "api_v1_query", Methods: []string{"GET"}, ResponseComparator: comp}}, []Route{{Path: constants.PathLokiPush, RouteName: "api_v1_push", Methods: []string{"POST"}}}, nil)
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
			cfg := ProxyConfig{
				BackendEndpoints:               strings.Join(backendURLs, ","),
				ServerServicePort:              0,
				BackendReadTimeout:             time.Second,
				PassThroughNonRegisteredRoutes: true,
				PrimaryBackend:                 PrimaryBackendConfig{Hostname: strconv.Itoa(testData.primaryBackendIdx)},
			}

			p, err := NewProxy(cfg, log.NewNopLogger(), testReadRoutes, testWriteRoutes, nil)
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
