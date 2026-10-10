//go:build integration

package integration

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	dskit_metrics "github.com/grafana/dskit/metrics"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestSampleQueryStreamDataObjEquivalence ingests the same data through the real distributor and
// Kafka, then reads it back two ways: once from the chunk store, after a flush, as the baseline;
// once from data objects, built from the same Kafka records by a dataobj-builder, routed there by
// the querier's stream-first data-object band. The two must return identical results.
func TestSampleQueryStreamDataObjEquivalence(t *testing.T) {
	// Single-partition fake Kafka, so the ingester and the dataobj-builder consume records in
	// the same order.
	kafkaCluster, _ := testkafka.CreateCluster(t, 1, "loki")
	kafkaAddr := kafkaCluster.ListenAddrs()[0]

	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer(fmt.Sprintf("v%d", config.LatestSchemaVersion))
	})
	t.Cleanup(func() {
		assert.NoError(t, clu.Cleanup())
	})

	kafkaIngestionFlags := []string{
		"-kafka.writer.address=" + kafkaAddr,
		"-kafka.reader.address=" + kafkaAddr,
		"-kafka.topic=loki",
		"-ingester.kafka-ingestion-enabled=true",
		"-ingester.partition-ring.store=inmemory",
	}

	// Start compactor and index-gateway.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1h",
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
		)
	)
	require.NoError(t, clu.Run())

	// Start distributor, ingester, and dataobj-builder, all reading the same Kafka topic.
	var (
		tDistributor = clu.AddComponent(
			"distributor",
			append([]string{
				"-target=distributor",
				"-distributor.kafka-writes-enabled=true",
				"-distributor.ingester-writes-enabled=false",
			}, kafkaIngestionFlags...)...,
		)
		tIngester = clu.AddComponent(
			"ingester",
			append([]string{
				"-target=ingester",
				"-ingester.lifecycler.ID=ingester-0", // deterministic partition id 0
				"-ingester.partition-ring.min-partition-owners-duration=0s",
				"-kafka.max-consumer-lag-at-startup=1s",
				"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
			}, kafkaIngestionFlags...)...,
		)
		_ = clu.AddComponent(
			"dataobj-builder",
			"-target=dataobj-builder",
			"-kafka.reader.address="+kafkaAddr,
			"-dataobj.enabled=true",
			"-dataobj.builder.topic=loki",
			"-dataobj.builder.partition-id=0", // the fake Kafka's only partition
			"-dataobj.builder.idle-flush-timeout=1s",
			"-dataobj.builder.max-builder-age=1s",
		)
	)
	require.NoError(t, clu.Run())

	// Start query-scheduler.
	tQueryScheduler := clu.AddComponent(
		"query-scheduler",
		"-target=query-scheduler",
		"-query-scheduler.use-scheduler-ring=false",
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
	)
	require.NoError(t, clu.Run())

	// Start querier and query-frontend. Store-only and stream-first are enabled from the start:
	// store-only only affects whether ingesters are consulted, not which store is picked, so there's
	// no need to restart for it. The -dataobj.* flags come later, once the object exists, so the
	// first query below is a genuine chunks-only baseline.
	var (
		tQuerier = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-querier.query-store-only=true",
			"-querier.stream-first-execution-enabled=true",
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			"-target=query-frontend",
			"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-frontend.default-validity=0s",
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
	)
	require.NoError(t, clu.Run())

	var (
		ctx    = context.Background()
		tenant = randStringRunes()
		now    = time.Now()

		tsA1 = now.Add(-6 * time.Minute)
		tsA2 = now.Add(-5 * time.Minute)
		tsA3 = now.Add(-4 * time.Minute)
		tsB1 = now.Add(-3 * time.Minute)
		tsB2 = now.Add(-2 * time.Minute)
	)

	cliDistributor := client.New(tenant, "", tDistributor.HTTPURL())
	cliFrontend := client.New(tenant, "", tQueryFrontend.HTTPURL())
	cliFrontend.Now = now
	cliIngester := client.New(tenant, "", tIngester.HTTPURL())
	cliIndexGateway := client.New("", "", tIndexGateway.HTTPURL())

	streamLabels := func(app string) map[string]string { return map[string]string{"job": "dataobjit", "app": app} }

	// Grouping by every fixture label keeps one output series per stream.
	const query = `sum by (job, app) (count_over_time({job="dataobjit"}[1h]))`
	expected := map[string]float64{
		labels.FromMap(streamLabels("a")).String(): 3,
		labels.FromMap(streamLabels("b")).String(): 2,
	}

	// runQuery and countsOf return plain errors, not require, so the Eventually loops below can
	// call them from their own goroutine.
	runQuery := func() (*client.Response, error) {
		return cliFrontend.RunQuery(ctx, query)
	}
	countsOf := func(resp *client.Response) (map[string]float64, error) {
		if resp.Data.ResultType != "vector" {
			return nil, fmt.Errorf("unexpected result type %q", resp.Data.ResultType)
		}
		got := map[string]float64{}
		for _, s := range resp.Data.Vector {
			v, err := strconv.ParseFloat(s.Value, 64)
			if err != nil {
				return nil, err
			}
			got[labels.FromMap(s.Metric).String()] = v
		}
		return got, nil
	}

	// Wait until the distributor can produce to an ACTIVE partition before pushing the real data.
	require.Eventually(t, func() bool {
		return cliDistributor.PushLogLine("__warmup__", now, nil, map[string]string{"job": "warmup"}) == nil
	}, 60*time.Second, 250*time.Millisecond, "distributor should be able to produce to an active partition")

	require.NoError(t, cliDistributor.PushLogLine("a1", tsA1, nil, streamLabels("a")))
	require.NoError(t, cliDistributor.PushLogLine("a2", tsA2, nil, streamLabels("a")))
	require.NoError(t, cliDistributor.PushLogLine("a3", tsA3, nil, streamLabels("a")))
	require.NoError(t, cliDistributor.PushLogLine("b1", tsB1, nil, streamLabels("b")))
	require.NoError(t, cliDistributor.PushLogLine("b2", tsB2, nil, streamLabels("b")))

	t.Run("a query served by the chunk store returns the complete expected result and reads chunks", func(t *testing.T) {
		// Flush to chunks, sync the index, and query, retrying until the result matches. The
		// querier is store-only, so there's no direct way to check Kafka-consumption completeness;
		// retrying the flush proves it instead, since a flush before the ingester consumes every record
		// would produce a partial result that could never match.
		var resp *client.Response
		require.Eventually(t, func() bool {
			if err := cliIngester.FlushTenant(""); err != nil {
				t.Logf("flush: %v", err)
				return false
			}
			// Best effort: ignore a sync already running; the next retry re-triggers it.
			if _, err := cliIndexGateway.TriggerSyncIndexes(); err != nil {
				t.Logf("sync trigger: %v", err)
				return false
			}
			r, err := runQuery()
			if err != nil {
				t.Logf("baseline query: %v", err)
				return false
			}
			counts, err := countsOf(r)
			if err != nil {
				t.Logf("baseline counts: %v", err)
				return false
			}
			resp = r
			return reflect.DeepEqual(counts, expected)
		}, 60*time.Second, 500*time.Millisecond, "the chunk store should eventually serve the complete expected result")

		counts, err := countsOf(resp)
		require.NoError(t, err)
		require.Equal(t, expected, counts)
		require.Positive(t, resp.Data.Statistics.Querier.Store.Chunk.DecompressedLines)
	})

	t.Run("a query served by data objects returns the complete expected result, reports the rows, bytes and section resolution time, and reads no chunks and no second-stage rows", func(t *testing.T) {
		tQuerier.AddFlags(
			"-dataobj.enabled=true",
			"-querier.dataobj-query-start-time="+now.Add(-48*time.Hour).UTC().Format(time.RFC3339),
			"-dataobj.storage-lag=0s",
			"-dataobj.metadata-cache.embedded-cache.enabled=true",
		)
		require.NoError(t, tQuerier.Restart())

		// The dataobj-builder flushes and uploads on its own (idle-flush-timeout/max-builder-age are set
		// short above); there's no force-flush endpoint. The metastore does an uncached lookup per
		// request, so poll the query itself rather than any builder-internal state.
		var resp *client.Response
		require.Eventually(t, func() bool {
			r, err := runQuery()
			if err != nil {
				t.Logf("data-object query: %v", err)
				return false
			}
			counts, err := countsOf(r)
			if err != nil {
				t.Logf("data-object counts: %v", err)
				return false
			}
			resp = r
			return reflect.DeepEqual(counts, expected)
		}, 60*time.Second, 500*time.Millisecond, "the data-object store should eventually serve the complete expected result")

		counts, err := countsOf(resp)
		require.NoError(t, err)
		require.Equal(t, expected, counts)

		store := resp.Data.Statistics.Querier.Store
		require.Positive(t, store.Dataobj.PrePredicateDecompressedRows)
		require.Positive(t, store.Dataobj.PrePredicateDecompressedBytes)
		require.Positive(t, store.Dataobj.SectionsResolutionMaxTime)
		require.Zero(t, store.Dataobj.PostFilterRows)
		require.Zero(t, store.Chunk.DecompressedLines)
	})

	t.Run("a further query served by data objects hits the metadata cache because it opens the same object again", func(t *testing.T) {
		_, err := runQuery()
		require.NoError(t, err)

		metrics, err := client.New("", "", tQuerier.HTTPURL()).Metrics()
		require.NoError(t, err)
		require.Positive(t, getMetricValue(t, "loki_dataobj_metadata_cache_hits_total", metrics))
	})

	t.Run("a line-filter query served by data objects returns the matching stream and reports the second-stage statistics because it reads the line column", func(t *testing.T) {
		resp, err := cliFrontend.RunQuery(ctx, `sum by (job, app) (count_over_time({job="dataobjit"} |= "a" [1h]))`)
		require.NoError(t, err)

		counts, err := countsOf(resp)
		require.NoError(t, err)
		require.Equal(t, map[string]float64{labels.FromMap(streamLabels("a")).String(): 3}, counts)

		dataobjStats := resp.Data.Statistics.Querier.Store.Dataobj
		require.Positive(t, dataobjStats.PrePredicateDecompressedRows)
		require.Positive(t, dataobjStats.PrePredicateDecompressedBytes)
		require.Positive(t, dataobjStats.PostPredicateDecompressedBytes)
		require.Positive(t, dataobjStats.PostFilterRows)
		require.Positive(t, dataobjStats.SectionsResolutionMaxTime)
	})

	t.Run("after the queries the querier counts object-store requests and bytes for the metastore and the streams reader, and none under the other component label", func(t *testing.T) {
		metrics, err := client.New("", "", tQuerier.HTTPURL()).Metrics()
		require.NoError(t, err)

		families, err := parseMetricFamilies(metrics)
		require.NoError(t, err)
		seriesValue := func(name string, labelNamesAndValues ...string) float64 {
			series := dskit_metrics.FindMetricsInFamilyMatchingLabels(families[name], labelNamesAndValues...)
			require.Len(t, series, 1, "series of %s with labels %v", name, labelNamesAndValues)
			return series[0].GetCounter().GetValue()
		}

		const requests = "loki_querier_dataobj_object_store_requests_total"
		const bytes = "loki_querier_dataobj_fetched_compressed_bytes_total"
		require.Positive(t, seriesValue(requests, "component", "metastore", "operation", "get"))
		require.Positive(t, seriesValue(requests, "component", "metastore", "operation", "get_range"))
		require.Positive(t, seriesValue(requests, "component", "streams-reader", "operation", "get_range"))
		require.Positive(t, seriesValue(bytes, "component", "metastore"))
		require.Positive(t, seriesValue(bytes, "component", "streams-reader"))
		for _, operation := range []string{"attributes", "get", "get_range"} {
			require.Zero(t, seriesValue(requests, "component", "other", "operation", operation))
		}
		require.Zero(t, seriesValue(bytes, "component", "other"))
	})
}
