//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/labelaccess"
	"github.com/grafana/loki/v3/pkg/labelaccess/types"
)

const (
	logsQuery           = `{job="varlog"} |= "line"`
	metricsQuery        = "count_over_time(" + logsQuery + " [2h])"
	aggregatedLogsQuery = `{__aggregated_metric__="varlog_service"} | logfmt`
)

func buildLBACPolicyHeaders(tenantID string, policies []types.LabelPolicy) []string {
	headers := make([]string, len(policies))
	for i := range policies {
		matchers := make([]string, len(policies[i].Selector))
		for j, selector := range policies[i].Selector {
			matcher, err := types.LabelMatcherToPromLabel(selector)
			if err != nil {
				panic(err)
			}
			matchers[j] = matcher.String()
		}
		headers[i] = fmt.Sprintf("%s:%s", tenantID, url.PathEscape("{"+strings.Join(matchers, ", ")+"}"))
	}
	return headers
}

var (
	testCases = []*testQueryAndLabelResults{
		{
			name:          "no matches",
			createCluster: clusterAllLBAC,
			tenantID:      randStringRunes(),
			now:           time.Now(),
			labelPolicies: []types.LabelPolicy{labelaccess.PolicyFromSelectorString(`{not="existing"}`)},
		},
		{
			name:          "disabled",
			createCluster: clusterAll,
			tenantID:      randStringRunes(),
			now:           time.Now(),
			labelPolicies: []types.LabelPolicy{
				labelaccess.PolicyFromSelectorString(`{env="dev"}`),
			},
			labelValues: map[string][]string{
				"classification": {"confidential", "secret"},
				"env":            {"dev", "prod"},
				"job":            {"varlog"},
			},
			lines: []string{"line1", "line2", "line3", "line4"},
			streams: []map[string]string{
				{
					"classification": "confidential",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "prod",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"job": "varlog",
				},
			},
		},
		{
			name:          "lbac enabled",
			createCluster: clusterAllLBAC,
			tenantID:      randStringRunes(),
			now:           time.Now(),
			labelPolicies: []types.LabelPolicy{
				labelaccess.PolicyFromSelectorString(`{env="dev"}`),
			},
			labelValues: map[string][]string{
				"classification": {"confidential", "secret"},
				"env":            {"dev"},
				"job":            {"varlog"},
			},
			lines: []string{"line1", "line3"},
			streams: []map[string]string{
				{
					"classification": "confidential",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "dev",
					"job":            "varlog",
				},
			},
		},
		{
			name:          "multiple policies",
			createCluster: clusterAllLBAC,
			tenantID:      randStringRunes(),
			now:           time.Now(),
			labelPolicies: []types.LabelPolicy{
				labelaccess.PolicyFromSelectorString(`{env="dev", classification!="secret"}`),
				labelaccess.PolicyFromSelectorString(`{classification=~"secre.*"}`),
			},
			labelValues: map[string][]string{
				"env":            {"dev", "prod"},
				"classification": {"secret", "confidential"},
				"job":            {"varlog"},
			},
			lines: []string{"line1", "line3", "line4"},
			streams: []map[string]string{
				{
					"classification": "confidential",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "prod",
					"job":            "varlog",
				},
			},
		},
	}
)

func clusterAll(t *testing.T) *cluster.Component {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	t.Cleanup(func() {
		assert.NoError(t, clu.Cleanup())
	})

	tAll := clu.AddComponent("all", "-target=all")
	require.NoError(t, clu.Run())

	return tAll
}

func clusterAllLBAC(t *testing.T) *cluster.Component {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	t.Cleanup(func() {
		assert.NoError(t, clu.Cleanup())
	})

	tAll := clu.AddComponent("all", "-target=all", "-lbac.enabled=true")
	require.NoError(t, clu.Run())

	return tAll
}

func clusterAllLBACAggregation(t *testing.T) *cluster.Component {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	t.Cleanup(func() {
		assert.NoError(t, clu.Cleanup())
	})

	tAll := clu.AddComponent("all", "-target=all", "-lbac.enabled=true", "-limits.aggregation-enabled=true", "-pattern-ingester.enabled=true")
	require.NoError(t, clu.Run())

	return tAll
}

// TestLabelAccessDefault starts a Loki cluster and verifies
// the query results are as expected as per the testCase configuration
func TestLabelAccessTestCases(t *testing.T) {
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.createCluster == nil {
				panic("test case is missing createCluster function")
			}
			tAll := testCase.createCluster(t)
			testCase.ingest(t, tAll)

			cliQuery := testCase.cliQuery(t, tAll)

			testCase.logsQuery(t, cliQuery)
			testCase.labelValuesQuery(t, cliQuery)
			testCase.seriesQuery(t, cliQuery)
			testCase.metricsRangeQuery(t, cliQuery)
			testCase.metricsQuery(t, cliQuery)
		})
	}
}

