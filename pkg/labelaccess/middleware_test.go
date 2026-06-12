package labelaccess

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabelAccessMiddlewareWithAggregatedMetrics(t *testing.T) {
	// Create a test HTTP handler that captures the request
	var capturedRequest *http.Request
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r.Clone(context.Background())
		w.WriteHeader(http.StatusOK)
	})

	// Create middleware with a logger
	middleware := NewLabelAccessMiddleware(log.NewNopLogger())
	wrappedHandler := middleware.Wrap(testHandler)

	// Create a test request with an aggregated metrics query
	reqURL, err := url.Parse("/loki/api/v1/query_range")
	require.NoError(t, err)

	q := url.Values{}
	q.Add("query", `{__aggregated_metric__="varlog_service"}`)
	reqURL.RawQuery = q.Encode()

	req := httptest.NewRequest("GET", reqURL.String(), bytes.NewReader([]byte{}))
	req.Header.Set("X-Scope-OrgID", "test_tenant")

	// Add LBAC policy headers - each policy needs its own header
	req.Header.Add(HTTPHeaderKey, "test_tenant:"+url.PathEscape(
		`{env="dev",classification!="secret"}`,
	))
	req.Header.Add(HTTPHeaderKey, "test_tenant:"+url.PathEscape(
		`{classification=~"secre.*"}`,
	))

	// Create a response recorder
	recorder := httptest.NewRecorder()

	// Call the wrapped handler
	wrappedHandler.ServeHTTP(recorder, req)

	// Verify the response
	assert.Equal(t, http.StatusOK, recorder.Code)

	// Verify that the request was modified
	require.NotNil(t, capturedRequest, "The request was not captured")

	// Check that the query was modified with LBAC filters
	modifiedQuery := capturedRequest.URL.Query().Get("query")
	require.NotEqual(t, `{__aggregated_metric__="varlog_service"}`, modifiedQuery, "Query was not modified")

	// Verify the expected filters were applied (this matches what we expect from the test in aggregated_metrics_test.go)
	expectedQuery := `{__aggregated_metric__="varlog_service"} | logfmt | ( ( env="dev" , classification!="secret" ) or classification=~"secre.*" )`
	assert.Equal(t, expectedQuery, modifiedQuery, "Query was not modified correctly")
}
