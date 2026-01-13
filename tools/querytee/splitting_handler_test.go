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

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
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

			preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, true, false, false)
			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return nil, nil
			})

			goldfishManager := &mockGoldfishManager{
				comparisonMinAge:    1 * time.Hour,
				comparisonStartDate: time.Time{},
			}

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:                     queryrange.DefaultCodec,
				FanOutHandler:             mockFanOutHandler,
				GoldfishManager:           goldfishManager,
				PreferredBackend:          preferredBackend,
				SkipFanoutWhenNotSampling: false,
				RoutingMode:               RoutingModeV1Preferred,
				SplitStart:                time.Time{},
				SplitLag:                  1 * time.Hour,
			}, log.NewNopLogger())
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

	handler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:                     queryrange.DefaultCodec,
		FanOutHandler:             mockFanOutHandler,
		GoldfishManager:           nil,
		PreferredBackend:          nil, // nil preferred backend
		SkipFanoutWhenNotSampling: false,
		RoutingMode:               RoutingModeV1Preferred,
		SplitStart:                time.Time{},
		SplitLag:                  0,
	}, log.NewNopLogger())
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

// TestSplittingHandler_RoutingModeV1Preferred_SkipsToDefaultWhenNotSampling tests that
// v1-preferred mode skips fanout and goes directly to the default handler when not sampling.
func TestSplittingHandler_RoutingModeV1Preferred_SkipsToDefaultWhenNotSampling(t *testing.T) {
	defaultHandlerCalled := false
	fanOutHandlerCalled := false

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defaultHandlerCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)

	preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, false, true, false)
	mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		fanOutHandlerCalled = true
		return &queryrange.LokiResponse{
			Status: "success",
			Data:   queryrange.LokiData{ResultType: "streams"},
		}, nil
	})

	goldfishManager := &mockGoldfishManager{
		comparisonMinAge:   1 * time.Hour,
		shouldSampleResult: false, // NOT sampling
	}

	handler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:                     queryrange.DefaultCodec,
		FanOutHandler:             mockFanOutHandler,
		GoldfishManager:           goldfishManager,
		PreferredBackend:          preferredBackend,
		SkipFanoutWhenNotSampling: true, // Enable skip when not sampling
		RoutingMode:               RoutingModeV1Preferred,
		SplitStart:                time.Time{},
		SplitLag:                  1 * time.Hour,
	}, log.NewNopLogger())
	require.NoError(t, err)

	// Use a LokiRequest with a v2-engine-supported query
	now := time.Now()
	query := `sum(rate({app="test"}[5m]))`
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)

	lokiReq := &queryrange.LokiRequest{
		Query:   query,
		StartTs: now.Add(-2 * time.Hour),
		EndTs:   now,
		Step:    60000,
		Limit:   100,
		Path:    "/loki/api/v1/query_range",
		Plan: &plan.QueryPlan{
			AST: expr,
		},
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, defaultHandlerCalled, "v1-preferred mode should skip to default handler when not sampling")
	require.False(t, fanOutHandlerCalled, "v1-preferred mode should NOT call fanout handler when not sampling")
}

// TestSplittingHandler_AlwaysSplitsEvenWhenNotSampling tests that
// v2-preferred and race modes always split queries when splitLag > 0, regardless of sampling.
func TestSplittingHandler_AlwaysSplitsEvenWhenNotSampling(t *testing.T) {
	testCases := []struct {
		name        string
		routingMode RoutingMode
	}{
		{
			name:        "v2-preferred mode",
			routingMode: RoutingModeV2Preferred,
		},
		{
			name:        "race mode",
			routingMode: RoutingModeRace,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defaultHandlerCalled := false
			fanOutHandlerCalled := false

			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return &queryrange.LokiResponse{
					Status: "success",
					Data:   queryrange.LokiData{ResultType: "streams"},
				}, nil
			})

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				defaultHandlerCalled = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			}))
			defer backend.Close()

			backendURL, err := url.Parse(backend.URL)
			require.NoError(t, err)

			preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, false, false, false)

			goldfishManager := &mockGoldfishManager{
				comparisonMinAge:   1 * time.Hour,
				shouldSampleResult: false, // NOT sampling
			}

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:                     queryrange.DefaultCodec,
				FanOutHandler:             mockFanOutHandler,
				GoldfishManager:           goldfishManager,
				PreferredBackend:          preferredBackend,
				SkipFanoutWhenNotSampling: true, // Enable skip when not sampling, which should not apply in these 2 modes
				RoutingMode:               tc.routingMode,
				SplitStart:                time.Time{},
				SplitLag:                  time.Hour,
			}, log.NewNopLogger())
			require.NoError(t, err)

			now := time.Now()
			query := `sum(rate({app="test"}[5m]))`
			expr, err := syntax.ParseExpr(query)
			require.NoError(t, err)

			lokiReq := &queryrange.LokiRequest{
				Query:   query,
				StartTs: now.Add(-2 * time.Hour), // Start before the lag window
				EndTs:   now,                     // End at now (within lag window)
				Step:    60000,                   // 1 minute step in milliseconds (required for metric queries)
				Limit:   100,
				Path:    "/loki/api/v1/query_range",
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			}

			ctx := user.InjectOrgID(context.Background(), "test-tenant")
			httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httpReq)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.True(t, defaultHandlerCalled, "%s should call handler even when not sampling", tc.name)
			require.True(t, fanOutHandlerCalled, "fanout handler should be called for post-lag data even when not sampling")
		})
	}
}

