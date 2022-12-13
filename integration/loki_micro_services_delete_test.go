package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"
)

func TestMicroServicesDeleteRequest(t *testing.T) {
	clu := cluster.New()
	defer func() {
		assert.NoError(t, clu.Cleanup())
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
			"-compactor.deletion-mode=filter-and-delete",
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

	t.Run("ingest-logs-store", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
	})

	t.Run("ingest-logs-ingester", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLine("lineC", map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineD", map[string]string{"job": "fake"}))
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}
		assert.ElementsMatch(t, []string{"lineA", "lineB", "lineC", "lineD"}, lines)
	})

	t.Run("flush-logs-and-restart-ingester-querier", func(t *testing.T) {
		// restart ingester which should flush the chunks
		require.NoError(t, tIngester.Restart())
		// ensure that ingester has 0 chunks in memory
		cliIngester = client.New(tenantID, "", tIngester.HTTPURL())
		cliIngester.Now = now
		metrics, err := cliIngester.Metrics()
		require.NoError(t, err)
		checkMetricValue(t, "loki_ingester_chunks_flushed_total", metrics, 1)

		require.NoError(t, tQuerier.Restart())
	})

	// Query lines
	t.Run("query again to verify logs being served from storage", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}

		assert.ElementsMatch(t, []string{"lineA", "lineB", "lineC", "lineD"}, lines)
	})

	t.Run("add-delete-request", func(t *testing.T) {
		params := client.DeleteRequestParams{Start: "0000000000", Query: `{job="fake"} |= "lineB"`}
		require.NoError(t, cliCompactor.AddDeleteRequest(params))
	})

	t.Run("read-delete-request", func(t *testing.T) {
		deleteRequests, err := cliCompactor.GetDeleteRequests()
		require.NoError(t, err)
		require.NotEmpty(t, deleteRequests)
		require.Len(t, deleteRequests, 1)
		require.Equal(t, `{job="fake"} |= "lineB"`, deleteRequests[0].Query)
		require.Equal(t, "received", deleteRequests[0].Status)
	})

	// Wait until delete request is finished
	t.Run("wait-until-delete-request-processed", func(t *testing.T) {
		require.Eventually(t, func() bool {
			deleteRequests, err := cliCompactor.GetDeleteRequests()
			require.NoError(t, err)
			require.Len(t, deleteRequests, 1)
			return deleteRequests[0].Status == "processed"
		}, 10*time.Second, 1*time.Second)

		// Check metrics
		metrics, err := cliCompactor.Metrics()
		require.NoError(t, err)
		checkUserLabelAndMetricValue(t, "loki_compactor_delete_requests_processed_total", metrics, tenantID, 1)
		checkUserLabelAndMetricValue(t, "loki_compactor_deleted_lines", metrics, tenantID, 1)
	})

	// Query lines
	t.Run("query", func(t *testing.T) {
		// restart querier to make it sync the index
		require.NoError(t, tQuerier.Restart())

		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}

		assert.ElementsMatch(t, []string{"lineA", "lineC", "lineD"}, lines, "lineB should not be there")
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
	val, _, err := extractMetric(metricName, metrics)
	require.NoError(t, err)
	require.Equal(t, expectedValue, val)
}