func TestAggregatedMetricsTestCases(t *testing.T) {
	var aggregatedMetricsTestCases = []*testQueryAndLabelResults{
		{
			name:          "aggregated metrics",
			createCluster: clusterAllLBACAggregation,
			tenantID:      randStringRunes(),
			now:           time.Now(),
			labelPolicies: []types.LabelPolicy{
				labelaccess.PolicyFromSelectorString(`{env!="dev", classification!="secret"}`),
				labelaccess.PolicyFromSelectorString(`{classification!="secret"}`),
			},
			labelValues: map[string][]string{
				"classification": {"confidential", "secret"},
				"env":            {"dev", "prod"},
				"job":            {"varlog"},
			},
			lines: []string{`ts=1 bytes=1024 count=10 job="varlog" service_name="varlog_service" env="dev" classification="confidential"`,
				`ts=4 bytes=8192 count=40 job="varlog" service_name="varlog_service" env="prod" classification="notsecret"`},
			streams: []map[string]string{
				{
					"classification": "confidential",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "prod",
					"job":            "varlog",
				},
				{
					"classification": "secret",
					"env":            "dev",
					"job":            "varlog",
				},
				{
					"job": "varlog",
				},
			},
		},
	}
	t.Skip("This test is not complete")
	for _, testCase := range aggregatedMetricsTestCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.createCluster == nil {
				panic("test case is missing createCluster function")
			}
			tAll := testCase.createCluster(t)
			testCase.ingestAggregatedMetrics(t, tAll)

			cliQuery := testCase.cliQuery(t, tAll)

			testCase.aggregatedMetricsQuery(t, cliQuery)
			testCase.labelValuesQuery(t, cliQuery)
			//testCase.seriesQuery(t, cliQuery)
			//testCase.metricsRangeQuery(t, cliQuery)
			//testCase.metricsQuery(t, cliQuery)
		})
	}
}

//
//	// Query aggregated metrics

//}

type testQueryAndLabelResults struct {
	name          string
	createCluster func(t *testing.T) *cluster.Component
	tenantID      string
	now           time.Time
	labelPolicies []types.LabelPolicy
	labelValues   map[string][]string // expected label values that match the label policy
	lines         []string            // expected lines that match the label policy
	streams       []map[string]string // expected streams
}

func (tc *testQueryAndLabelResults) ingest(t *testing.T, tAll *cluster.Component) {
	cliWrite := client.New(tc.tenantID, "", tAll.HTTPURL())
	cliWrite.Now = tc.now

	require.NoError(t, cliWrite.PushLogLine("line1", tc.now, nil, map[string]string{"env": "dev", "classification": "confidential"}))
	require.NoError(t, cliWrite.PushLogLine("line2", tc.now.Add(time.Second), nil))
	require.NoError(t, cliWrite.PushLogLine("line3", tc.now.Add(2*time.Second), nil, map[string]string{"classification": "secret", "env": "dev"}))
	require.NoError(t, cliWrite.PushLogLine("line4", tc.now.Add(3*time.Second), nil, map[string]string{"classification": "secret", "env": "prod"}))
	require.NoError(t, cliWrite.Flush())
}

func (tc *testQueryAndLabelResults) ingestAggregatedMetrics(t *testing.T, tAll *cluster.Component) {
	cliWrite := client.New(tc.tenantID, "", tAll.HTTPURL())
	cliWrite.Now = tc.now

	aggMetricLabels := map[string]string{"__aggregated_metric__": "varlog_service"}

	// Aggregated metrics for dev environment with confidential classification
	require.NoError(t, cliWrite.PushLogLine(
		`ts=1 bytes=1024 count=10 job="varlog" service_name="varlog_service" env="dev" classification="confidential"`,
		tc.now,
		aggMetricLabels,
	))

	// Aggregated metrics for dev environment with secret classification
	require.NoError(t, cliWrite.PushLogLine(
		`ts=2 bytes=2048 count=20 job="varlog" service_name="varlog_service" env="dev" classification="secret"`,
		tc.now.Add(-time.Second),
		aggMetricLabels,
	))

	// Aggregated metrics for prod environment with secret classification
	require.NoError(t, cliWrite.PushLogLine(
		`ts=3 bytes=4096 count=30 job="varlog" service_name="varlog_service" env="prod" classification="secret"`,
		tc.now.Add(-2*time.Second),
		aggMetricLabels,
	))

	// Aggregated metrics for prod environment with notsecret classification
	require.NoError(t, cliWrite.PushLogLine(
		`ts=4 bytes=8192 count=40 job="varlog" service_name="varlog_service" env="prod" classification="notsecret"`,
		tc.now.Add(-3*time.Second),
		aggMetricLabels,
	))

	require.NoError(t, cliWrite.Flush())
}

