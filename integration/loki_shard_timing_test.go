//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

// TestMicroServicesShardTimingStats verifies the temporary stream-sharding
// instrumentation end-to-end: a source-reducing metric query over
// __stream_shard__ streams, read from the store and query-sharded, must report
// per-logical-stream shard timings in the response stats. This is the path that
// was previously uncovered (metric queries reduce labels at the source, and the
// outer query's empty tracker clobbered the downstream shards' contributions).
func TestMicroServicesShardTimingStats(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDBAndTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() { assert.NoError(t, clu.Cleanup()) }()

	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1s",
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
			"-tsdb.shipper.resync-interval=250ms",
		)
		tDistributor = clu.AddComponent(
			"distributor",
			"-target=distributor",
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
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
	)
	require.NoError(t, clu.Run())

	// query-store-only forces reads through the (instrumented) store path rather
	// than the ingesters. A small tsdb-max-bytes-per-shard makes the query planner
	// shard even this tiny dataset, exercising the downstream-shard merge path.
	clu.AddComponent(
		"querier",
		"-target=querier",
		"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-querier.query-store-only=true",
		"-querier.tsdb-max-bytes-per-shard=256",
		"-tsdb.shipper.resync-interval=250ms",
		"-common.compactor-address="+tCompactor.HTTPURL(),
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
	)
	require.NoError(t, clu.Run())

	tQueryFrontend := clu.AddComponent(
		"query-frontend",
		"-target=query-frontend",
		"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-frontend.encoding=protobuf",
		"-querier.query-store-only=true",
		"-querier.tsdb-max-bytes-per-shard=256",
		"-common.compactor-address="+tCompactor.HTTPURL(),
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
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

	// Push two physical shards of one logical stream {job="shardtimer"}.
	for _, shard := range []string{"0", "1"} {
		lbls := map[string]string{"job": "shardtimer", "__stream_shard__": shard}
		for i := 0; i < 20; i++ {
			require.NoError(t, cliDistributor.PushLogLine("payload line for shard timing", now.Add(-time.Duration(i+1)*time.Second), nil, lbls))
		}
	}

	// Flush to the store so the query-store-only reads hit the instrumented path.
	require.NoError(t, cliIngester.FlushTenant(`{job="shardtimer"}`))

	start := now.Add(-30 * time.Minute)
	end := now.Add(time.Minute)

	// First wait for the flushed data to become queryable from the store.
	require.Eventually(t, func() bool {
		resp, err := cliQueryFrontend.RunRangeQueryWithStartEnd(context.Background(), `{job="shardtimer"}`, start, end)
		if err != nil {
			t.Logf("log query error: %v", err)
			return false
		}
		var n int
		for _, s := range resp.Data.Stream {
			n += len(s.Values)
		}
		if n == 0 {
			t.Logf("no store data yet")
		}
		return n > 0
	}, 60*time.Second, 1*time.Second, "flushed data never became queryable from the store")

	// Now the source-reducing metric query: sum() reduces labels at the source,
	// exactly the case the storage-level hook exists to cover. Assert it reports
	// per-logical-stream shard timings.
	require.Eventually(t, func() bool {
		resp, err := cliQueryFrontend.RunRangeQueryWithStartEnd(context.Background(), `sum(count_over_time({job="shardtimer"}[5m]))`, start, end)
		if err != nil {
			t.Logf("metric query error: %v", err)
			return false
		}
		if len(resp.Data.Statistics.ShardedStreams) == 0 {
			t.Logf("no sharded streams yet (matrix_len=%d shards=%d)", len(resp.Data.Matrix), resp.Data.Statistics.Summary.Shards)
			return false
		}

		var found bool
		for _, s := range resp.Data.Statistics.ShardedStreams {
			// The shard label is stripped, so the two physical shards collapse
			// into one logical stream keyed by the remaining labels.
			if strings.Contains(s.Stream, `job="shardtimer"`) {
				found = true
				assert.NotContains(t, s.Stream, "__stream_shard__", "logical key must have the shard label stripped")
				assert.GreaterOrEqual(t, s.Shards, int64(2), "both physical shards should be attributed to the logical stream")
				assert.Greater(t, s.SumDurationNanos, int64(0), "read time should be attributed")
			}
		}
		return found
	}, 60*time.Second, 2*time.Second, "expected the metric query response to report shard timings for {job=shardtimer}")
}
