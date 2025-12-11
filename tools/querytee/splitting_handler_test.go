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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
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
			// Track whether the default handler was called
			defaultHandlerCalled := false
			fanOutHandlerCalled := false

			// Create a mock backend server for the preferred backend
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defaultHandlerCalled = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				// Return appropriate response based on request path
				var response string
				switch {
				case r.URL.Path == "/loki/api/v1/query":
					// LokiInstantRequest response
					response = `{"status":"success","data":{"resultType":"streams","result":[]}}`
				case r.URL.Path == "/loki/api/v1/series":
					// LokiSeriesRequest response
					response = `{"status":"success","data":[]}`
				case r.URL.Path == "/loki/api/v1/label/app/values" || r.URL.Path == "/loki/api/v1/labels":
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
			mockFanOutHandler := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				defaultHandlerCalled = true
				return nil, nil
			})

			//TODO(twhitney): Change NewSplittingHandler to accept an interface rather than concrete type, then use a mock here
			goldfishConfig := goldfish.Config{
				ComparisonMinAge: 1 * time.Hour,
			}
			goldfishManager, err := goldfish.NewManager(
				goldfishConfig,
				nil, // comparator not needed for this test
				nil, // storage not needed for this test
				nil, // resultStore not needed for this test
				log.NewNopLogger(),
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)

			handler, err := NewSplittingHandler(
				queryrange.DefaultCodec,
				mockFanOutHandler,
				goldfishManager,
				log.NewNopLogger(),
				preferredBackend,
			)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test-tenant")

			// Call serveSplits with the unsupported request type
			resp, err := handler.serveSplits(ctx, tt.request)
			require.NoError(t, err)
			require.NotNil(t, resp)

			// Verify that the default handler was called (not logs/metrics handlers)
			require.True(t, defaultHandlerCalled, "expected default handler to be called for unsupported request type")
			require.False(t, fanOutHandlerCalled, "fan-out handler was not called for unsupported request type")
		})
	}
}
