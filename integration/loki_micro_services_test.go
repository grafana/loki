package integration

import (
	"context"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"
	"github.com/grafana/loki/pkg/storage"
)

func TestMicroServicesIngestQuery(t *testing.T) {
	clu := cluster.New(cluster.ConfigWithBoltDB(false))
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// run initially the compactor, indexgateway, and distributor.
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

	// then, run only the ingester and query scheduler.
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

	tenantID := randStringRunes()

	now := time.Now()
	cliDistributor := client.New(tenantID, "", tDistributor.HTTPURL())
	cliDistributor.Now = now
	cliIngester := client.New(tenantID, "", tIngester.HTTPURL())
	cliIngester.Now = now
	cliQueryFrontend := client.New(tenantID, "", tQueryFrontend.HTTPURL())
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

	t.Run("label-names", func(t *testing.T) {
		resp, err := cliQueryFrontend.LabelNames(context.Background())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"job"}, resp)
	})

	t.Run("label-values", func(t *testing.T) {
		resp, err := cliQueryFrontend.LabelValues(context.Background(), "job")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fake"}, resp)
	})
}

func TestMicroServicesMultipleBucketSingleProvider(t *testing.T) {
	for name, template := range map[string]*template.Template{
		"boltdb-index": cluster.ConfigWithBoltDB(true),
		"tsdb-index":   cluster.ConfigWithTSDB(true),
	} {
		t.Run(name, func(t *testing.T) {
			storage.ResetBoltDBIndexClientsWithShipper()
			clu := cluster.New(template)
			defer func() {
				storage.ResetBoltDBIndexClientsWithShipper()
				assert.NoError(t, clu.Cleanup())
			}()

			// initially, run only compactor and distributor.
			var (
				tCompactor = clu.AddComponent(
					"compactor",
					"-target=compactor",
					"-boltdb.shipper.compactor.compaction-interval=1s",
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

			t.Run("ingest-logs-store", func(t *testing.T) {
				// ingest some log lines
				require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", time.Now().Add(-48*time.Hour), map[string]string{"job": "fake"}))
				require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", time.Now().Add(-36*time.Hour), map[string]string{"job": "fake"}))
			})

			t.Run("ingest-logs-ingester", func(t *testing.T) {
				// ingest some log lines
				require.NoError(t, cliDistributor.PushLogLine("lineC", map[string]string{"job": "fake"}))
				require.NoError(t, cliDistributor.PushLogLine("lineD", map[string]string{"job": "fake"}))
			})

			t.Run("query-lookback-default", func(t *testing.T) {
				resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
				require.NoError(t, err)
				assert.Equal(t, "streams", resp.Data.ResultType)

				var lines []string
				for _, stream := range resp.Data.Stream {
					for _, val := range stream.Values {
						lines = append(lines, val[1])
					}
				}
				assert.ElementsMatch(t, []string{"lineC", "lineD"}, lines)
			})

			t.Run("flush-logs-and-restart-ingester-querier", func(t *testing.T) {
				// restart ingester which should flush the chunks
				require.NoError(t, tIngester.Restart())

				// restart querier and index shipper to sync the index
				storage.ResetBoltDBIndexClientsWithShipper()
				tQuerier.AddFlags("-querier.query-store-only=true")
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

		})
	}
}
