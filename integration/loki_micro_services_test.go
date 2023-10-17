package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util/querylimits"
)

func TestMicroServicesIngestQuery(t *testing.T) {
	clu := cluster.New(nil)
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
			"-querier.per-request-limits-enabled=true",
			"-frontend.required-query-response-format=protobuf",
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

	t.Run("ingest-logs", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))

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

	t.Run("per-request-limits", func(t *testing.T) {
		queryLimitsPolicy := client.InjectHeadersOption(map[string][]string{querylimits.HTTPHeaderQueryLimitsKey: {`{"maxQueryLength": "1m"}`}})
		cliQueryFrontendLimited := client.New(tenantID, "", tQueryFrontend.HTTPURL(), queryLimitsPolicy)
		cliQueryFrontendLimited.Now = now

		_, err := cliQueryFrontendLimited.LabelNames(context.Background())
		require.ErrorContains(t, err, "the query time range exceeds the limit (query length")
	})
}

func TestMicroServicesIngestQueryWithSchemaChange(t *testing.T) {
	// init the cluster with a single tsdb period. Uses prefix index_tsdb_
	clu := cluster.New(nil, cluster.SchemaWithTSDB)

	defer func() {
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

	t.Run("ingest-logs", func(t *testing.T) {
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", time.Now().Add(-72*time.Hour), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", time.Now().Add(-48*time.Hour), map[string]string{"job": "fake"}))
	})

	t.Run("query-lookback-default", func(t *testing.T) {
		// queries ingesters with the default lookback period (3h)
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}
		assert.ElementsMatch(t, []string{}, lines)
	})

	t.Run("query-lookback-7d", func(t *testing.T) {
		tQuerier.AddFlags("-querier.query-ingesters-within=168h")
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
		assert.ElementsMatch(t, []string{"lineA", "lineB"}, lines)

		tQuerier.AddFlags("-querier.query-ingesters-within=3h")
		require.NoError(t, tQuerier.Restart())
	})

	t.Run("flush-logs-and-restart-ingester-querier", func(t *testing.T) {
		// restart ingester which should flush the chunks and index
		require.NoError(t, tIngester.Restart())

		// restart querier and index shipper to sync the index
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

		assert.ElementsMatch(t, []string{"lineA", "lineB"}, lines)

		tQuerier.AddFlags("-querier.query-store-only=false")
		require.NoError(t, tQuerier.Restart())
	})

	// Add new tsdb period with a different index prefix(index_)
	clu.ResetSchemaConfig()
	cluster.SchemaWithTSDBAndTSDB(clu)

	// restart to load the new schema
	require.NoError(t, tIngester.Restart())
	require.NoError(t, tQuerier.Restart())

	t.Run("ingest-logs-new-period", func(t *testing.T) {
		// ingest logs to the new period
		require.NoError(t, cliDistributor.PushLogLine("lineC", map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineD", map[string]string{"job": "fake"}))
	})

	t.Run("query-both-periods-with-default-lookback", func(t *testing.T) {
		// queries with the default lookback period (3h)
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
		// restart ingester which should flush the chunks and index
		require.NoError(t, tIngester.Restart())

		// restart querier and index shipper to sync the index
		tQuerier.AddFlags("-querier.query-store-only=true")
		require.NoError(t, tQuerier.Restart())
	})

	// Query lines
	t.Run("query both periods to verify logs being served from storage", func(t *testing.T) {
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
}

func TestMicroServicesIngestQueryOverMultipleBucketSingleProvider(t *testing.T) {
	for name, opt := range map[string]func(c *cluster.Cluster){
		"boltdb-index":    cluster.SchemaWithBoltDBAndBoltDB,
		"tsdb-index":      cluster.SchemaWithTSDBAndTSDB,
		"boltdb-and-tsdb": cluster.SchemaWithBoltDBAndTSDB,
	} {
		t.Run(name, func(t *testing.T) {
			storage.ResetBoltDBIndexClientsWithShipper()
			clu := cluster.New(nil, opt)

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

			t.Run("ingest-logs", func(t *testing.T) {
				require.NoError(t, cliDistributor.PushLogLineWithTimestampAndStructuredMetadata("lineA", time.Now().Add(-48*time.Hour), map[string]string{"traceID": "123"}, map[string]string{"job": "fake"}))
				require.NoError(t, cliDistributor.PushLogLineWithTimestampAndStructuredMetadata("lineB", time.Now().Add(-36*time.Hour), map[string]string{"traceID": "456"}, map[string]string{"job": "fake"}))

				// ingest logs to the current period
				require.NoError(t, cliDistributor.PushLogLineWithStructuredMetadata("lineC", map[string]string{"traceID": "789"}, map[string]string{"job": "fake"}))
				require.NoError(t, cliDistributor.PushLogLineWithStructuredMetadata("lineD", map[string]string{"traceID": "123"}, map[string]string{"job": "fake"}))

			})

			t.Run("query-lookback-default", func(t *testing.T) {
				// queries ingesters with the default lookback period (3h)
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
				// restart ingester which should flush the chunks and index
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

func TestSchedulerRing(t *testing.T) {
	clu := cluster.New(nil)
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
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-query-scheduler.use-scheduler-ring=true",
		)
	)
	require.NoError(t, clu.Run())

	// finally, run the query-frontend and querier.
	var (
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			"-target=query-frontend",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-querier.per-request-limits-enabled=true",
			"-query-scheduler.use-scheduler-ring=true",
			"-frontend.scheduler-worker-concurrency=5",
		)
		_ = clu.AddComponent(
			"querier",
			"-target=querier",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-query-scheduler.use-scheduler-ring=true",
			"-querier.max-concurrent=4",
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
	cliQueryScheduler := client.New(tenantID, "", tQueryScheduler.HTTPURL())
	cliQueryScheduler.Now = now

	t.Run("verify-scheduler-connections", func(t *testing.T) {
		require.Eventually(t, func() bool {
			// Check metrics to see if query scheduler is connected with query-frontend
			metrics, err := cliQueryScheduler.Metrics()
			require.NoError(t, err)
			return getMetricValue(t, "cortex_query_scheduler_connected_frontend_clients", metrics) == 5
		}, 5*time.Second, 500*time.Millisecond)

		require.Eventually(t, func() bool {
			// Check metrics to see if query scheduler is connected with query-frontend
			metrics, err := cliQueryScheduler.Metrics()
			require.NoError(t, err)
			return getMetricValue(t, "cortex_query_scheduler_connected_querier_clients", metrics) == 4
		}, 5*time.Second, 500*time.Millisecond)
	})

	t.Run("ingest-logs", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", now.Add(-45*time.Minute), map[string]string{"job": "fake"}))

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
}

func TestQueryTSDB_WithCachedPostings(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB)

	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tDistributor = clu.AddComponent(
			"distributor",
			"-target=distributor",
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
			"-tsdb.enable-postings-cache=true",
			"-store.index-cache-read.embedded-cache.enabled=true",
		)
	)
	require.NoError(t, clu.Run())

	var (
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-ingester.flush-on-shutdown=true",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-boltdb.shipper.compactor.compaction-interval=1s",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
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
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		_ = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
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
	cliIndexGateway := client.New(tenantID, "", tIndexGateway.HTTPURL())
	cliIndexGateway.Now = now

	// initial cache state.
	igwMetrics, err := cliIndexGateway.Metrics()
	require.NoError(t, err)
	assertCacheState(t, igwMetrics, &expectedCacheState{
		cacheName: "store.index-cache-read.embedded-cache",
		misses:    0,
		added:     0,
	})

	t.Run("ingest-logs", func(t *testing.T) {
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineA", time.Now().Add(-72*time.Hour), map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLineWithTimestamp("lineB", time.Now().Add(-48*time.Hour), map[string]string{"job": "fake"}))
	})

	// restart ingester which should flush the chunks and index
	require.NoError(t, tIngester.Restart())

	// Query lines
	t.Run("query to verify logs being served from storage", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}

		assert.ElementsMatch(t, []string{"lineA", "lineB"}, lines)
	})

	igwMetrics, err = cliIndexGateway.Metrics()
	require.NoError(t, err)
	assertCacheState(t, igwMetrics, &expectedCacheState{
		cacheName: "store.index-cache-read.embedded-cache",
		misses:    1,
		added:     1,
	})

	// ingest logs with ts=now.
	require.NoError(t, cliDistributor.PushLogLine("lineC", map[string]string{"job": "fake"}))
	require.NoError(t, cliDistributor.PushLogLine("lineD", map[string]string{"job": "fake"}))

	// default length is 7 days.
	resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
	require.NoError(t, err)
	assert.Equal(t, "streams", resp.Data.ResultType)

	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			lines = append(lines, val[1])
		}
	}
	// expect lines from both, ingesters memory and from the store.
	assert.ElementsMatch(t, []string{"lineA", "lineB", "lineC", "lineD"}, lines)

}

func getValueFromMF(mf *dto.MetricFamily, lbs []*dto.LabelPair) float64 {
	for _, m := range mf.Metric {
		if !assert.ObjectsAreEqualValues(lbs, m.GetLabel()) {
			continue
		}

		return m.Counter.GetValue()
	}

	return 0
}

func assertCacheState(t *testing.T, metrics string, e *expectedCacheState) {
	var parser expfmt.TextParser
	mfs, err := parser.TextToMetricFamilies(strings.NewReader(metrics))
	require.NoError(t, err)

	lbs := []*dto.LabelPair{
		{
			Name:  proto.String("cache"),
			Value: proto.String(e.cacheName),
		},
	}

	mf, found := mfs["loki_embeddedcache_added_new_total"]
	require.True(t, found)
	require.Equal(t, e.added, getValueFromMF(mf, lbs))

	lbs = []*dto.LabelPair{
		{
			Name:  proto.String("name"),
			Value: proto.String(e.cacheName),
		},
	}

	gets, found := mfs["loki_cache_fetched_keys"]
	require.True(t, found)

	hits, found := mfs["loki_cache_hits"]
	require.True(t, found)
	require.Equal(t, e.misses, getValueFromMF(gets, lbs)-getValueFromMF(hits, lbs))
}

type expectedCacheState struct {
	cacheName string
	misses    float64
	added     float64
}
