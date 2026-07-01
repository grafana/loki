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

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
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

func TestIsVolumeRequest(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/loki/api/v1/index/volume", true},
		{"/loki/api/v1/index/volume_range", true}, // matched by Contains; tripperware-level filter handles correctness
		{"/loki/api/v1/index/stats", false},
		{"/loki/api/v1/query_range", false},
		{"/loki/api/v1/labels", false},
		{"/", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.path, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, isVolumeRequest(req))
		})
	}
}

func TestModifyVolumeQuery(t *testing.T) {
	const tenant = "test_tenant"

	cases := []struct {
		name          string
		path          string
		orgID         string
		query         string
		policies      LabelPolicySet
		expectedQuery string // empty string means: assert query is unchanged
		expectedErr   bool
	}{
		{
			name:  "single policy is ANDed into existing selector",
			path:  "/loki/api/v1/index/volume",
			orgID: tenant,
			query: `{job="varlog"}`,
			policies: LabelPolicySet{
				tenant: []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
						{Type: types.LABEL_MATCHER_TYPE_NEQ, Name: "classification", Value: "secret"},
					}},
				},
			},
			expectedQuery: `{job="varlog", env="dev", classification!="secret"}`,
		},
		{
			name:  "regex matcher is preserved",
			path:  "/loki/api/v1/index/volume",
			orgID: tenant,
			query: `{job="varlog"}`,
			policies: LabelPolicySet{
				tenant: []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_RE, Name: "namespace", Value: "team-.*"},
					}},
				},
			},
			expectedQuery: `{job="varlog", namespace=~"team-.*"}`,
		},
		{
			name:  "multiple policies are NOT injected at the query level",
			path:  "/loki/api/v1/index/volume",
			orgID: tenant,
			query: `{job="varlog"}`,
			policies: LabelPolicySet{
				tenant: []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					}},
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "prod"},
					}},
				},
			},
			// query left untouched — response-level OR filter in the tripperware handles this case
			expectedQuery: "",
		},
		{
			name:  "missing X-Scope-OrgID is a no-op",
			path:  "/loki/api/v1/index/volume",
			orgID: "",
			query: `{job="varlog"}`,
			policies: LabelPolicySet{
				tenant: []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					}},
				},
			},
			expectedQuery: "",
		},
		{
			name:          "tenant has no policies — query untouched",
			path:          "/loki/api/v1/index/volume",
			orgID:         tenant,
			query:         `{job="varlog"}`,
			policies:      LabelPolicySet{tenant: []*types.LabelPolicy{}},
			expectedQuery: "",
		},
		{
			name:  "policy with empty selector — query untouched",
			path:  "/loki/api/v1/index/volume",
			orgID: tenant,
			query: `{job="varlog"}`,
			policies: LabelPolicySet{
				tenant: []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{}},
				},
			},
			expectedQuery: "",
		},
		{
			name:  "invalid existing query returns error",
			path:  "/loki/api/v1/index/volume",
			orgID: tenant,
			query: "not a valid logql selector",
			policies: LabelPolicySet{
				tenant: []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					}},
				},
			},
			expectedErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqURL, err := url.Parse(tc.path)
			require.NoError(t, err)
			if tc.query != "" {
				q := url.Values{}
				q.Set("query", tc.query)
				reqURL.RawQuery = q.Encode()
			}

			req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
			require.NoError(t, err)
			if tc.orgID != "" {
				req.Header.Set("X-Scope-OrgID", tc.orgID)
			}

			err = modifyVolumeQuery(req, tc.policies)
			if tc.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			got := req.URL.Query().Get("query")
			if tc.expectedQuery == "" {
				assert.Equal(t, tc.query, got, "query should be unchanged")
			} else {
				assert.Equal(t, tc.expectedQuery, got)
			}
		})
	}
}

// TestLabelAccessMiddlewareWithVolumeRequest pins the end-to-end behavior of
// the HTTP middleware for Volume requests: the X-Prom-Label-Policy header is
// extracted, the policy is folded into the query parameter as additional
// matchers, and the modified request is forwarded to the downstream handler.
// This is the regression scenario from #5180.
func TestLabelAccessMiddlewareWithVolumeRequest(t *testing.T) {
	var capturedRequest *http.Request
	testHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedRequest = r.Clone(context.Background())
	})

	mw := NewLabelAccessMiddleware(log.NewNopLogger())
	wrapped := mw.Wrap(testHandler)

	reqURL, err := url.Parse("/loki/api/v1/index/volume")
	require.NoError(t, err)
	q := url.Values{}
	q.Set("query", `{job="varlog"}`)
	reqURL.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodGet, reqURL.String(), bytes.NewReader(nil))
	req.Header.Set("X-Scope-OrgID", "test_tenant")
	req.Header.Add(HTTPHeaderKey, "test_tenant:"+url.PathEscape(`{env="dev"}`))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, capturedRequest)

	got := capturedRequest.URL.Query().Get("query")
	assert.Equal(t, `{job="varlog", env="dev"}`, got, "volume request query should be modified to include LBAC matchers")
}

// TestLabelAccessMiddlewareWithVolumeRequest_NonVolumePathUnchanged guards the
// Contains-based path check: stats requests share the /index/ prefix but must
// not be modified.
func TestLabelAccessMiddlewareWithVolumeRequest_NonVolumePathUnchanged(t *testing.T) {
	var capturedRequest *http.Request
	testHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedRequest = r.Clone(context.Background())
	})

	mw := NewLabelAccessMiddleware(log.NewNopLogger())
	wrapped := mw.Wrap(testHandler)

	reqURL, err := url.Parse("/loki/api/v1/index/stats")
	require.NoError(t, err)
	q := url.Values{}
	q.Set("query", `{job="varlog"}`)
	reqURL.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodGet, reqURL.String(), bytes.NewReader(nil))
	req.Header.Set("X-Scope-OrgID", "test_tenant")
	req.Header.Add(HTTPHeaderKey, "test_tenant:"+url.PathEscape(`{env="dev"}`))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	require.NotNil(t, capturedRequest)
	assert.Equal(t, `{job="varlog"}`, capturedRequest.URL.Query().Get("query"))
}
