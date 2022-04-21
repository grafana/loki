package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"
)

func TestMicroServicesIngestQuery(t *testing.T) {
	clu := cluster.New()
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
		)
		tDistributor = clu.AddComponent(
			"distributor",
			"-target=distributor",
		)
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL().Host,
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL().Host,
		)
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			"-target=query-frontend",
			"-frontend.scheduler-address="+tQueryScheduler.GRPCURL().Host,
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL().Host,
		)
		_ = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL().Host,
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL().Host,
		)
	)

	require.NoError(t, clu.Run())

	tenantID := randStringRunes(12)

	now := time.Now()
	cliDistributor := client.New(tenantID, "", tDistributor.HTTPURL().String())
	cliDistributor.Now = now
	cliIngester := client.New(tenantID, "", tIngester.HTTPURL().String())
	cliIngester.Now = now
	cliQueryFrontend := client.New(tenantID, "", tQueryFrontend.HTTPURL().String())
	cliQueryFrontend.Now = now

	t.Run("ingest-logs-store", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))

		// TODO: Flushing is currently causing a panic, as the boltdb shipper is shared using a global variable in:
		// https://github.com/grafana/loki/blob/66a4692423582ed17cce9bd86b69d55663dc7721/pkg/storage/factory.go#L32-L35
		//require.NoError(t, cliIngester.Flush())
	})

	t.Run("ingest-logs-ingester", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLine("lineC", map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineD", map[string]string{"job": "fake"}))
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(`{job="fake"}`)
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

	t.Run("label-names", func(t *testing.T) {
		resp, err := cliQueryFrontend.LabelNames()
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"__name__", "job"}, resp)
	})

	t.Run("label-values", func(t *testing.T) {
		resp, err := cliQueryFrontend.LabelValues("job")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fake"}, resp)
	})
}
