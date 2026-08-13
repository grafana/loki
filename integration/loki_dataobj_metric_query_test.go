//go:build integration

package integration

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestMetricQueryFromDataObjects runs a stream-first metric query end to end across all three query
// sources for two tenants and asserts each tenant sees only its own per-stream counts. Both tenants
// write the same two streams ({job="fake", app="foo"} and app="bar") into all three tiers; each line's
// timestamp places it in exactly one tier:
//
//   - ingester    [now-15m, now]           recent lines, still in memory (never flushed).
//   - chunk store (now-45m, now-15m]        older lines, flushed and index-synced.
//   - data object [storage-start, now-45m]  oldest lines, built directly into the object store.
//
// The oldest tier is a single data object holding both tenants' streams (a multi-tenant object). Its
// index registers under each tenant's metastore ToC section, so a per-tenant query must read only its
// own sections out of the shared object. This test is the end-to-end proof of that read isolation. The
// per-stream counts differ per tenant, so a cross-tenant leak would change a total and fail the assertion.
//
// The bands come from two knobs: the bounded ingester/store split (ingester.query-store-max-look-back-
// period=15m) puts everything older than 15m on the store, and the data-object availability lag
// (query-engine.storage-lag=45m) puts the store's [.., now-45m] slice on data objects and the recent
// (now-45m, now-15m] slice back on the chunk store. The ingester and chunk store may hold overlapping
// data (the querier deduplicates across them), which is why the split keeps them query-disjoint here;
// the data-object reader does not deduplicate, so the routing keeps its band strictly older than both.
//
// Ingestion uses the Kafka path (distributor -> Kafka -> ingester), like TestDedupMicroServicesKafka.
// Because that path is asynchronous, a single-partition sentinel pushed after both tenants' chunk-store
// lines gates the flush: once the sentinel is queryable, offset order guarantees the earlier lines were
// consumed. The oldest tier's data objects, their index, and the metastore ToC are produced with the
// production build components: the dataobj-consumer and dataobj-index-builder cannot run inside the
// in-process harness because they register the loki_dataobj_encoding_* metrics with inconsistent label
// sets (consumer {partition}, index-builder {topic, component}), which collides on the shared registry.
func TestMetricQueryFromDataObjects(t *testing.T) {
	// Single-partition fake Kafka broker, so all records are consumed in one total (offset) order; the
	// sentinel barrier below relies on that.
	kafkaCluster, _ := testkafka.CreateCluster(t, 1, "loki")
	kafkaAddr := kafkaCluster.ListenAddrs()[0]

	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	t.Cleanup(func() { assert.NoError(t, clu.Cleanup()) })

	// Shared by the distributor and ingester so the Kafka ingestion path owns an active partition.
	kafkaIngestionFlags := []string{
		"-kafka.writer.address=" + kafkaAddr,
		"-kafka.reader.address=" + kafkaAddr,
		"-kafka.topic=loki",
		"-ingester.kafka-ingestion-enabled=true",
		"-ingester.partition-ring.store=inmemory",
	}

	// Batch 1: compactor + index-gateway (the querier reads flushed chunks through the gateway).
	var (
		tCompactor    = clu.AddComponent("compactor", "-target=compactor", "-compactor.compaction-interval=1h")
		tIndexGateway = clu.AddComponent("index-gateway", "-target=index-gateway")
	)
	require.NoError(t, clu.Run())

	// Batch 2: distributor (Kafka writes) + ingester (Kafka ingestion).
	var (
		tDistributor = clu.AddComponent("distributor", append([]string{
			"-target=distributor",
			"-distributor.kafka-writes-enabled=true",
			"-distributor.ingester-writes-enabled=false",
		}, kafkaIngestionFlags...)...)
		tIngester = clu.AddComponent("ingester", append([]string{
			"-target=ingester",
			"-ingester.lifecycler.ID=ingester-0", // deterministic partition id 0
			"-ingester.partition-ring.min-partition-owners-duration=0s",
			"-ingester.chunks-retain-period=1h", // keep flushed chunks in memory so recent stays queryable
			"-kafka.max-consumer-lag-at-startup=1s",
			"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
		}, kafkaIngestionFlags...)...)
	)
	require.NoError(t, clu.Run())

	// Batch 3: query scheduler.
	tScheduler := clu.AddComponent("query-scheduler",
		"-target=query-scheduler",
		"-query-scheduler.use-scheduler-ring=false",
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
	)
	require.NoError(t, clu.Run())

	// Batch 4: querier (data-object reader on) + query-frontend.
	dataObjFlags := []string{
		"-dataobj.enabled=true",
		"-dataobj-storage-bucket-prefix=",          // flat layout: log objects at the object-store root
		"-dataobj-metastore.index-storage-prefix=", // flat layout: indexes + ToC at the object-store root
		"-dataobj-metastore.read-postings-sections=true",
		"-querier.engine.stream-ordered-execution-enabled=true",
		"-querier.engine.dataobjects-reader-enabled=true",
		"-ingester.query-store-max-look-back-period=15m", // ingester serves the last 15m, the store the rest
		"-query-engine.storage-lag=45m",                  // data objects serve up to now-45m; (now-45m, now-15m] stays on chunks
		"-query-engine.storage-start-date=2020-01-01",
	}
	var (
		tQuerier = clu.AddComponent("querier", append([]string{
			"-target=querier",
			"-querier.scheduler-address=" + tScheduler.GRPCURL(),
			"-common.compactor-address=" + tCompactor.HTTPURL(),
			"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
		}, dataObjFlags...)...)
		tFrontend = clu.AddComponent("query-frontend",
			"-target=query-frontend",
			"-frontend.scheduler-address="+tScheduler.GRPCURL(),
			"-frontend.default-validity=0s",
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
	)
	require.NoError(t, clu.Run())

	var (
		ctx = context.Background()
		now = time.Now()
		at  = func(minutes int) time.Time { return now.Add(-time.Duration(minutes) * time.Minute) }
	)

	foo := map[string]string{"job": "fake", "app": "foo"}
	bar := map[string]string{"job": "fake", "app": "bar"}

	// Per-tenant, per-tier line counts for each stream. The counts are distinct so a total that mixes
	// another tenant's lines would not match the expected sum.
	type counts struct{ ingester, chunk, dataobj int }
	tenants := []struct {
		id       string
		foo, bar counts
	}{
		{id: randStringRunes(), foo: counts{2, 3, 4}, bar: counts{1, 1, 1}},
		{id: randStringRunes(), foo: counts{1, 2, 1}, bar: counts{2, 1, 3}},
	}

	// entries returns n log entries at at(baseMin+i), one per minute, with unique lines. The baseMin
	// per tier keeps every timestamp inside that tier's band.
	entries := func(prefix string, baseMin, n int) []push.Entry {
		out := make([]push.Entry, n)
		for i := 0; i < n; i++ {
			out[i] = push.Entry{Timestamp: at(baseMin + i), Line: fmt.Sprintf("%s-%d", prefix, i)}
		}
		return out
	}

	pushLines := func(cli *client.Client, stream map[string]string, prefix string, baseMin, n int) {
		for i := 0; i < n; i++ {
			require.NoError(t, cli.PushLogLine(fmt.Sprintf("%s-%d", prefix, i), at(baseMin+i), nil, stream))
		}
	}

	dist := map[string]*client.Client{}
	front := map[string]*client.Client{}
	for _, tn := range tenants {
		dist[tn.id] = client.New(tn.id, "", tDistributor.HTTPURL())
		front[tn.id] = client.New(tn.id, "", tFrontend.HTTPURL())
		front[tn.id].Now = now
	}
	cliIngester := client.New(tenants[0].id, "", tIngester.HTTPURL())
	cliIndexGateway := client.New("", "", tIndexGateway.HTTPURL())

	syncIndexes := func() {
		require.Eventually(t, func() bool {
			started, err := cliIndexGateway.TriggerSyncIndexes()
			return err == nil && started
		}, 10*time.Second, 50*time.Millisecond, "a manual index sync should be accepted")
		require.Eventually(t, func() bool {
			inProgress, err := cliIndexGateway.SyncIndexesInProgress()
			return err == nil && !inProgress
		}, 30*time.Second, 100*time.Millisecond, "the index sync should complete")
	}

	// Oldest tier: build one data object holding both tenants' (now-45m and older) streams directly into
	// the object store the querier resolves via getDataObjBucket ("store-1" named filesystem store at
	// <shared>/fs-store-1). The single object's index registers under each tenant's ToC section.
	dataObjByTenant := map[string][]logproto.Stream{}
	for _, tn := range tenants {
		var streams []logproto.Stream
		if tn.foo.dataobj > 0 {
			streams = append(streams, logproto.Stream{Labels: labels.FromMap(foo).String(), Entries: entries(tn.id+"-fo", 90, tn.foo.dataobj)})
		}
		if tn.bar.dataobj > 0 {
			streams = append(streams, logproto.Stream{Labels: labels.FromMap(bar).String(), Entries: entries(tn.id+"-bo", 90, tn.bar.dataobj)})
		}
		dataObjByTenant[tn.id] = streams
	}
	buildDataObjectsInStore(ctx, t, filepath.Join(tQuerier.ClusterSharedPath(), "fs-store-1"), dataObjByTenant)

	// Wait until the distributor can produce to an active partition before the real pushes. The warmup
	// stream is never queried, so its retries are harmless.
	require.Eventually(t, func() bool {
		return dist[tenants[0].id].PushLogLine("warmup", now, nil, map[string]string{"job": "warmup"}) == nil
	}, 60*time.Second, 250*time.Millisecond, "distributor should be able to produce to an active partition")

	// Middle tier: push each tenant's (now-45m, now-15m] lines. A sentinel pushed last (single partition,
	// offset order) becoming queryable proves those lines were consumed, so the flush below captures them all.
	for _, tn := range tenants {
		pushLines(dist[tn.id], foo, tn.id+"-fm", 30, tn.foo.chunk)
		pushLines(dist[tn.id], bar, tn.id+"-bm", 30, tn.bar.chunk)
	}

	require.NoError(t, dist[tenants[0].id].PushLogLine("sentinel", at(1), nil, map[string]string{"job": "sentinel"}))
	require.Eventually(t, func() bool {
		resp, err := front[tenants[0].id].RunQuery(ctx, `count_over_time({job="sentinel"}[3h])`)
		return err == nil && resp.Data.ResultType == "vector" && len(resp.Data.Vector) == 1
	}, 60*time.Second, 500*time.Millisecond, "the middle-tier lines must be consumed before the flush")

	require.NoError(t, cliIngester.Flush())
	syncIndexes()

	// Recent tier: pushed after the flush so it stays in the ingester's memory.
	for _, tn := range tenants {
		pushLines(dist[tn.id], foo, tn.id+"-fr", 5, tn.foo.ingester)
		pushLines(dist[tn.id], bar, tn.id+"-br", 5, tn.bar.ingester)
	}

	for _, tn := range tenants {
		expected := map[string]float64{
			labels.FromMap(foo).String(): float64(tn.foo.ingester + tn.foo.chunk + tn.foo.dataobj),
			labels.FromMap(bar).String(): float64(tn.bar.ingester + tn.bar.chunk + tn.bar.dataobj),
		}
		cli := front[tn.id]

		var (
			got          map[string]float64
			dataObjBytes int64
		)
		require.Eventually(t, func() bool {
			resp, err := cli.RunQuery(ctx, `count_over_time({job="fake"}[3h])`)
			if err != nil {
				t.Logf("metric query error for tenant %s: %v", tn.id, err)
				return false
			}
			if resp.Data.ResultType != "vector" {
				return false
			}
			got = map[string]float64{}
			for _, s := range resp.Data.Vector {
				v, err := strconv.ParseFloat(s.Value, 64)
				if err != nil {
					return false
				}
				got[labels.FromMap(s.Metric).String()] = v
			}

			// The [3h] window spans the data-object band, so the response's data-object byte stats are
			// populated by the data-object reader.
			dataObjBytes = resp.Data.Statistics.Querier.Store.Dataobj.PrePredicateDecompressedBytes

			return maps.Equal(got, expected)
		}, 60*time.Second, time.Second, "tenant %s metric query should sum the counts from the ingester, chunk store, and data objects", tn.id)

		require.Equal(t, expected, got, "tenant %s", tn.id)

		// Data-object reads must report processed bytes.
		require.Positive(t, dataObjBytes, "tenant %s data-object reads should report processed bytes", tn.id)
	}

	// The querier exports per-component data-object byte counters for the reads above.
	querierMetrics, err := client.New("", "", tQuerier.HTTPURL()).Metrics()
	require.NoError(t, err)
	for _, name := range []string{
		"loki_querier_dataobj_fetched_compressed_bytes_total",
		"loki_querier_dataobj_processed_uncompressed_bytes_total",
	} {
		total, err := sumCounter(name, querierMetrics)
		require.NoError(t, err, "parsing querier metrics")
		require.Positivef(t, total, "metric %s must be exported and positive after data-object queries", name)
	}
}

// buildDataObjectsInStore builds one logs data object holding every tenant's streams, indexes it, and
// registers it in the metastore table of contents under each tenant — all on a filesystem bucket at dir,
// mirroring what the dataobj-consumer and dataobj-index-builder produce. The per-append received-time is
// unused here (it only feeds GetEarliestRecordTime); the object's index time range comes from the entry
// timestamps, so a fixed epoch value is passed.
func buildDataObjectsInStore(ctx context.Context, t *testing.T, dir string, tenantStreams map[string][]logproto.Stream) {
	t.Helper()

	bucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err)
	up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          1 << 20,
		TargetObjectSize:        10 << 20,
		TargetSectionSize:       1 << 20,
		BufferSize:              1 << 20,
		SectionStripeMergeLimit: 2,
	}

	logsBuilder, err := logsobj.NewBuilder(logsobj.BuilderConfig{BuilderBaseConfig: cfg}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)
	for tenant, streams := range tenantStreams {
		for _, s := range streams {
			require.NoError(t, logsBuilder.Append(tenant, s, time.Unix(0, 0)))
		}
	}
	logsObj, logsCloser, err := logsBuilder.Flush()
	require.NoError(t, err)
	logsPath, err := up.Upload(ctx, logsObj)
	require.NoError(t, err)
	require.NoError(t, logsCloser.Close())

	idxBuilder, err := indexobj.NewBuilder(cfg, nil)
	require.NoError(t, err)
	calc := index.NewCalculator(idxBuilder)
	logsRO, err := dataobj.FromBucket(ctx, bucket, logsPath, 0)
	require.NoError(t, err)
	require.NoError(t, calc.Calculate(ctx, log.NewNopLogger(), logsRO, logsPath))

	idxObj, idxCloser, timeRanges, err := calc.Flush()
	require.NoError(t, err)
	idxPath, err := up.Upload(ctx, idxObj)
	require.NoError(t, err)
	require.NoError(t, idxCloser.Close())

	toc := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	require.NoError(t, toc.WriteEntry(ctx, idxPath, timeRanges))
}
