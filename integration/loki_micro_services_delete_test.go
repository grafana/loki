//go:build integration

package integration

import (
	"context"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage"
)

type pushRequest struct {
	stream  map[string]string
	entries []logproto.Entry
}

func TestMicroServicesDeleteRequest(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDBAndTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// initially, run only compactor, index-gateway and distributor.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1s",
			"-compactor.retention-delete-delay=1s",
			// By default, a minute is added to the delete request start time. This compensates for that.
			"-compactor.delete-request-cancel-period=-60s",
			"-compactor.deletion-mode=filter-only",
			"-compactor.delete-max-interval=0",
			"-limits.per-user-override-period=1s",
		)
		tDistributor = clu.AddComponent(
			"distributor",
			"-target=distributor",
		)
	)
	require.NoError(t, clu.Run())

	// then, run only ingester and query-scheduler.
	var (
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-ingester.flush-on-shutdown=true",
			"-ingester.wal-enabled=false",
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
		)
	)
	require.NoError(t, clu.Run())

	// finally, run the query-frontend and querier.
	var (
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			"-target=query-frontend",
			"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-frontend.default-validity=0s",
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
		tQuerier = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
	)
	require.NoError(t, clu.Run())

	tenantID := randStringRunes()

	now := time.Now()
	cliDistributor := client.New(tenantID, "", tDistributor.HTTPURL())
	cliDistributor.Now = now
	cliIngester := client.New(tenantID, "", tIngester.HTTPURL())
	cliIngester.Now = now
	cliQueryFrontend := client.New(tenantID, "", tQueryFrontend.HTTPURL())
	cliQueryFrontend.Now = now
	cliCompactor := client.New(tenantID, "", tCompactor.HTTPURL())
	cliCompactor.Now = now

	var pushRequests []pushRequest
	var expectedStreams []client.StreamValues
	for _, deletionType := range []string{"filter", "filter_no_match", "nothing", "partially_by_time", "whole"} {
		pushRequests = append(pushRequests, pushRequest{
			stream: map[string]string{
				"job":           "fake",
				"deletion_type": deletionType,
			},
			entries: []logproto.Entry{
				{
					Timestamp: now.Add(-48 * time.Hour),
					Line:      "lineA",
				},
				{
					Timestamp: now.Add(-48 * time.Hour),
					Line:      "lineB",
				},
				{
					Timestamp: now.Add(-time.Minute),
					Line:      "lineC",
				},
				{
					Timestamp: now.Add(-time.Minute),
					Line:      "lineD",
				},
			},
		})
	}

	pushRequests = append(pushRequests, pushRequest{
		stream: map[string]string{
			"job":           "fake",
			"deletion_type": "with_structured_metadata",
		},
		entries: []logproto.Entry{
			{
				Timestamp: now.Add(-48 * time.Hour),
				Line:      "AlineA",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "line",
						Value: "A",
					},
				},
			},
			{
				Timestamp: now.Add(-48 * time.Hour),
				Line:      "AlineB",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "line",
						Value: "B",
					},
				},
			},
			{
				Timestamp: now.Add(-time.Minute),
				Line:      "AlineC",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "line",
						Value: "C",
					},
				},
			},
			{
				Timestamp: now.Add(-time.Minute),
				Line:      "AlineD",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  "line",
						Value: "D",
					},
				},
			},
		},
	})

	for _, pr := range pushRequests {
		expectedStreams = append(expectedStreams, pushRequestToClientStreamValues(t, pr)...)
	}

	expectedDeleteRequests := []client.DeleteRequest{
		{
			StartTime: now.Add(-48 * time.Hour).Unix(),
			EndTime:   now.Unix(),
			Query:     `{deletion_type="filter"} |= "lineB"`,
			Status:    "received",
		},
		{
			StartTime: now.Add(-48 * time.Hour).Unix(),
			EndTime:   now.Unix(),
			Query:     `{deletion_type="filter_no_match"} |= "foo"`,
			Status:    "received",
		},
		{
			StartTime: now.Add(-48 * time.Hour).Unix(),
			EndTime:   now.Add(-10 * time.Minute).Unix(),
			Query:     `{deletion_type="partially_by_time"}`,
			Status:    "received",
		},
		{
			StartTime: now.Add(-48 * time.Hour).Unix(),
			EndTime:   now.Unix(),
			Query:     `{deletion_type="whole"}`,
			Status:    "received",
		},
		{
			StartTime: now.Add(-48 * time.Hour).Unix(),
			EndTime:   now.Unix(),
			Query:     `{deletion_type="with_structured_metadata"} | line="A"`,
			Status:    "received",
		},
	}

	validateQueryResponse := func(expectedStreams []client.StreamValues, resp *client.Response) {
		t.Helper()
		assert.Equal(t, "success", resp.Status)
		assert.Equal(t, "streams", resp.Data.ResultType)

		require.Len(t, resp.Data.Stream, len(expectedStreams))
		sort.Slice(resp.Data.Stream, func(i, j int) bool {
			return labels.FromMap(resp.Data.Stream[i].Stream).String() < labels.FromMap(resp.Data.Stream[j].Stream).String()
		})
		for _, stream := range resp.Data.Stream {
			sort.Slice(stream.Values, func(i, j int) bool {
				return stream.Values[i][1] < stream.Values[j][1]
			})
		}
		require.Equal(t, expectedStreams, resp.Data.Stream)
	}

	t.Run("ingest-logs", func(t *testing.T) {
		// ingest some log lines
		for _, pr := range pushRequests {
			for _, entry := range pr.entries {
				require.NoError(t, cliDistributor.PushLogLine(
					entry.Line,
					entry.Timestamp,
					logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata).Map(),
					pr.stream,
				))
			}
		}
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)

		// given default value of query_ingesters_within is 3h, older samples won't be present in the response
		var es []client.StreamValues
		for _, stream := range expectedStreams {
			s := client.StreamValues{
				Stream: stream.Stream,
				Values: nil,
			}
			for _, sv := range stream.Values {
				tsNs, err := strconv.ParseInt(sv[0], 10, 64)
				require.NoError(t, err)
				if !time.Unix(0, tsNs).Before(now.Add(-3 * time.Hour)) {
					s.Values = append(s.Values, sv)
				}
			}
			if len(s.Values) > 0 {
				es = append(es, s)
			}
		}
		validateQueryResponse(es, resp)
	})

	t.Run("flush-logs-and-restart-ingester-querier", func(t *testing.T) {
		// restart ingester which should flush the chunks
		require.NoError(t, tIngester.Restart())
		// ensure that ingester has 0 chunks in memory
		cliIngester = client.New(tenantID, "", tIngester.HTTPURL())
		cliIngester.Now = now
		metrics, err := cliIngester.Metrics()
		require.NoError(t, err)
		checkMetricValue(t, "loki_ingester_chunks_flushed_total", metrics, 6)

		// reset boltdb-shipper client and restart querier
		storage.ResetBoltDBIndexClientsWithShipper()
		require.NoError(t, tQuerier.Restart())
	})

	// Query lines
	t.Run("query again to verify logs being served from storage", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		validateQueryResponse(expectedStreams, resp)
	})

	t.Run("add-delete-requests", func(t *testing.T) {
		for _, deleteRequest := range expectedDeleteRequests {
			params := client.DeleteRequestParams{
				Start: strconv.FormatInt(deleteRequest.StartTime, 10),
				End:   strconv.FormatInt(deleteRequest.EndTime, 10),
				Query: deleteRequest.Query,
			}
			require.NoError(t, cliCompactor.AddDeleteRequest(params))
		}
	})

	t.Run("read-delete-request", func(t *testing.T) {
		deleteRequests, err := cliCompactor.GetDeleteRequests()
		require.NoError(t, err)
		require.ElementsMatch(t, client.DeleteRequests(expectedDeleteRequests), deleteRequests)
	})

	// Query lines
	t.Run("verify query time filtering", func(t *testing.T) {
		// reset boltdb-shipper client and restart querier
		storage.ResetBoltDBIndexClientsWithShipper()
		require.NoError(t, tQuerier.Restart())

		// update expectedStreams as per the issued requests
		expectedStreams[0].Values = append(expectedStreams[0].Values[:1], expectedStreams[0].Values[2:]...)
		expectedStreams[3].Values = expectedStreams[3].Values[2:]
		expectedStreams = append(expectedStreams[:4], expectedStreams[6:]...)

		// query and verify that we get the resp which matches expectedStreams
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)

		validateQueryResponse(expectedStreams, resp)
	})

	// Wait until delete request is finished
	t.Run("wait-until-delete-request-processed", func(t *testing.T) {
		tenantLimits := tCompactor.GetTenantLimits(tenantID)
		tenantLimits.DeletionMode = "filter-and-delete"
		require.NoError(t, tCompactor.SetTenantLimits(tenantID, tenantLimits))

		// all the delete requests should have been processed
		for i := range expectedDeleteRequests {
			expectedDeleteRequests[i].Status = "processed"
		}

		require.Eventually(t, func() bool {
			deleteRequests, err := cliCompactor.GetDeleteRequests()
			require.NoError(t, err)

		outer:
			for i := range deleteRequests {
				for j := range expectedDeleteRequests {
					if deleteRequests[i] == expectedDeleteRequests[j] {
						continue outer
					}
				}
				return false
			}
			return true
		}, 20*time.Second, 1*time.Second)

		// Check metrics
		metrics, err := cliCompactor.Metrics()
		require.NoError(t, err)
		checkUserLabelAndMetricValue(t, "loki_compactor_delete_requests_processed_total", metrics, tenantID, float64(len(expectedDeleteRequests)))

		// ideally this metric should be equal to 2 given that a single line matches the line filter and structured metadata filter
		// but the same chunks are indexed in 3 tables
		checkUserLabelAndMetricValue(t, "loki_compactor_deleted_lines", metrics, tenantID, 6)
	})

	// Query lines
	t.Run("query-without-query-time-filtering", func(t *testing.T) {
		// disable deletion for tenant to stop query time filtering of data requested for deletion
		tenantLimits := tQuerier.GetTenantLimits(tenantID)
		tenantLimits.DeletionMode = "disabled"
		require.NoError(t, tQuerier.SetTenantLimits(tenantID, tenantLimits))

		// restart querier to make it sync the index
		storage.ResetBoltDBIndexClientsWithShipper()
		require.NoError(t, tQuerier.Restart())

		// ensure the deletion-mode limit is updated
		require.Equal(t, "disabled", tQuerier.GetTenantLimits(tenantID).DeletionMode)

		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)

		validateQueryResponse(expectedStreams, resp)
	})
}

