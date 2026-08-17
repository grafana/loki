//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// syncBuffer is a concurrency-safe buffer for capturing log output from the
// in-process cluster components, which all log through the global util_log.Logger.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// frontendMetricsLineWith returns the first captured frontend metrics.go log
// line that contains all of the given substrings, or "" if none matched.
func frontendMetricsLineWith(captured string, must ...string) string {
	for _, line := range strings.Split(captured, "\n") {
		if !strings.Contains(line, "component=frontend") {
			continue
		}
		ok := true
		for _, m := range must {
			if !strings.Contains(line, m) {
				ok = false
				break
			}
		}
		if ok {
			return line
		}
	}
	return ""
}

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

	// Capture the frontend's metrics.go log line: it is emitted through the global
	// util_log.Logger (pkg/querier/queryrange/stats.go), so redirecting that (while
	// still teeing to stderr) lets us assert on the actual line the user reads in
	// Grafana, not just the response stats it is derived from.
	var captured syncBuffer
	oldLogger := util_log.Logger
	util_log.Logger = log.NewSyncLogger(log.NewLogfmtLogger(io.MultiWriter(os.Stderr, &captured)))
	defer func() { util_log.Logger = oldLogger }()

	// Now the source-reducing metric query: sum() reduces labels at the source,
	// exactly the case the storage-level hook exists to cover. Poll until the
	// response carries the per-logical-stream shard timings.
	var shardedStreams []stats.ShardedStream
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
		shardedStreams = resp.Data.Statistics.ShardedStreams
		return true
	}, 60*time.Second, 2*time.Second, "expected the metric query response to report shard timings for {job=shardtimer}")

	// Derive the metrics.go frontend fields exactly as RecordRangeAndInstantQueryMetrics
	// does from response stats, and validate them on this real data. (The literal
	// log line can't be grepped from the in-process cluster; its formatting is
	// covered by the unit test TestRecordMetrics_FrontendUnshardedEstimate.)
	var (
		totalNanos int64 // sharded_total_duration
		maxAdded   int64 // unsharded_added_estimate = max_s(T_s - M_s)
		crit       stats.ShardedStream
		found      bool
	)
	for _, s := range shardedStreams {
		totalNanos += s.SumDurationNanos
		if added := s.SumDurationNanos - s.MaxDurationNanos; added > maxAdded {
			maxAdded, crit = added, s
		}
		if strings.Contains(s.Stream, `job="shardtimer"`) {
			found = true
			assert.NotContains(t, s.Stream, "__stream_shard__", "logical key must have the shard label stripped")
			assert.GreaterOrEqual(t, s.Shards, int64(2), "both physical shards should be attributed to the logical stream")
			assert.Greater(t, s.SumDurationNanos, int64(0), "read time should be attributed")
		}
	}
	require.True(t, found, "the logical stream {job=shardtimer} must appear in the shard timings")

	t.Logf("metrics.go fields: sharded_total_duration=%s unsharded_added_estimate=%s unsharded_critical_stream=%q unsharded_critical_shards=%d",
		time.Duration(totalNanos), time.Duration(maxAdded), crit.Stream, crit.Shards)

	// sharded_total_duration is always populated when any sharded stream was read.
	assert.Positive(t, totalNanos, "sharded_total_duration must be > 0")
	// unsharded_added_estimate is >= 0; it is only > 0 when a stream's shards were
	// spread across subqueries (data-dependent, so not hard-required). When it is,
	// the critical stream must be a real sharded one.
	assert.GreaterOrEqual(t, maxAdded, int64(0), "unsharded_added_estimate must be non-negative")
	if maxAdded > 0 {
		assert.NotEmpty(t, crit.Stream, "a positive estimate must name a critical stream")
		assert.GreaterOrEqual(t, crit.Shards, int64(2), "the critical stream must have >= 2 shards")
	}

	// Finally, assert the actual frontend metrics.go log line carries all the
	// shard-timing fields for our query. sharded_total_duration is only logged
	// when the block fires (ShardedStreams non-empty), so it identifies the line.
	line := frontendMetricsLineWith(captured.String(), "shardtimer", "sharded_total_duration=")
	require.NotEmpty(t, line, "no frontend metrics.go line with shard-timing fields was logged for the query")
	for _, field := range []string{
		"unsharded_added_estimate=",
		"unsharded_added_pct=",
		"unsharded_critical_stream",
		"unsharded_critical_total=",
		"unsharded_critical_maxshard=",
		"unsharded_critical_shards=",
		"sharded_total_duration=",
	} {
		assert.Contains(t, line, field, "frontend metrics.go line is missing a shard-timing field")
	}
}