// TestSplittingHandler_NoSplitLag_UsesFanoutHandler tests that when splitLag is 0
// the handler uses the fanout handler directly.
func TestSplittingHandler_NoSplitLag_UsesFanoutHandler(t *testing.T) {
	defaultHandlerCalled := false
	fanOutHandlerCalled := false

	mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		fanOutHandlerCalled = true
		return &queryrange.LokiResponse{
			Status: "success",
			Data:   queryrange.LokiData{ResultType: "streams"},
		}, nil
	})

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defaultHandlerCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)

	preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, false, true, false)

	goldfishManager := &mockGoldfishManager{
		comparisonMinAge:   0,
		shouldSampleResult: true, // sampling enabled
	}

	handler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:                     queryrange.DefaultCodec,
		FanOutHandler:             mockFanOutHandler,
		GoldfishManager:           goldfishManager,
		PreferredBackend:          preferredBackend,
		SkipFanoutWhenNotSampling: false,
		RoutingMode:               RoutingModeRace,
		SplitStart:                time.Time{},
		SplitLag:                  0, // No split lag - should use fanout directly
	}, log.NewNopLogger())
	require.NoError(t, err)

	lokiReq := &queryrange.LokiInstantRequest{
		Query:  `{app="test"}`,
		TimeTs: time.Now(),
		Limit:  100,
		Path:   "/loki/api/v1/query",
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, fanOutHandlerCalled, "when splitLag is 0, should use fanout handler directly")
	require.False(t, defaultHandlerCalled, "when splitLag is 0, should NOT use default handler")
}

// TestSplittingHandler_V1Preferred_SplitsWhenSampling tests that v1-preferred mode
// does split queries when sampling is enabled.
func TestSplittingHandler_V1Preferred_SplitsWhenSampling(t *testing.T) {
	defaultHandlerCalled := false
	fanOutHandlerCalled := false

	mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		fanOutHandlerCalled = true
		return &queryrange.LokiResponse{
			Status: "success",
			Data:   queryrange.LokiData{ResultType: "streams"},
		}, nil
	})

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defaultHandlerCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)

	preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, false, true, false)

	goldfishManager := &mockGoldfishManager{
		comparisonMinAge:   1 * time.Hour,
		shouldSampleResult: true, // IS sampling
	}

	handler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:                     queryrange.DefaultCodec,
		FanOutHandler:             mockFanOutHandler,
		GoldfishManager:           goldfishManager,
		PreferredBackend:          preferredBackend,
		SkipFanoutWhenNotSampling: true,
		RoutingMode:               RoutingModeV1Preferred,
		SplitStart:                time.Time{},
		SplitLag:                  1 * time.Hour,
	}, log.NewNopLogger())
	require.NoError(t, err)

	// Use a LokiRequest with a v2-engine-supported query so it goes through the splitting logic
	// and calls both the default handler (for pre-lag data) and fanout handler (for post-lag data)
	now := time.Now()
	query := `sum(rate({app="test"}[5m]))`
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)

	lokiReq := &queryrange.LokiRequest{
		Query:   query,
		StartTs: now.Add(-2 * time.Hour),
		EndTs:   now,
		Step:    60000,
		Limit:   100,
		Path:    "/loki/api/v1/query_range",
		Plan: &plan.QueryPlan{
			AST: expr,
		},
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, defaultHandlerCalled, "v1-preferred mode should split when sampling is enabled")
	require.True(t, fanOutHandlerCalled, "fanout handler should be called for post-lag data when sampling")
}

// TestSplittingHandler_SkipFanoutDisabled_AlwaysSplits tests that when
// SkipFanoutWhenNotSampling is false, all modes split regardless of sampling.
func TestSplittingHandler_SkipFanoutDisabled_AlwaysSplits(t *testing.T) {
	for _, routingMode := range []RoutingMode{RoutingModeV1Preferred, RoutingModeV2Preferred, RoutingModeRace} {
		t.Run(string(routingMode), func(t *testing.T) {
			defaultHandlerCalled := false
			fanOutHandlerCalled := false

			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return &queryrange.LokiResponse{
					Status: "success",
					Data:   queryrange.LokiData{ResultType: "streams"},
				}, nil
			})

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				defaultHandlerCalled = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			}))
			defer backend.Close()

			backendURL, err := url.Parse(backend.URL)
			require.NoError(t, err)

			preferredBackend := NewProxyBackend("preferred", backendURL, 5*time.Second, false, true, false)

			goldfishManager := &mockGoldfishManager{
				comparisonMinAge:   1 * time.Hour,
				shouldSampleResult: false, // NOT sampling
			}

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:                     queryrange.DefaultCodec,
				FanOutHandler:             mockFanOutHandler,
				GoldfishManager:           goldfishManager,
				PreferredBackend:          preferredBackend,
				SkipFanoutWhenNotSampling: false, // Disabled - should always split
				RoutingMode:               routingMode,
				SplitStart:                time.Time{},
				SplitLag:                  1 * time.Hour,
			}, log.NewNopLogger())
			require.NoError(t, err)

			// Use a LokiRequest with a v2-engine-supported query
			now := time.Now()
			query := `sum(rate({app="test"}[5m]))`
			expr, err := syntax.ParseExpr(query)
			require.NoError(t, err)

			lokiReq := &queryrange.LokiRequest{
				Query:   query,
				StartTs: now.Add(-2 * time.Hour),
				EndTs:   now,
				Step:    60000,
				Limit:   100,
				Path:    "/loki/api/v1/query_range",
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			}

			ctx := user.InjectOrgID(context.Background(), "test-tenant")
			httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httpReq)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.True(t, defaultHandlerCalled)
			require.True(t, fanOutHandlerCalled)
		})
	}
}
