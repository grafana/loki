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
	"github.com/grafana/loki/v3/pkg/util/constants"
)

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
				Path:   constants.PathLokiQuery,
				Shards: nil,
			},
		},
		{
			name: "LokiSeriesRequest",
			request: &queryrange.LokiSeriesRequest{
				Match:   []string{`{app="test"}`},
				StartTs: time.Now().Add(-1 * time.Hour),
				EndTs:   time.Now(),
				Path:    constants.PathLokiSeries,
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
				constants.PathLokiLabel+"/app/values",
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
				case constants.PathLokiQuery:
					response = `{"status":"success","data":{"resultType":"streams","result":[]}}`
				case constants.PathLokiSeries:
					response = `{"status":"success","data":[]}`
				case constants.PathLokiLabel + "/app/values", constants.PathLokiLabels:
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

			preferredBackend, err := NewProxyBackend("preferred", backendURL, 5*time.Second, true, false)
			require.NoError(t, err)
			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return nil, nil
			})

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:         queryrange.DefaultCodec,
				FanOutHandler: mockFanOutHandler,
				V1Backend:     preferredBackend,
				RoutingMode:   RoutingModeV1Preferred,
				SplitStart:    time.Time{},
				SplitLag:      1 * time.Hour,
			}, log.NewNopLogger())
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test-tenant")
			httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, tt.request)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httpReq)

			require.Equal(t, http.StatusOK, recorder.Code)
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
		Codec:         queryrange.DefaultCodec,
		FanOutHandler: mockFanOutHandler,
		V1Backend:     nil, // nil preferred backend
		RoutingMode:   RoutingModeV1Preferred,
		SplitStart:    time.Time{},
		SplitLag:      0,
	}, log.NewNopLogger())
	require.NoError(t, err)

	lokiReq := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Step:    60000,
		Limit:   100,
		Path:    constants.PathLokiQueryRange,
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

// TestSplittingHandler_AlwaysSplitsInV2PreferredAndRaceMode tests that
// v2-preferred and race modes always split queries when splitLag > 0.
func TestSplittingHandler_AlwaysSplitsInV2PreferredAndRaceMode(t *testing.T) {
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

			preferredBackend, err := NewProxyBackend("preferred", backendURL, 5*time.Second, false, false)
			require.NoError(t, err)

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:         queryrange.DefaultCodec,
				FanOutHandler: mockFanOutHandler,
				V1Backend:     preferredBackend,
				RoutingMode:   tc.routingMode,
				SplitStart:    time.Time{},
				SplitLag:      time.Hour,
			}, log.NewNopLogger())
			require.NoError(t, err)

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
				Path:    constants.PathLokiQueryRange,
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
			require.True(t, defaultHandlerCalled, "%s should call handler", tc.name)
			require.True(t, fanOutHandlerCalled, "fanout handler should be called for post-lag data")
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

	preferredBackend, err := NewProxyBackend("preferred", backendURL, 5*time.Second, true, false)
	require.NoError(t, err)

	handler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:         queryrange.DefaultCodec,
		FanOutHandler: mockFanOutHandler,
		V1Backend:     preferredBackend,
		RoutingMode:   RoutingModeRace,
		SplitStart:    time.Time{},
		SplitLag:      0, // No split lag - should use fanout directly
	}, log.NewNopLogger())
	require.NoError(t, err)

	lokiReq := &queryrange.LokiInstantRequest{
		Query:  `{app="test"}`,
		TimeTs: time.Now(),
		Limit:  100,
		Path:   constants.PathLokiQuery,
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

// TestSplittingHandler_MultiTenantQuery_RoutesToV1Only tests that multi-tenant queries
// are routed exclusively to v1, regardless of routing mode.
func TestSplittingHandler_MultiTenantQuery_RoutesToV1Only(t *testing.T) {
	for _, routingMode := range []RoutingMode{RoutingModeV1Preferred, RoutingModeV2Preferred, RoutingModeRace} {
		t.Run(string(routingMode), func(t *testing.T) {
			v1BackendCalled := false
			fanOutHandlerCalled := false
			var capturedTenantID string

			v1Backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				v1BackendCalled = true
				capturedTenantID = r.Header.Get("X-Scope-OrgID")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			}))
			defer v1Backend.Close()

			v1BackendURL, err := url.Parse(v1Backend.URL)
			require.NoError(t, err)

			v1ProxyBackend, err := NewProxyBackend("v1", v1BackendURL, 5*time.Second, true, false)
			require.NoError(t, err)

			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return &queryrange.LokiResponse{
					Status: "success",
					Data:   queryrange.LokiData{ResultType: "streams"},
				}, nil
			})

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:         queryrange.DefaultCodec,
				FanOutHandler: mockFanOutHandler,
				V1Backend:     v1ProxyBackend,
				RoutingMode:   routingMode,
				SplitStart:    time.Time{},
				SplitLag:      1 * time.Hour,
			}, log.NewNopLogger())
			require.NoError(t, err)

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
				Path:    constants.PathLokiQueryRange,
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			}

			// Inject multi-tenant org ID (pipe-separated)
			ctx := user.InjectOrgID(context.Background(), "tenant1|tenant2")
			httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httpReq)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.True(t, v1BackendCalled, "multi-tenant query should be routed to v1 backend in %s mode", routingMode)
			require.False(t, fanOutHandlerCalled, "multi-tenant query should NOT use fanout handler in %s mode", routingMode)
			require.Equal(t, "tenant1|tenant2", capturedTenantID, "tenant ID should be preserved when routing to v1")
		})
	}
}

