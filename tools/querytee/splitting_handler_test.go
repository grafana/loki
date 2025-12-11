package querytee

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
)

// mockGoldfishManager is a mock implementation of goldfish.ManagerInterface for testing
type mockGoldfishManager struct {
	comparisonMinAge    time.Duration
	comparisonStartDate time.Time
	shouldSampleResult  bool
}

func (m *mockGoldfishManager) ComparisonMinAge() time.Duration {
	return m.comparisonMinAge
}

func (m *mockGoldfishManager) ComparisonStartDate() time.Time {
	return m.comparisonStartDate
}

func (m *mockGoldfishManager) ShouldSample(_ string) bool {
	return m.shouldSampleResult
}

func (m *mockGoldfishManager) SendToGoldfish(_ *http.Request, _, _ *goldfish.BackendResponse) {}

func (m *mockGoldfishManager) Close() error {
	return nil
}

func TestSplittingHandler_ServeSplits_UnsupportedRequestUsesDefaultHandler(t *testing.T) {
	tests := []struct {
		name    string
		request queryrangebase.Request
	}{
		{
			name: "LokiInstantRequest",
			request: &queryrange.LokiInstantRequest{
				Query:  `{app="test"}`,
				TimeTs: time.Now(),
				Limit:  100,
				Path:   "/loki/api/v1/query",
				Shards: nil,
			},
		},
		{
			name: "LokiSeriesRequest",
			request: &queryrange.LokiSeriesRequest{
				Match:   []string{`{app="test"}`},
				StartTs: time.Now().Add(-1 * time.Hour),
				EndTs:   time.Now(),
				Path:    "/loki/api/v1/series",
				Shards:  nil,
			},
		},
		{
			name: "LabelRequest",
			request: queryrange.NewLabelRequest(
				time.Now().Add(-1*time.Hour),
				time.Now(),
				"",
				"app",
				"/loki/api/v1/label/app/values",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedTenantID string
			defaultHandlerCalled := false
			fanOutHandlerCalled := false

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defaultHandlerCalled = true
				capturedTenantID = r.Header.Get(user.OrgIDHeaderName)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				var response string
				switch r.URL.Path {
				case "/loki/api/v1/query":
					// LokiInstantRequest response
					response = `{"status":"success","data":{"resultType":"streams","result":[]}}`
				case "/loki/api/v1/series":
					// LokiSeriesRequest response
					response = `{"status":"success","data":[]}`
				case "/loki/api/v1/label/app/values", "/loki/api/v1/labels":
					// LabelRequest response
					response = `{"status":"success","data":[]}`
				default:
					response = `{"status":"success"}`
				}

				_, err := w.Write([]byte(response))
				require.NoError(t, err)
			}))
			defer backend.Close()

			backendURL, err := url.Parse(backend.URL)
			require.NoError(t, err)

			preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, true)
			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return nil, nil
			})

			goldfishManager := &mockGoldfishManager{
				comparisonMinAge:    1 * time.Hour,
				comparisonStartDate: time.Time{},
			}

			handler, err := NewSplittingHandler(
				queryrange.DefaultCodec,
				mockFanOutHandler,
				goldfishManager,
				log.NewNopLogger(),
				preferredBackend,
			)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test-tenant")
			httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, tt.request)
			require.NoError(t, err)

			// Create a response recorder to capture the response
			recorder := httptest.NewRecorder()

			// Call ServeHTTP with the HTTP request
			handler.ServeHTTP(recorder, httpReq)

			// Verify the response was successful
			require.Equal(t, http.StatusOK, recorder.Code)

			// Verify that the default handler was called (not logs/metrics handlers)
			require.True(t, defaultHandlerCalled, "expected default handler to be called for unsupported request type")
			require.False(t, fanOutHandlerCalled, "fan-out handler was not called for unsupported request type")
			require.Equal(t, "test-tenant", capturedTenantID, "expected tenant ID to be passed to default handler")
		})
	}
}

func TestSplittingHandler_NilPreferredBackend_CallsFanoutHandler(t *testing.T) {
	var capturedTenantID string
	fanOutHandlerCalled := false

	mockFanOutHandler := queryrangebase.HandlerFunc(
		func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
			fanOutHandlerCalled = true
			tenantID, err := user.ExtractOrgID(ctx)
			if err == nil {
				capturedTenantID = tenantID
			}

			return &queryrange.LokiResponse{
				Status: "success",
				Data:   queryrange.LokiData{ResultType: "streams"},
			}, nil
		})

	handler, err := NewSplittingHandler(
		queryrange.DefaultCodec,
		mockFanOutHandler,
		nil, // no goldfish manager needed
		log.NewNopLogger(),
		nil, // nil preferred backend
	)
	require.NoError(t, err)

	lokiReq := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Step:    60000, // 1 minute in milliseconds
		Limit:   100,
		Path:    "/loki/api/v1/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, fanOutHandlerCalled, "expected fanout handler to be called when preferred backend is nil")
	require.Equal(t, "test-tenant", capturedTenantID, "expected tenant ID to be passed to fanout handler")
}
