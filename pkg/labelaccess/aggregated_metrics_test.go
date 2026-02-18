package labelaccess

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModifyAggregatedMetricsQuery(t *testing.T) {
	testCases := []struct {
		name             string
		query            string
		policies         []*types.LabelPolicy
		expectedModified bool
		expectedQuery    string
		expectedError    bool
	}{
		{
			name:  "non-aggregated metric query is not modified",
			query: `{job="varlog"} |= "line"`,
			policies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					},
				},
			},
			expectedModified: false,
			expectedQuery:    `{job="varlog"} |= "line"`,
		},
		{
			name:             "aggregated metric query with no policies is not modified",
			query:            `{__aggregated_metric__="varlog_service"}`,
			policies:         []*types.LabelPolicy{},
			expectedModified: false,
			expectedQuery:    `{__aggregated_metric__="varlog_service"}`,
		},
		{
			name:  "aggregated metric query with env=dev policy is modified correctly",
			query: `{__aggregated_metric__="varlog_service"}`,
			policies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					},
				},
			},
			expectedModified: true,
			expectedQuery:    `{__aggregated_metric__="varlog_service"} | logfmt | env="dev"`,
		},
		{
			name:  "aggregated metric query with multiple policies is modified correctly",
			query: `{__aggregated_metric__="varlog_service"}`,
			policies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
						{Type: types.LABEL_MATCHER_TYPE_NEQ, Name: "classification", Value: "secret"},
					},
				},
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_RE, Name: "classification", Value: "secre.*"},
					},
				},
			},
			expectedModified: true,
			expectedQuery:    `{__aggregated_metric__="varlog_service"} | logfmt | ( ( env="dev" , classification!="secret" ) or classification=~"secre.*" )`,
		},
		{
			name:  "aggregated metric query with existing filter gets lbac filter added before existing filter",
			query: `{__aggregated_metric__="varlog_service"} |= "some text"`,
			policies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					},
				},
			},
			expectedModified: true,
			expectedQuery:    `{__aggregated_metric__="varlog_service"} | logfmt | env="dev" |= "some text"`,
		},
		{
			name:  "aggregated metric metric query gets modified correctly",
			query: `sum by (job) (count_over_time({__aggregated_metric__="varlog_service"}[5m]))`,
			policies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					},
				},
			},
			expectedModified: true,
			expectedQuery:    `sum by (job)(count_over_time({__aggregated_metric__="varlog_service"} | logfmt | env="dev"[5m]))`,
		},
		{
			name: "complicated aggregated metrics queries with policies are rejected",
			query: `sum(
        sum by (job) (
          count_over_time({__aggregated_metric__="varlog_service"}[5m])
        ) / sum by (foo) (
          count_over_time({__aggregated_metric__="varlog_service"}[5m])
        )
      )`,
			policies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					},
				},
				{
					Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "prod"},
						{Type: types.LABEL_MATCHER_TYPE_NEQ, Name: "classification", Value: "secret"},
					},
				},
			},
			expectedError: true,
		},
		{
			name:  "complicated aggregated metrics query with empty policy label filter",
			query: "sum by (detected_level) (sum_over_time({__aggregated_metric__=`fantastic-signals-campaign-service` } | logfmt | unwrap count [10s]))",
			policies: []*types.LabelPolicy{
				func() *types.LabelPolicy {
					_, policy, err := policyFromHeaderValue("12345:{}")
					require.NoError(t, err)

					return policy
				}(),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a request with the query
			req, err := http.NewRequest("GET", "/loki/api/v1/query_range", nil)
			require.NoError(t, err)

			// Set the orgID header that the function will look for
			req.Header.Set("X-Scope-OrgID", "test_tenant")

			q := url.Values{}
			q.Add("query", tc.query)
			req.URL.RawQuery = q.Encode()

			// Convert policies to LabelPolicySet
			policies := LabelPolicySet{}
			policies["test_tenant"] = tc.policies

			// Call the function
			err = ModifyAggregatedMetricsQuery(req, policies)
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Check the result
				resultQuery := req.URL.Query().Get("query")
				if tc.expectedModified {
					assert.Equal(t, tc.expectedQuery, resultQuery)
				} else {
					assert.Equal(t, tc.query, resultQuery)
				}
			}
		})
	}
}

