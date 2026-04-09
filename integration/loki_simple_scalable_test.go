//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

func TestSimpleScalable_IngestQuery(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// Start compactor, index-gateway, and distributor first.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
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

	// Then start ingester and query-scheduler (both need the index-gateway address).
	tQueryScheduler := clu.AddComponent(
		"query-scheduler",
		"-target=query-scheduler",
		"-query-scheduler.use-scheduler-ring=false",
		"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
	)
	clu.AddComponent(
		"ingester",
		"-target=ingester",
		"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
	)
	require.NoError(t, clu.Run())

	// Then start querier (needs query-scheduler address).
	clu.AddComponent(
		"querier",
		"-target=querier",
		"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		"-common.compactor-address="+tCompactor.HTTPURL(),
	)
	require.NoError(t, clu.Run())

	// Finally start query-frontend (needs query-scheduler address).
	tQueryFrontend := clu.AddComponent(
		"query-frontend",
		"-target=query-frontend",
		"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		"-common.compactor-address="+tCompactor.HTTPURL(),
	)
	require.NoError(t, clu.Run())

	tenantID := randStringRunes()

	now := time.Now()
	cliWrite := client.New(tenantID, "", tDistributor.HTTPURL())
	cliWrite.Now = now
	cliRead := client.New(tenantID, "", tQueryFrontend.HTTPURL())
	cliRead.Now = now

	t.Run("ingest logs", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliWrite.PushLogLine("lineA", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliWrite.PushLogLine("lineB", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))

		require.NoError(t, cliWrite.PushLogLine("lineC", now, nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliWrite.PushLogLine("lineD", now, nil, map[string]string{"job": "fake"}))
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliRead.RunRangeQuery(context.Background(), `{job="fake"}`)
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
		resp, err := cliRead.LabelNames(context.Background())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"job"}, resp)
	})

	t.Run("label-values", func(t *testing.T) {
		resp, err := cliRead.LabelValues(context.Background(), "job")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fake"}, resp)
	})
}
