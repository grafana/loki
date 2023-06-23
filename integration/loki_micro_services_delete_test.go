package integration

import (
	"context"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"

	"github.com/grafana/loki/pkg/storage"
)

func TestMicroServicesDeleteRequest(t *testing.T) {
	storage.ResetBoltDBIndexClientsWithShipper()
	clu := cluster.New(nil, cluster.SchemaWithBoltDBAndBoltDB)
	defer func() {
		assert.NoError(t, clu.Cleanup())
		storage.ResetBoltDBIndexClientsWithShipper()
	}()

	// initially, run only compactor, index-gateway and distributor.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-boltdb.shipper.compactor.compaction-interval=1s",
			"-boltdb.shipper.compactor.retention-delete-delay=1s",
			// By default, a minute is added to the delete request start time. This compensates for that.
			"-boltdb.shipper.compactor.delete-request-cancel-period=-60s",
			"-compactor.deletion-mode=filter-only",
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

	var expectedStreams []client.StreamValues
	for _, deletionType := range []string{"filter", "filter_no_match", "nothing", "partially_by_time", "whole"} {
		expectedStreams = append(expectedStreams, client.StreamValues{
			Stream: map[string]string{
				"job":           "fake",
				"deletion_type": deletionType,
			},
			Values: [][]string{
				{
					strconv.FormatInt(now.Add(-48*time.Hour).UnixNano(), 10),
					"lineA",
				},
				{
					strconv.FormatInt(now.Add(-48*time.Hour).UnixNano(), 10),
					"lineB",
				},
				{
					strconv.FormatInt(now.Add(-time.Minute).UnixNano(), 10),
					"lineC",
				},
				{
					strconv.FormatInt(now.Add(-time.Minute).UnixNano(), 10),
					"lineD",
				},
			},
		})
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
	}

	validateQueryResponse := func(expectedStreams []client.StreamValues, resp *client.Response) {
		t.Helper()
		assert.Equal(t, "streams", resp.Data.ResultType)

		require.Len(t, resp.Data.Stream, len(expectedStreams))
		sort.Slice(resp.Data.Stream, func(i, j int) bool {
			return resp.Data.Stream[i].Stream["deletion_type"] < resp.Data.Stream[j].Stream["deletion_type"]
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
		for _, stream := range expectedStreams {
			for _, val := range stream.Values {
				tsNs, err := strconv.ParseInt(val[0], 10, 64)
				require.NoError(t, err)
				require.NoError(t, cliDistributor.PushLogLineWithTimestamp(val[1], time.Unix(0, tsNs), stream.Stream))
			}
		}
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)

		// given default value of query_ingesters_within is 3h, older samples won't be present in the response
		var es []client.StreamValues
		for _, stream := range expectedStreams {
			stream.Values = stream.Values[2:]
			es = append(es, stream)
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
		checkMetricValue(t, "loki_ingester_chunks_flushed_total", metrics, 5)

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
		expectedStreams = expectedStreams[:4]

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

		// ideally this metric should be equal to 1 given that a single line matches the line filter
		// but the same chunk is indexed in 3 tables
		checkUserLabelAndMetricValue(t, "loki_compactor_deleted_lines", metrics, tenantID, 3)
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
