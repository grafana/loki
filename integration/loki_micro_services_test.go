//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log/level"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util/httpreq"
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
			"-compactor.compaction-interval=1s",
			"-compactor.retention-delete-delay=1s",
			// By default, a minute is added to the delete request start time. This compensates for that.
			"-compactor.delete-request-cancel-period=-60s",
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

	// the run querier.
	var (
		tQuerier = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
	)
	require.NoError(t, clu.Run())

	// finally, run the query-frontend.
	var (
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			"-target=query-frontend",
			"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-querier.per-request-limits-enabled=true",
			"-frontend.encoding=protobuf",
			"-querier.shard-aggregations=quantile_over_time",
			"-frontend.tail-proxy-url="+tQuerier.HTTPURL(),
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
		require.NoError(t, cliDistributor.PushLogLine("lineA", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineB", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))

		require.NoError(t, cliDistributor.PushLogLine("lineC", now, nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineD", now, nil, map[string]string{"job": "fake"}))
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

	t.Run("series", func(t *testing.T) {
		resp, err := cliQueryFrontend.Series(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.ElementsMatch(t, []map[string]string{{"job": "fake"}}, resp)
	})

	t.Run("series error", func(t *testing.T) {
		_, err := cliQueryFrontend.Series(context.Background(), `{job="fake"}|= "search"`)
		require.ErrorContains(t, err, "status code 400: only label matchers are supported")
	})

	t.Run("stats error", func(t *testing.T) {
		_, err := cliQueryFrontend.Stats(context.Background(), `{job="fake"}|= "search"`)
		require.ErrorContains(t, err, "status code 400: only label matchers are supported")
	})

	t.Run("per-request-limits", func(t *testing.T) {
		queryLimitsPolicy := client.InjectHeadersOption(map[string][]string{querylimits.HTTPHeaderQueryLimitsKey: {`{"maxQueryLength": "1m"}`}})
		cliQueryFrontendLimited := client.New(tenantID, "", tQueryFrontend.HTTPURL(), queryLimitsPolicy)
		cliQueryFrontendLimited.Now = now

		_, err := cliQueryFrontendLimited.LabelNames(context.Background())
		require.ErrorContains(t, err, "the query time range exceeds the limit (query length")
	})

	t.Run("tail", func(t *testing.T) {
		ctx, cancelFunc := context.WithCancel(context.Background())
		defer cancelFunc()

		out := make(chan client.TailResult)
		wc, err := cliQueryFrontend.Tail(ctx, `{job="fake"}`, out)
		require.NoError(t, err)
		defer wc.Close()

		var lines []string
		mu := sync.Mutex{}
		done := make(chan struct{})
		go func() {
			for resp := range out {
				require.NoError(t, resp.Err)
				for _, stream := range resp.Response.Streams {
					for _, e := range stream.Entries {
						mu.Lock()
						lines = append(lines, e.Line)
						mu.Unlock()
					}
				}
			}
			done <- struct{}{}
		}()
		assert.Eventually(
			t,
			func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(lines) == 4
			},
			10*time.Second,
			100*time.Millisecond,
		)
		wc.Close()
		cancelFunc()
		<-done
		assert.ElementsMatch(t, []string{"lineA", "lineB", "lineC", "lineD"}, lines)
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
			"-compactor.compaction-interval=1s",
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
		require.NoError(t, cliDistributor.PushLogLine("lineA", time.Now().Add(-72*time.Hour), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineB", time.Now().Add(-48*time.Hour), nil, map[string]string{"job": "fake"}))
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
		require.NoError(t, cliDistributor.PushLogLine("lineC", now, nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineD", now, nil, map[string]string{"job": "fake"}))
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
			clu := cluster.New(nil, opt)

			defer func() {
				assert.NoError(t, clu.Cleanup())
			}()

			// initially, run only compactor and distributor.
			var (
				tCompactor = clu.AddComponent(
					"compactor",
					"-target=compactor",
					"-compactor.compaction-interval=1s",
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
				require.NoError(t, cliDistributor.PushLogLine("lineA", time.Now().Add(-48*time.Hour), map[string]string{"traceID": "123"}, map[string]string{"job": "fake"}))
				require.NoError(t, cliDistributor.PushLogLine("lineB", time.Now().Add(-36*time.Hour), map[string]string{"traceID": "456"}, map[string]string{"job": "fake"}))

				// ingest logs to the current period
				require.NoError(t, cliDistributor.PushLogLine("lineC", now, map[string]string{"traceID": "789"}, map[string]string{"job": "fake"}))
				require.NoError(t, cliDistributor.PushLogLine("lineD", now, map[string]string{"traceID": "123"}, map[string]string{"job": "fake"}))

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
			"-compactor.compaction-interval=1s",
			"-compactor.retention-delete-delay=1s",
			// By default, a minute is added to the delete request start time. This compensates for that.
			"-compactor.delete-request-cancel-period=-60s",
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
			return getMetricValue(t, "loki_query_scheduler_connected_frontend_clients", metrics) == 5
		}, 5*time.Second, 500*time.Millisecond)

		require.Eventually(t, func() bool {
			// Check metrics to see if query scheduler is connected with query-frontend
			metrics, err := cliQueryScheduler.Metrics()
			require.NoError(t, err)
			return getMetricValue(t, "loki_query_scheduler_connected_querier_clients", metrics) == 4
		}, 5*time.Second, 500*time.Millisecond)
	})

	t.Run("ingest-logs", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLine("lineA", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineB", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))

		require.NoError(t, cliDistributor.PushLogLine("lineC", now, nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("lineD", now, nil, map[string]string{"job": "fake"}))
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

func TestOTLPLogsIngestQuery(t *testing.T) {
	clu := cluster.New(nil, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// run initially the compactor, indexgateway, and distributor.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1s",
			"-compactor.retention-delete-delay=1s",
			// By default, a minute is added to the delete request start time. This compensates for that.
			"-compactor.delete-request-cancel-period=-60s",
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
			"-frontend.encoding=protobuf",
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
		require.NoError(t, cliDistributor.PushOTLPLogLine("lineA", now.Add(-45*time.Minute), map[string]any{"trace_id": 1, "user_id": "2"}))
		require.NoError(t, cliDistributor.PushOTLPLogLine("lineB", now.Add(-45*time.Minute), nil))

		require.NoError(t, cliDistributor.PushOTLPLogLine("lineC", now, map[string]any{"order.ids": []any{5, 6}}))
		require.NoError(t, cliDistributor.PushOTLPLogLine("lineD", now, nil))
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{service_name="varlog"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		numLinesReceived := 0
		for i, stream := range resp.Data.Stream {
			switch i {
			case 0:
				require.Len(t, stream.Values, 2)
				require.Equal(t, "lineD", stream.Values[0][1])
				require.Equal(t, "lineB", stream.Values[1][1])
				require.Equal(t, map[string]string{
					"service_name": "varlog",
				}, stream.Stream)
				numLinesReceived += 2
			case 1:
				require.Len(t, stream.Values, 1)
				require.Equal(t, "lineA", stream.Values[0][1])
				require.Equal(t, map[string]string{
					"service_name": "varlog",
					"trace_id":     "1",
					"user_id":      "2",
				}, stream.Stream)
				numLinesReceived++
			case 2:
				require.Len(t, stream.Values, 1)
				require.Equal(t, "lineC", stream.Values[0][1])
				require.Equal(t, map[string]string{
					"service_name": "varlog",
					"order_ids":    "[5,6]",
				}, stream.Stream)
				numLinesReceived++
			default:
				t.Errorf("unexpected case %d", i)
			}
		}
		require.Equal(t, 4, numLinesReceived)
	})
}

func TestCategorizedLabels(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})

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
			"-store.index-cache-read.embedded-cache.enabled=true",
		)
	)
	require.NoError(t, clu.Run())

	var (
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-ingester.flush-on-shutdown=true",
			"-ingester.wal-enabled=false",
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
			"-compactor.compaction-interval=1s",
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

	now = time.Now()
	require.NoError(t, cliDistributor.PushLogLine("lineA", now.Add(-1*time.Second), nil, map[string]string{"job": "fake"}))
	require.NoError(t, cliDistributor.PushLogLine("lineB", now.Add(-2*time.Second), map[string]string{"traceID": "123", "user": "a"}, map[string]string{"job": "fake"}))
	require.NoError(t, tIngester.Restart())
	require.NoError(t, cliDistributor.PushLogLine("lineC msg=foo", now.Add(-3*time.Second), map[string]string{"traceID": "456", "user": "b"}, map[string]string{"job": "fake"}))
	require.NoError(t, cliDistributor.PushLogLine("lineD msg=foo text=bar", now.Add(-4*time.Second), map[string]string{"traceID": "789", "user": "c"}, map[string]string{"job": "fake"}))

	type expectedStream struct {
		Stream            map[string]string
		Lines             []string
		CategorizedLabels []map[string]map[string]string
	}

	for _, tc := range []struct {
		name            string
		query           string
		encodingFlags   []string
		expectedStreams []expectedStream
	}{
		{
			name:  "no header - no parser ",
			query: `{job="fake"}`,
			expectedStreams: []expectedStream{
				{
					Stream: labels.FromStrings("job", "fake").Map(),
					Lines:  []string{"lineA"},
				},
				{
					Stream: map[string]string{
						"job":     "fake",
						"traceID": "123",
						"user":    "a",
					},
					Lines: []string{"lineB"},
				},
				{
					Stream: map[string]string{
						"job":     "fake",
						"traceID": "456",
						"user":    "b",
					},
					Lines: []string{"lineC msg=foo"},
				},
				{
					Stream: map[string]string{
						"job":     "fake",
						"traceID": "789",
						"user":    "c",
					},
					Lines: []string{"lineD msg=foo text=bar"},
				},
			},
		},
		{
			name:  "no header - with parser",
			query: `{job="fake"} | logfmt`,
			expectedStreams: []expectedStream{
				{
					Stream: map[string]string{
						"job": "fake",
					},
					Lines: []string{"lineA"},
				},
				{
					Stream: map[string]string{
						"job":     "fake",
						"traceID": "123",
						"user":    "a",
					},
					Lines: []string{"lineB"},
				},
				{
					Stream: map[string]string{
						"job":     "fake",
						"traceID": "456",
						"user":    "b",
						"msg":     "foo",
					},
					Lines: []string{"lineC msg=foo"},
				},
				{
					Stream: map[string]string{
						"job":     "fake",
						"traceID": "789",
						"user":    "c",
						"msg":     "foo",
						"text":    "bar",
					},
					Lines: []string{"lineD msg=foo text=bar"},
				},
			},
		},
		{
			name:  "with header - no parser ",
			query: `{job="fake"}`,
			encodingFlags: []string{
				string(httpreq.FlagCategorizeLabels),
			},
			expectedStreams: []expectedStream{
				{
					Stream: map[string]string{
						"job": "fake",
					},
					Lines: []string{"lineA", "lineB", "lineC msg=foo", "lineD msg=foo text=bar"},
					CategorizedLabels: []map[string]map[string]string{
						{},
						{
							"structuredMetadata": {
								"traceID": "123",
								"user":    "a",
							},
						},
						{
							"structuredMetadata": {
								"traceID": "456",
								"user":    "b",
							},
						},
						{
							"structuredMetadata": {
								"traceID": "789",
								"user":    "c",
							},
						},
					},
				},
			},
		},
		{
			name:  "with header - with parser",
			query: `{job="fake"} | logfmt`,
			encodingFlags: []string{
				string(httpreq.FlagCategorizeLabels),
			},
			expectedStreams: []expectedStream{
				{
					Stream: map[string]string{
						"job": "fake",
					},
					Lines: []string{"lineA", "lineB", "lineC msg=foo", "lineD msg=foo text=bar"},
					CategorizedLabels: []map[string]map[string]string{
						{},
						{
							"structuredMetadata": {
								"traceID": "123",
								"user":    "a",
							},
						},
						{
							"structuredMetadata": {
								"traceID": "456",
								"user":    "b",
							},
							"parsed": {
								"msg": "foo",
							},
						},
						{
							"structuredMetadata": {
								"traceID": "789",
								"user":    "c",
							},
							"parsed": {
								"msg":  "foo",
								"text": "bar",
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Add header with encoding flags and expect them to be returned in the response.
			var headers []client.Header
			var expectedEncodingFlags []string
			if len(tc.encodingFlags) > 0 {
				headers = append(headers, client.Header{Name: httpreq.LokiEncodingFlagsHeader, Value: strings.Join(tc.encodingFlags, httpreq.EncodeFlagsDelimiter)})
				expectedEncodingFlags = tc.encodingFlags
			}

			resp, err := cliQueryFrontend.RunQuery(context.Background(), tc.query, headers...)
			require.NoError(t, err)
			assert.Equal(t, "streams", resp.Data.ResultType)

			var streams []expectedStream
			for _, stream := range resp.Data.Stream {
				var lines []string
				var categorizedLabels []map[string]map[string]string

				for _, val := range stream.Values {
					lines = append(lines, val[1])

					var catLabels map[string]map[string]string
					if len(val) >= 3 && val[2] != "" {
						err = json.Unmarshal([]byte(val[2]), &catLabels)
						require.NoError(t, err)
						categorizedLabels = append(categorizedLabels, catLabels)
					}
				}

				streams = append(streams, expectedStream{
					Stream:            stream.Stream,
					Lines:             lines,
					CategorizedLabels: categorizedLabels,
				})
			}

			assert.ElementsMatch(t, tc.expectedStreams, streams)
			assert.ElementsMatch(t, expectedEncodingFlags, resp.Data.EncodingFlags)
		})
	}
}

func TestBloomFiltersEndToEnd(t *testing.T) {
	t.Skip("skipping until blooms have settled")
	commonFlags := []string{
		"-bloom-compactor.compaction-interval=10s",
		"-bloom-compactor.enable-compaction=true",
		"-bloom-compactor.enabled=true",
		"-bloom-gateway.enable-filtering=true",
		"-bloom-gateway.enabled=true",
		"-compactor.compaction-interval=1s",
		"-frontend.default-validity=0s",
		"-ingester.flush-on-shutdown=true",
		"-ingester.wal-enabled=false",
		"-query-scheduler.use-scheduler-ring=false",
		"-store.index-cache-read.embedded-cache.enabled=true",
		"-querier.split-queries-by-interval=24h",
	}

	tenantID := randStringRunes()

	clu := cluster.New(
		level.DebugValue(),
		cluster.SchemaWithTSDB,
		func(c *cluster.Cluster) { c.SetSchemaVer("v13") },
	)

	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tDistributor = clu.AddComponent(
			"distributor",
			append(
				commonFlags,
				"-target=distributor",
			)...,
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			append(
				commonFlags,
				"-target=index-gateway",
			)...,
		)
		tBloomGateway = clu.AddComponent(
			"bloom-gateway",
			append(
				commonFlags,
				"-target=bloom-gateway",
			)...,
		)
	)
	require.NoError(t, clu.Run())

	var (
		tIngester = clu.AddComponent(
			"ingester",
			append(
				commonFlags,
				"-target=ingester",
				"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			)...,
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			append(
				commonFlags,
				"-target=query-scheduler",
				"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			)...,
		)
		tCompactor = clu.AddComponent(
			"compactor",
			append(
				commonFlags,
				"-target=compactor",
				"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			)...,
		)
		tBloomCompactor = clu.AddComponent(
			"bloom-compactor",
			append(
				commonFlags,
				"-target=bloom-compactor",
				"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			)...,
		)
	)
	require.NoError(t, clu.Run())

	// finally, run the query-frontend and querier.
	var (
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			append(
				commonFlags,
				"-target=query-frontend",
				"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
				"-common.compactor-address="+tCompactor.HTTPURL(),
				"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			)...,
		)
		_ = clu.AddComponent(
			"querier",
			append(
				commonFlags,
				"-target=querier",
				"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
				"-common.compactor-address="+tCompactor.HTTPURL(),
				"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			)...,
		)
	)
	require.NoError(t, clu.Run())

	now := time.Now()

	cliDistributor := client.New(tenantID, "", tDistributor.HTTPURL())
	cliDistributor.Now = now

	cliIngester := client.New(tenantID, "", tIngester.HTTPURL())
	cliIngester.Now = now

	cliQueryFrontend := client.New(tenantID, "", tQueryFrontend.HTTPURL())
	cliQueryFrontend.Now = now

	cliIndexGateway := client.New(tenantID, "", tIndexGateway.HTTPURL())
	cliIndexGateway.Now = now

	cliBloomGateway := client.New(tenantID, "", tBloomGateway.HTTPURL())
	cliBloomGateway.Now = now

	cliBloomCompactor := client.New(tenantID, "", tBloomCompactor.HTTPURL())
	cliBloomCompactor.Now = now

	lineTpl := `caller=loki_micro_services_test.go msg="push log line" id="%s"`
	// ingest logs from 10 different pods
	// from now-60m to now-55m
	// each line contains a random, unique string
	// that string is used to verify filtering using bloom gateway
	uniqueStrings := make([]string, 5*60)
	for i := 0; i < len(uniqueStrings); i++ {
		id := randStringRunes()
		id = fmt.Sprintf("%s-%d", id, i)
		uniqueStrings[i] = id
		pod := fmt.Sprintf("pod-%d", i%10)
		line := fmt.Sprintf(lineTpl, id)
		err := cliDistributor.PushLogLine(
			line,
			now.Add(-1*time.Hour).Add(time.Duration(i)*time.Second),
			nil,
			map[string]string{"pod": pod},
		)
		require.NoError(t, err)
	}

	// restart ingester to flush chunks and that there are zero chunks in memory
	require.NoError(t, cliIngester.Flush())
	require.NoError(t, tIngester.Restart())

	// wait for compactor to compact index and for bloom compactor to build bloom filters
	require.Eventually(t, func() bool {
		// verify metrics that observe usage of block for filtering
		metrics, err := cliBloomCompactor.Metrics()
		require.NoError(t, err)
		successfulRunCount, labels, err := extractMetric(`loki_bloomcompactor_runs_completed_total`, metrics)
		if err != nil {
			return false
		}
		t.Log("bloom compactor runs", successfulRunCount, labels)
		if labels["status"] != "success" {
			return false
		}

		return successfulRunCount == 1
	}, 30*time.Second, time.Second)

	// use bloom gateway to perform needle in the haystack queries
	randIdx := rand.Intn(len(uniqueStrings))
	q := fmt.Sprintf(`{job="varlog"} |= "%s"`, uniqueStrings[randIdx])
	start := now.Add(-90 * time.Minute)
	end := now.Add(-30 * time.Minute)
	resp, err := cliQueryFrontend.RunRangeQueryWithStartEnd(context.Background(), q, start, end)
	require.NoError(t, err)

	// verify response
	require.Len(t, resp.Data.Stream, 1)
	expectedLine := fmt.Sprintf(lineTpl, uniqueStrings[randIdx])
	require.Equal(t, expectedLine, resp.Data.Stream[0].Values[0][1])

	// verify metrics that observe usage of block for filtering
	bloomGwMetrics, err := cliBloomGateway.Metrics()
	require.NoError(t, err)

	unfilteredCount := getMetricValue(t, "loki_bloom_gateway_chunkrefs_pre_filtering", bloomGwMetrics)
	require.Equal(t, float64(10), unfilteredCount)

	filteredCount := getMetricValue(t, "loki_bloom_gateway_chunkrefs_post_filtering", bloomGwMetrics)
	require.Equal(t, float64(1), filteredCount)

	mf, err := extractMetricFamily("loki_bloom_gateway_bloom_query_latency", bloomGwMetrics)
	require.NoError(t, err)

	count := getValueFromMetricFamilyWithFunc(mf, &dto.LabelPair{
		Name:  proto.String("status"),
		Value: proto.String("success"),
	}, func(m *dto.Metric) uint64 {
		return m.Histogram.GetSampleCount()
	})
	require.Equal(t, uint64(1), count)
}

func getValueFromMF(mf *dto.MetricFamily, lbs []*dto.LabelPair) float64 {
	return getValueFromMetricFamilyWithFunc(mf, lbs[0], func(m *dto.Metric) float64 { return m.Counter.GetValue() })
}

func getValueFromMetricFamilyWithFunc[R any](mf *dto.MetricFamily, lbs *dto.LabelPair, f func(*dto.Metric) R) R {
	eq := func(e *dto.LabelPair) bool {
		return e.GetName() == lbs.GetName() && e.GetValue() == lbs.GetValue()
	}
	var zero R
	for _, m := range mf.Metric {
		if !slices.ContainsFunc(m.GetLabel(), eq) {
			continue
		}
		return f(m)
	}
	return zero
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