func (tc *testQueryAndLabelResults) aggregatedMetricsQuery(t *testing.T, qc *client.Client) {
	t.Run("aggregated-metrics-query", func(t *testing.T) {

		// query the available lines
		output, err := qc.RunRangeQuery(context.Background(), aggregatedLogsQuery)
		require.NoError(t, err)
		assert.Equal(t, "success", output.Status)
		assert.Equal(t, "streams", output.Data.ResultType)

		var actualLines, streams []string
		for _, row := range output.Data.Stream {
			streams = append(streams, labels.FromMap(row.Stream).String())
			for _, lines := range row.Values {
				actualLines = append(actualLines, lines[1])
			}
		}
		assert.ElementsMatch(t, tc.lines, actualLines)
		//assert.ElementsMatch(t, tc.streams, streams)
	})

	//	require.Len(t, output.Data.Stream, 2)
	//	require.Equal(t, "varlog_service", output.Data.Stream[0].Stream["__aggregated_metric__"])
	//
	//	expected := []string{
	//		`ts=1 bytes=1024 count=10 job="varlog" service_name="varlog_service" env="dev" classification="confidential"`,
	//		`ts=4 bytes=8192 count=40 job="varlog" service_name="varlog_service" env="prod" classification="notsecret"`,
	//	}
	//
	//	actual := []string{}
	//	for _, stream := range output.Data.Stream {
	//		for _, v := range stream.Values {
	//			actual = append(actual, v[1])
	//		}
	//	}
	//
	//	require.ElementsMatch(t, expected, actual)
}

func (tc *testQueryAndLabelResults) cliQuery(t *testing.T, tAll *cluster.Component) *client.Client {
	headers := buildLBACPolicyHeaders(tc.tenantID, tc.labelPolicies)

	cliQuery := client.New(
		tc.tenantID,
		"",
		tAll.HTTPURL(),
		client.InjectHeadersOption{
			labelaccess.HTTPHeaderKey: headers,
		},
	)
	cliQuery.Now = tc.now.Add(4 * time.Second)
	return cliQuery
}

func (tc *testQueryAndLabelResults) labelValuesQuery(t *testing.T, qc *client.Client) {
	t.Run("label-values-query", func(t *testing.T) {
		labelMap := make(map[string][]string)
		labelNames, err := qc.LabelNames(context.Background())
		require.NoError(t, err)
		for _, label := range labelNames {
			labelValues, err := qc.LabelValues(context.Background(), label)
			require.NoError(t, err)
			labelMap[label] = labelValues
		}
		require.NoError(t, err)

		require.Equal(t, normalizeLabelsData(tc.labelValues), normalizeLabelsData(labelMap))
	})
}
func (tc *testQueryAndLabelResults) logsQuery(t *testing.T, qc *client.Client) {
	t.Run("logs-query", func(t *testing.T) {

		// query the available lines
		output, err := qc.RunRangeQuery(context.Background(), logsQuery)
		require.NoError(t, err)
		assert.Equal(t, "success", output.Status)
		assert.Equal(t, "streams", output.Data.ResultType)

		var actualLines, streams []string
		for _, row := range output.Data.Stream {
			streams = append(streams, labels.FromMap(row.Stream).String())
			for _, lines := range row.Values {
				actualLines = append(actualLines, lines[1])
			}
		}
		assert.ElementsMatch(t, tc.lines, actualLines)
		//assert.ElementsMatch(t, tc.streams, streams)
	})
}

func (tc *testQueryAndLabelResults) seriesQuery(t *testing.T, qc *client.Client) {
	t.Run("series-query", func(t *testing.T) {
		seriesResult, err := qc.Series(context.Background(), "{}")
		require.NoError(t, err)
		require.ElementsMatch(t, tc.streams, seriesResult)
	})
}

func (tc *testQueryAndLabelResults) metricsQuery(t *testing.T, qc *client.Client) {
	t.Run("metrics-query", func(t *testing.T) {
		output, err := qc.RunQuery(context.Background(), metricsQuery)
		require.NoError(t, err)
		assert.Equal(t, "success", output.Status)
		assert.Equal(t, "vector", output.Data.ResultType)

		var streams []map[string]string
		for _, row := range output.Data.Vector {
			streams = append(streams, labels.FromMap(row.Metric).Map())
		}
		assert.ElementsMatch(t, tc.streams, streams)
	})
}

func (tc *testQueryAndLabelResults) metricsRangeQuery(t *testing.T, qc *client.Client) {
	t.Run("metrics-range-query", func(t *testing.T) {
		output, err := qc.RunRangeQuery(context.Background(), metricsQuery)
		require.NoError(t, err)
		require.Equal(t, "success", output.Status)
		require.Equal(t, "matrix", output.Data.ResultType)
		require.EqualValues(t, len(tc.lines), sumMatrixValues(t, output.Data.Matrix))
	})
}

func sumMatrixValues(t *testing.T, matrix []client.MatrixValues) float64 {
	t.Helper()

	var total float64
	for _, row := range matrix {
		for _, sample := range row.Values {
			require.Len(t, sample, 2)
			value, ok := sample[1].(string)
			require.True(t, ok)

			parsed, err := strconv.ParseFloat(value, 64)
			require.NoError(t, err)
			total += parsed
		}
	}

	return total
}

func normalizeLabelsData(labels map[string][]string) map[string][]string {
	normalized := make(map[string][]string, len(labels))
	for name, values := range labels {
		sortedValues := append([]string(nil), values...)
		sort.Strings(sortedValues)
		normalized[name] = sortedValues
	}
	return normalized
}