func TestIsV2SupportedRequest(t *testing.T) {
	tests := []struct {
		name     string
		request  queryrangebase.Request
		expected bool
	}{
		{
			name: "LokiRequest is supported",
			request: &queryrange.LokiRequest{
				Query: `{app="test"}`,
				Path:  constants.PathLokiQueryRange,
			},
			expected: true,
		},
		{
			name: "LokiInstantRequest is supported",
			request: &queryrange.LokiInstantRequest{
				Query: `{app="test"}`,
				Path:  constants.PathLokiQuery,
			},
			expected: true,
		},
		{
			name: "LabelRequest is not supported",
			request: queryrange.NewLabelRequest(
				time.Now().Add(-1*time.Hour),
				time.Now(),
				"",
				"",
				constants.PathLokiLabels,
			),
			expected: false,
		},
		{
			name: "LokiSeriesRequest is not supported",
			request: &queryrange.LokiSeriesRequest{
				Match:   []string{`{app="test"}`},
				StartTs: time.Now().Add(-1 * time.Hour),
				EndTs:   time.Now(),
				Path:    constants.PathLokiSeries,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isV2SupportedRequest(tt.request)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestSplittingHandler_V2UnsupportedRoutes_RoutedToV1Only tests that metadata routes
// are routed directly to v1 when routing mode is v2-preferred or race with splitLag=0.
func TestSplittingHandler_V2UnsupportedRoutes_RoutedToV1Only(t *testing.T) {
	testCases := []struct {
		name        string
		routingMode RoutingMode
		request     queryrangebase.Request
	}{
		{
			name:        "v2-preferred labels request",
			routingMode: RoutingModeV2Preferred,
			request: queryrange.NewLabelRequest(
				time.Now().Add(-1*time.Hour),
				time.Now(),
				"",
				"",
				constants.PathLokiLabels,
			),
		},
		{
			name:        "v2-preferred series request",
			routingMode: RoutingModeV2Preferred,
			request: &queryrange.LokiSeriesRequest{
				Match:   []string{`{app="test"}`},
				StartTs: time.Now().Add(-1 * time.Hour),
				EndTs:   time.Now(),
				Path:    constants.PathLokiSeries,
			},
		},
		{
			name:        "race labels request",
			routingMode: RoutingModeRace,
			request: queryrange.NewLabelRequest(
				time.Now().Add(-1*time.Hour),
				time.Now(),
				"",
				"",
				constants.PathLokiLabels,
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v1HandlerCalled := false
			fanOutHandlerCalled := false

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				v1HandlerCalled = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
			}))
			defer backend.Close()

			backendURL, err := url.Parse(backend.URL)
			require.NoError(t, err)

			preferredBackend, err := NewProxyBackend("preferred", backendURL, 5*time.Second, true, false)
			require.NoError(t, err)

			mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				fanOutHandlerCalled = true
				return &queryrange.LokiResponse{
					Status: "success",
					Data:   queryrange.LokiData{ResultType: "streams"},
				}, nil
			})

			handler, err := NewSplittingHandler(SplittingHandlerConfig{
				Codec:         queryrange.DefaultCodec,
				FanOutHandler: mockFanOutHandler,
				V1Backend:     preferredBackend,
				RoutingMode:   tc.routingMode,
				SplitStart:    time.Time{},
				SplitLag:      0,
			}, log.NewNopLogger())
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test-tenant")
			httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, tc.request)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httpReq)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.True(t, v1HandlerCalled, "expected v1 handler to be called for unsupported request type")
			require.False(t, fanOutHandlerCalled, "fan-out handler should NOT be called for unsupported request type")
		})
	}
}

// TestSplittingHandler_V2SupportedRoutes_StillFanOut verifies that query/query_range
// routes continue to fan out.
func TestSplittingHandler_V2SupportedRoutes_StillFanOut(t *testing.T) {
	v1HandlerCalled := false
	fanOutHandlerCalled := false

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		v1HandlerCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)

	preferredBackend, err := NewProxyBackend("preferred", backendURL, 5*time.Second, true, false)
	require.NoError(t, err)

	mockFanOutHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		fanOutHandlerCalled = true
		return &queryrange.LokiResponse{
			Status: "success",
			Data:   queryrange.LokiData{ResultType: "streams"},
		}, nil
	})

	handler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:         queryrange.DefaultCodec,
		FanOutHandler: mockFanOutHandler,
		V1Backend:     preferredBackend,
		RoutingMode:   RoutingModeV2Preferred,
		SplitStart:    time.Time{},
		SplitLag:      0,
	}, log.NewNopLogger())
	require.NoError(t, err)

	lokiReq := &queryrange.LokiRequest{
		Query:   `{app="test"}`,
		StartTs: time.Now().Add(-1 * time.Hour),
		EndTs:   time.Now(),
		Step:    60000,
		Limit:   100,
		Path:    constants.PathLokiQueryRange,
	}

	ctx := user.InjectOrgID(context.Background(), "test-tenant")
	httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, lokiReq)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, fanOutHandlerCalled, "v2-supported request should still use fanout handler")
	require.False(t, v1HandlerCalled, "v2-supported request should NOT take v1-only early return")
}