func TestIsAggregatedMetricQueryString(t *testing.T) {
	testCases := []struct {
		name           string
		query          string
		expectedResult bool
	}{
		{
			name:           "simple aggregated metric query",
			query:          `{__aggregated_metric__="varlog_service"}`,
			expectedResult: true,
		},
		{
			name:           "complex aggregated metric query",
			query:          `{__aggregated_metric__="varlog_service", job="metrics"}`,
			expectedResult: true,
		},
		{
			name:           "negated aggregated metric query",
			query:          `{__aggregated_metric__!="something_else", level="info"}`,
			expectedResult: true,
		},
		{
			name:           "regex aggregated metric query",
			query:          `{__aggregated_metric__=~"something_else"}`,
			expectedResult: true,
		},
		{
			name:           "negated regex aggregated metric query",
			query:          `{__aggregated_metric__!~"something_else", level="info"}`,
			expectedResult: true,
		},
		{
			name:           "regular log query",
			query:          `{job="varlog"}`,
			expectedResult: false,
		},
		{
			name:           "metric query",
			query:          `rate({job="varlog"}[5m])`,
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)
			result := isAggregatedMetricQueryString(expr)
			assert.Equal(t, tc.expectedResult, result)
		})
	}
}

func TestPolicyFilters(t *testing.T) {
	testCases := []struct {
		name                      string
		policy                    string
		expectedExtractResult     bool
		expectedBuildPolicyResult bool
	}{
		{
			name:                      "basic filter",
			policy:                    `test_tenant:{cluster="foo"}`,
			expectedExtractResult:     true,
			expectedBuildPolicyResult: true,
		},
		{
			name:                      "negated filter",
			policy:                    `test_tenant:{cluster!="foo"}`,
			expectedExtractResult:     true,
			expectedBuildPolicyResult: true,
		},
		{
			name:                      "regex filter",
			policy:                    `test_tenant:{cluster=~"(foo|bar)"}`,
			expectedExtractResult:     true,
			expectedBuildPolicyResult: true,
		},
		{
			name:                      "complex regex filter",
			policy:                    `test_tenant:{cluster=~"(foo|bar)[^f]ar.*?baz"}`,
			expectedExtractResult:     true,
			expectedBuildPolicyResult: true,
		},
		{
			name:                      "negated regex filter",
			policy:                    `test_tenant:{cluster!~"(foo|bar)"}`,
			expectedExtractResult:     true,
			expectedBuildPolicyResult: true,
		},
		{
			name:                      "empty label filter",
			policy:                    "test_tenant:{}",
			expectedExtractResult:     true,
			expectedBuildPolicyResult: false,
		},
		{
			name:                  "empty header value",
			policy:                "test_tenant:''",
			expectedExtractResult: false,
		},
		{
			name:                  "missing header value",
			policy:                "test_tenant:",
			expectedExtractResult: false,
		},
		{
			name:                  "multi-label filter",
			policy:                `test_tenant:{cluster="foo", namespace="bar"}`,
			expectedExtractResult: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Create a request with the requisite headers for extracting the policy
			req, err := http.NewRequest("GET", "/loki/api/v1/query_range", nil)
			require.NoError(t, err)

			req.Header.Set("X-Scope-OrgID", "test_tenant")
			req.Header.Set("X-Prom-Label-Policy", testCase.policy)

			matchers, err := ExtractLabelMatchersHTTP(req)

			// if we expect extraction of the header to fail and it does, great, keep going with the test cases
			if !testCase.expectedExtractResult {
				require.Error(t, err)
			} else {
				// otherwise, move on to test the policy build process
				require.NoError(t, err)

				policy, err := buildPolicyFilter(matchers["test_tenant"])

				if testCase.expectedBuildPolicyResult {
					require.NoError(t, err)
					require.NotNil(t, policy)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}
