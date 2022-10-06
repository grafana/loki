package integration

import (
	"context"
	"net/http"
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
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
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
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
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
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
		_ = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
	)
	require.NoError(t, clu.Run())

	remoteCalled := []bool{false, false}

	var (
		tRuler = clu.AddComponent(
			"ruler",
			"-target=ruler",
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
	)

	handler1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/write" {
			t.Errorf("Expected to request '/api/v1/write', got: %s", r.URL.Path)
		}
		remoteCalled[0] = true

		w.WriteHeader(http.StatusOK)
	})
	server1 := cluster.NewRemoteWriteServer(&handler1)
	defer server1.Close()

	handler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/write" {
			t.Errorf("Expected to request '/api/v1/write', got: %s", r.URL.Path)
		}
		remoteCalled[1] = true

		w.WriteHeader(http.StatusOK)
	})
	server2 := cluster.NewRemoteWriteServer(&handler2)
	defer server2.Close()

	tRuler.RemoteWriteUrls = []string{
		server1.URL,
		server2.URL,
	}
	// initialize only the ruler now.
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
	cliRuler := client.New(tRuler.RulesTenant, "", tRuler.HTTPURL())
	cliRuler.Now = now

	t.Run("ingest-logs-store", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))

		// TODO: Flushing is currently causing a panic, as the boltdb shipper is shared using a global variable in:
		// https://github.com/grafana/loki/blob/66a4692423582ed17cce9bd86b69d55663dc7721/pkg/storage/factory.go#L32-L35
		// require.NoError(t, cliIngester.Flush())
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
		checkLabelValue(t, "loki_compactor_delete_requests_processed_total", metrics, tenantID, 1)
		// Re-enable this once flush works
		// checkLabelValue(t, "loki_compactor_deleted_lines", metrics, tenantID, 1)
	})

	// Query lines
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
		// Remove lineB once flush works
		assert.ElementsMatch(t, []string{"lineA", "lineB", "lineC", "lineD"}, lines, "lineB should not be there")
	})

	t.Run("ruler", func(t *testing.T) {
		// Check rules are read correctly.
		resp, err := cliRuler.GetRules(context.Background())

		require.NoError(t, err)
		require.NotNil(t, resp)

		require.Equal(t, "success", resp.Status)

		require.Len(t, resp.Data.Groups, 1)
		require.Len(t, resp.Data.Groups[0].Rules, 1)

		// Wait for remote write to be called.
		time.Sleep(5 * time.Second)

		// Check remote write was successful.
		require.EqualValues(t, []bool{true, true}, remoteCalled, "one or both of the remote write target were not called")
	})
}

func checkLabelValue(t *testing.T, metricName, metrics, tenantID string, expectedValue float64) {
	t.Helper()
	val, labels, err := extractMetric(metricName, metrics)
	require.NoError(t, err)
	require.NotNil(t, labels)
	require.Len(t, labels, 1)
	require.Contains(t, labels, "user")
	require.Equal(t, labels["user"], tenantID)
	require.Equal(t, expectedValue, val)
}
