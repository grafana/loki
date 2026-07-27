//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// TestDedupMicroServicesKafka verifies query-time log line deduplication over the
// Kafka-based ingestion path, and asserts the behaviour is identical whether the
// data is served from the ingester (in-memory) or from the chunk store (after a
// flush + TSDB index build + querier discovery).
func TestDedupMicroServicesKafka(t *testing.T) {
	// Fake Kafka broker with a single partition, so all records are consumed in a
	// single total order (the sentinel barrier below relies on this).
	kafkaCluster, _ := testkafka.CreateCluster(t, 1, "loki")
	kafkaAddr := kafkaCluster.ListenAddrs()[0]

	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
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

	// Batch 1: start compactor and index-gateway
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

	// Batch 2: start distributor and ingester.
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
	)
	require.NoError(t, clu.Run())

	// Batch 3: start the query scheduler.
	tQueryScheduler := clu.AddComponent(
		"query-scheduler",
		"-target=query-scheduler",
		"-query-scheduler.use-scheduler-ring=false",
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
	)
	require.NoError(t, clu.Run())

	// Batch 4: start the querier and query-frontend.
	var (
		tQuerier = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
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

		// Per-case timestamps, all within the default 3h query-ingesters-within window.
		tsC1a = now.Add(-6 * time.Minute)
		tsC1b = now.Add(-5 * time.Minute)
		tsC2  = now.Add(-4 * time.Minute)
		tsC3  = now.Add(-3 * time.Minute)
		tsC4  = now.Add(-2 * time.Minute)
		tsC5  = now.Add(-1 * time.Minute)
		tsSen = now.Add(-30 * time.Second)

		// Explicit query window covering all pushed timestamps.
		queryStart = now.Add(-24 * time.Hour)
		queryEnd   = now.Add(time.Minute)
	)

	const sentinelLine = "__sentinel__"

	cliDistributor := client.New(tenant, "", tDistributor.HTTPURL())
	cliFrontend := client.New(tenant, "", tQueryFrontend.HTTPURL())
	ingesterCli := client.New(tenant, "", tIngester.HTTPURL())
	indexGatewayCli := client.New("", "", tIndexGateway.HTTPURL())

	structuredMetadata := func(a string) map[string]string { return map[string]string{"a": a} }
	streamLabels := func(kv ...string) map[string]string {
		m := map[string]string{"job": "fake"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}

	rangeQuery := func(query string, headers ...client.Header) (*client.Response, error) {
		return cliFrontend.RunRangeQueryWithStartEnd(ctx, query, queryStart, queryEnd, headers...)
	}

	// waitForLogLine blocks until line is queryable for query. The polling closure
	// must not call require, since testify runs it on a separate goroutine.
	waitForLogLine := func(query, line string) {
		require.Eventually(t, func() bool {
			resp, err := rangeQuery(query)
			if err != nil {
				t.Logf("waitForLogLine %q: query error: %v", query, err)
				return false
			}
			return slices.Contains(linesFromResponse(resp), line)
		}, 60*time.Second, 500*time.Millisecond, "line %q must be queryable for %q", line, query)
	}

	waitForLogSentinel := func() {
		waitForLogLine(`{case="sentinel"}`, sentinelLine)
	}

	// syncIndexes drives an explicit index-gateway resync and waits for it to finish,
	// making freshly-shipped TSDB indexes discoverable.
	syncIndexes := func() {
		require.Eventually(t, func() bool {
			started, err := indexGatewayCli.TriggerSyncIndexes()
			return err == nil && started
		}, 10*time.Second, 50*time.Millisecond, "a manual index sync should be accepted")
		require.Eventually(t, func() bool {
			inProgress, err := indexGatewayCli.SyncIndexesInProgress()
			return err == nil && !inProgress
		}, 30*time.Second, 100*time.Millisecond, "the index sync should complete")
	}

	// Wait until the distributor can produce to an ACTIVE partition before pushing the
	// real data. The warmup line goes to a stream we never query, so retries are
	// harmless. Real pushes below use require.NoError (no retry) to avoid duplicates.
	require.Eventually(t, func() bool {
		return cliDistributor.PushLogLine("__warmup__", now, nil, streamLabels("case", "warmup")) == nil
	}, 60*time.Second, 250*time.Millisecond, "distributor should be able to produce to an active partition")

	// case 1: same stream and structured metadata, different timestamp -> NOT deduped
	require.NoError(t, cliDistributor.PushLogLine("c1", tsC1a, structuredMetadata("1"), streamLabels("case", "c1")))
	require.NoError(t, cliDistributor.PushLogLine("c1", tsC1b, structuredMetadata("1"), streamLabels("case", "c1")))

	// case 2: same timestamp and structured metadata, different stream -> NOT deduped
	require.NoError(t, cliDistributor.PushLogLine("c2", tsC2, structuredMetadata("1"), streamLabels("case", "c2", "inst", "a")))
	require.NoError(t, cliDistributor.PushLogLine("c2", tsC2, structuredMetadata("1"), streamLabels("case", "c2", "inst", "b")))

	// case 3: same timestamp and stream, different structured metadata -> NOT deduped
	require.NoError(t, cliDistributor.PushLogLine("c3", tsC3, structuredMetadata("1"), streamLabels("case", "c3")))
	require.NoError(t, cliDistributor.PushLogLine("c3", tsC3, structuredMetadata("2"), streamLabels("case", "c3")))

	// case 4: same as case 3, but queried with the categorize-labels encoding flag.
	require.NoError(t, cliDistributor.PushLogLine("c4", tsC4, structuredMetadata("1"), streamLabels("case", "c4")))
	require.NoError(t, cliDistributor.PushLogLine("c4", tsC4, structuredMetadata("2"), streamLabels("case", "c4")))

	// case 5: same timestamp + stream + structured metadata -> deduped.
	//
	// Loki collapses identical entries at write time both within a head block and
	// against a stream's last appended line. To make the duplicates survive to read
	// time, the first copy is flushed into its own chunk (and its index synced so it
	// stays queryable from the store) before the second copy is written - with a
	// separator line in between to advance the stream's last appended line. The two
	// copies then live in separate chunks, merged and deduplicated only on read.
	require.NoError(t, cliDistributor.PushLogLine("c5", tsC5, structuredMetadata("1"), streamLabels("case", "c5")))
	waitForLogLine(`{case="c5"}`, "c5") // ensure the first copy is consumed before flushing it
	require.NoError(t, ingesterCli.FlushTenant(`{case="c5"}`))
	syncIndexes()
	require.NoError(t, cliDistributor.PushLogLine("c5-sep", tsC5, structuredMetadata("1"), streamLabels("case", "c5")))
	require.NoError(t, cliDistributor.PushLogLine("c5", tsC5, structuredMetadata("1"), streamLabels("case", "c5")))

	// Sentinel pushed LAST. On a single partition consumed in offset order, the
	// sentinel becoming queryable proves every earlier record was already consumed and
	// appended, so the assertions below run against a complete data set.
	require.NoError(t, cliDistributor.PushLogLine(sentinelLine, tsSen, nil, streamLabels("case", "sentinel")))

	// runAssertions runs the five dedup assertions against cliFrontend.
	runAssertions := func(t *testing.T, storage string) {
		// case 1: same stream and structured metadata, different timestamp -> NOT deduped
		{
			resp, err := rangeQuery(`{case="c1"}`)
			require.NoError(t, err, storage)
			entries := allEntries(resp)
			require.Len(t, entries, 2, "%s case1: different timestamp must not dedup", storage)
			require.ElementsMatch(t, []string{"c1", "c1"}, linesFromResponse(resp), storage)
			require.NotEqual(t, entries[0][0], entries[1][0], "%s case1: entries must keep distinct timestamps", storage)
			require.Zero(t, resp.Data.Statistics.TotalDuplicates(), "%s case1: no entry should be deduplicated at query time", storage)

			// Negative control: prove the phase reads the expected source. Phase 1 must
			// reach the ingesters; the store-only phase must not.
			if storage == "store" {
				require.Zero(t, resp.Data.Statistics.Ingester.TotalReached, "%s: store phase must not query ingesters", storage)
			} else {
				require.Positive(t, resp.Data.Statistics.Ingester.TotalReached, "%s: ingester phase must query ingesters", storage)
			}
		}

		// case 2: same timestamp and structured metadata, different stream -> NOT deduped
		{
			resp, err := rangeQuery(`{case="c2"}`)
			require.NoError(t, err, storage)
			require.Len(t, resp.Data.Stream, 2, "%s case2: different stream must not dedup", storage)
			require.ElementsMatch(t, []string{"c2", "c2"}, linesFromResponse(resp), storage)
			require.ElementsMatch(t, []string{"a", "b"},
				[]string{resp.Data.Stream[0].Stream["inst"], resp.Data.Stream[1].Stream["inst"]},
				"%s case2: entries must belong to distinct streams", storage)
			require.Zero(t, resp.Data.Statistics.TotalDuplicates(), "%s case2: no entry should be deduplicated at query time", storage)
		}

		// case 3: same timestamp and stream, different structured metadata -> NOT deduped
		//
		// Without the categorize-labels flag, structured metadata surfaces as stream
		// labels, so the two entries appear as separate streams differing in "a".
		{
			resp, err := rangeQuery(`{case="c3"}`)
			require.NoError(t, err, storage)
			require.ElementsMatch(t, []string{"c3", "c3"}, linesFromResponse(resp),
				"%s case3: different structured metadata must NOT dedup", storage)
			require.Len(t, resp.Data.Stream, 2, "%s case3: structured metadata must keep entries distinct", storage)
			require.ElementsMatch(t, []string{"1", "2"},
				[]string{resp.Data.Stream[0].Stream["a"], resp.Data.Stream[1].Stream["a"]}, storage)
			require.Zero(t, resp.Data.Statistics.TotalDuplicates(), "%s case3: no entry should be deduplicated at query time", storage)
		}

		// case 4: same as case 3, but queried with the categorize-labels encoding flag,
		// which keeps structured metadata out of the stream labels and returns it as a
		// separate category in each entry, so both entries share a single stream.
		{
			resp, err := rangeQuery(`{case="c4"}`,
				client.Header{Name: httpreq.LokiEncodingFlagsHeader, Value: string(httpreq.FlagCategorizeLabels)})
			require.NoError(t, err, storage)

			require.Len(t, resp.Data.Stream, 1, "%s case4: categorize-labels must keep entries under one stream", storage)
			require.NotContains(t, resp.Data.Stream[0].Stream, "a",
				"%s case4: structured metadata must not appear in the stream labels", storage)

			entries := allEntries(resp)
			require.Len(t, entries, 2, "%s case4: different structured metadata must NOT dedup", storage)

			var metadataValues []string
			for _, entry := range entries {
				require.Equal(t, "c4", entry[1], storage)
				require.Len(t, entry, 3, "%s case4: categorized labels must be present in the entry", storage)
				var categorized struct {
					StructuredMetadata map[string]string `json:"structuredMetadata"`
				}
				require.NoError(t, json.Unmarshal([]byte(entry[2]), &categorized), storage)
				metadataValues = append(metadataValues, categorized.StructuredMetadata["a"])
			}
			require.ElementsMatch(t, []string{"1", "2"}, metadataValues,
				"%s case4: structured metadata must be returned as a separate category", storage)
			require.Zero(t, resp.Data.Statistics.TotalDuplicates(), "%s case4: no entry should be deduplicated at query time", storage)
		}

		// case 5: same timestamp + stream + structured metadata -> deduped.
		//
		// In this case we have the two identical copies, stored in separate chunks, are collapsed by
		// the query-time merge iterator into a single "c5"; the separator remains.
		{
			resp, err := rangeQuery(`{case="c5"}`)
			require.NoError(t, err, storage)
			lines := linesFromResponse(resp)
			require.Equal(t, 1, countLines(lines, "c5"), "%s case5: identical entry must be deduped to one copy", storage)
			require.Equal(t, 1, countLines(lines, "c5-sep"), storage)
			require.Len(t, lines, 2, storage)
			require.Positive(t, resp.Data.Statistics.TotalDuplicates(),
				"%s case5: query-time dedup must remove the duplicate (nonzero duplicates stat)", storage)
		}
	}

	// Phase 1: served from the ingester, except for logs created by case 5 (the first
	// log copy is already flushed to the store).
	waitForLogSentinel()
	runAssertions(t, "ingester")

	// Phase 2: flush everything to the chunk store, discover the index, and re-query
	// store-only so results provably come from the store.
	tsdbsBeforeFlush := countShippedTSDBs(t, tIngester.ClusterSharedPath())
	require.NoError(t, ingesterCli.FlushTenant(""))
	require.Greater(t, countShippedTSDBs(t, tIngester.ClusterSharedPath()), tsdbsBeforeFlush,
		"the whole-tenant flush should ship additional TSDB indexes to object storage")
	syncIndexes()

	tQuerier.AddFlags("-querier.query-store-only=true")
	require.NoError(t, tQuerier.Restart())

	// The flushed chunks contain every entry, so once the sentinel is visible from the
	// store the whole data set is present.
	waitForLogSentinel()
	runAssertions(t, "store")
}

// allEntries flattens all entries out of a streams query response.
func allEntries(resp *client.Response) []client.Entry {
	var out []client.Entry
	for _, stream := range resp.Data.Stream {
		out = append(out, stream.Values...)
	}
	return out
}

// countLines returns how many of lines equal want.
func countLines(lines []string, want string) int {
	var n int
	for _, l := range lines {
		if l == want {
			n++
		}
	}
	return n
}