func checkUserLabelAndMetricValue(t *testing.T, metricName, metrics, tenantID string, expectedValue float64) {
	t.Helper()
	val, labels, err := extractMetric(metricName, metrics)
	require.NoError(t, err)
	require.NotNil(t, labels)
	require.Len(t, labels, 1)
	require.Contains(t, labels, "user")
	require.Equal(t, labels["user"], tenantID)
	require.Equal(t, expectedValue, val)
}

func checkMetricValue(t *testing.T, metricName, metrics string, expectedValue float64) {
	t.Helper()
	require.Equal(t, expectedValue, getMetricValue(t, metricName, metrics))
}

func getMetricValue(t *testing.T, metricName, metrics string) float64 {
	t.Helper()
	val, _, err := extractMetric(metricName, metrics)
	require.NoError(t, err)
	return val
}

func pushRequestToClientStreamValues(t *testing.T, p pushRequest) []client.StreamValues {
	logsByStream := map[string][]client.Entry{}
	for _, entry := range p.entries {
		lb := labels.NewBuilder(labels.FromMap(p.stream))
		for _, l := range entry.StructuredMetadata {
			lb.Set(l.Name, l.Value)
		}
		stream := lb.Labels().String()
		logsByStream[stream] = append(logsByStream[stream], []string{
			strconv.FormatInt(entry.Timestamp.UnixNano(), 10),
			entry.Line,
		})
	}

	var svs []client.StreamValues
	for stream, values := range logsByStream {
		parsedLabels, err := syntax.ParseLabels(stream)
		require.NoError(t, err)

		svs = append(svs, client.StreamValues{
			Stream: parsedLabels.Map(),
			Values: values,
		})
	}

	sort.Slice(svs, func(i, j int) bool {
		return labels.FromMap(svs[i].Stream).String() < labels.FromMap(svs[j].Stream).String()
	})

	return svs
}
