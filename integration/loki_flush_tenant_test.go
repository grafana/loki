//go:build integration

package integration

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

// TestFlushTenant exercises the POST /flush/tenant ingester endpoint end to end:
// it flushes a single tenant's in-memory chunks (optionally scoped by a LogQL
// stream selector) and force-ships the in-memory TSDB index to object storage.
//
// To prove the flushed data is genuinely retrievable from object storage - and
// not merely still resident in the ingester's memory, which the endpoint
// deliberately keeps - the querier runs with -querier.query-store-only=true, so
// reads never hit the ingesters. Anything a query returns therefore came from
// the store.
//
//   - tenant A pushes {app="a"} and {app="b"} and flushes only {app="a"} (scoped);
//   - tenant B pushes {app="c"} and is never flushed (cross-tenant isolation).
//
// Step 5 then does a whole-tenant (empty-selector) flush of tenant A - a SECOND
// forced flush in the same index period that also ships {app="b"}. Its index is
// durable immediately but, because the index-gateway re-lists object storage
// through a ~1m per-table cache, only becomes queryable after an explicit
// PUT /sync-indexes, which refreshes that cache and downloads the new index.
func TestFlushTenant(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// Run the index-gateway and distributor first. The index-gateway downloads
	// shipped TSDBs from object storage and serves them to the querier; a
	// sub-second resync makes a freshly-shipped index visible almost immediately
	// (this is the only periodic gate - the flush endpoint uploads synchronously).
	var (
		// The harness config enables retention, so the querier's
		// cache-generation-loader requires a compactor address. A long
		// compaction interval keeps the compactor from rewriting the
		// freshly-shipped TSDB index while the test inspects it.
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1h",
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

	// Then the ingester (holds chunks in memory; target of the flush) and the
	// query scheduler.
	var (
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
	)
	require.NoError(t, clu.Run())

	// The querier reads only from the store, never the ingesters.
	clu.AddComponent(
		"querier",
		"-target=querier",
		"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-querier.query-store-only=true",
		"-tsdb.shipper.resync-interval=250ms",
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		"-common.compactor-address="+tCompactor.HTTPURL(),
	)
	require.NoError(t, clu.Run())

	// Finally, the query-frontend - the query entry point.
	tQueryFrontend := clu.AddComponent(
		"query-frontend",
		"-target=query-frontend",
		"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-querier.query-store-only=true",
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		"-common.compactor-address="+tCompactor.HTTPURL(),
	)
	require.NoError(t, clu.Run())

	const (
		lineA = "lineA" // tenant A, {app="a"} - flushed via selector
		lineB = "lineB" // tenant A, {app="b"} - excluded by the scoped flush
		lineC = "lineC" // tenant B, {app="c"} - never flushed
	)

	tenantA := randStringRunes()
	tenantB := randStringRunes()
	now := time.Now()

	// push sends a single log line for the given tenant through the distributor.
	push := func(tenant, line string, lbls map[string]string) {
		cli := client.New(tenant, "", tDistributor.HTTPURL())
		cli.Now = now
		require.NoError(t, cli.PushLogLine(line, now, nil, lbls))
	}
	push(tenantA, lineA, map[string]string{"app": "a"})
	push(tenantA, lineB, map[string]string{"app": "b"})
	push(tenantB, lineC, map[string]string{"app": "c"})

	// flushClient issues /flush/tenant against the ingester for a tenant.
	flushClient := func(tenant string) *client.Client {
		cli := client.New(tenant, "", tIngester.HTTPURL())
		cli.Now = now
		return cli
	}
	// frontendClient queries through the (store-only) read path for a tenant.
	frontendClient := func(tenant string) *client.Client {
		cli := client.New(tenant, "", tQueryFrontend.HTTPURL())
		cli.Now = now
		return cli
	}

	// queryLines runs a range query and returns the matched log lines. It uses
	// require, so it must only be called from the test goroutine.
	queryLines := func(cli *client.Client, query string) []string {
		resp, err := cli.RunRangeQuery(context.Background(), query)
		require.NoError(t, err)
		return linesFromResponse(resp)
	}
	// eventuallyHasLine returns a condition for require.Eventually that polls a
	// store-only query until the wanted line appears. It must not use require
	// (testify runs the condition on its own goroutine).
	eventuallyHasLine := func(cli *client.Client, query, want string) func() bool {
		return func() bool {
			resp, err := cli.RunRangeQuery(context.Background(), query)
			if err != nil {
				return false
			}
			return slices.Contains(linesFromResponse(resp), want)
		}
	}

	cliFrontendA := frontendClient(tenantA)
	cliFrontendB := frontendClient(tenantB)

	// 1. Before any flush the store-only querier sees nothing: the data lives only
	//    in the ingester's memory, which the querier never queries. No index has
	//    been shipped to object storage yet either.
	require.Empty(t, queryLines(cliFrontendA, `{app="a"}`), "store-only query must not see un-flushed data")
	require.Zero(t, countShippedTSDBs(t, tIngester.ClusterSharedPath()), "no index should be shipped before any flush")

	// 2. Scoped flush: flush only tenant A's {app="a"} stream and force its index
	//    to ship. The ship writes at least one TSDB index file to object storage.
	require.NoError(t, flushClient(tenantA).FlushTenant(`{app="a"}`))
	require.Positive(t, countShippedTSDBs(t, tIngester.ClusterSharedPath()), "flush should have shipped a TSDB index file")

	// 3. Headline proof: {app="a"} is now queryable through a store-only querier,
	//    so the result can only have come from object storage. require.Eventually
	//    absorbs the (sub-second) index-gateway resync lag.
	require.Eventually(t, eventuallyHasLine(cliFrontendA, `{app="a"}`, lineA),
		10*time.Second, 250*time.Millisecond, "flushed stream should become queryable from the store")

	// 4. Matcher scoping held: {app="b"} was not selected by the flush, so it was
	//    not shipped and stays invisible to the store-only querier.
	require.Empty(t, queryLines(cliFrontendA, `{app="b"}`), "stream excluded by the selector must not be flushed")

	// 5. Whole-tenant flush (empty selector) for tenant A - a SECOND forced flush in
	//    this index period that also ships the previously-excluded {app="b"}. The new
	//    file lands in the already-tracked today table, which the index gateway serves
	//    from a ~1m cached object listing (cacheTimeout in
	//    pkg/storage/stores/shipper/indexshipper/storage/cached_client.go), so it stays
	//    invisible until /sync-indexes refreshes that cache and downloads the index.
	require.NoError(t, flushClient(tenantA).FlushTenant(""))

	// The new file is shipped but not yet synced, so the store-only querier still
	// cannot see {app="b"}: the index gateway is serving its stale cached listing.
	require.Empty(t, queryLines(cliFrontendA, `{app="b"}`), "second flush must not be queryable before the sync")

	syncClient := client.New("", "", tIndexGateway.HTTPURL())

	// Trigger a manual (cache-refreshing) sync. A periodic resync may momentarily
	// hold the sync lock and 409 our request - and, unlike the manual sync, it does
	// not refresh the list cache - so retry until ours is accepted (almost always
	// the first attempt). A true return guarantees the sync was marked in progress
	// before TriggerSyncIndexes returned (see TriggerManual's started-channel).
	require.Eventually(t, func() bool {
		started, err := syncClient.TriggerSyncIndexes()
		return err == nil && started
	}, 5*time.Second, 50*time.Millisecond, "a manual index sync should be accepted")

	// Wait for the sync to complete. We do not assert on observing in_progress==true
	// here: against the local-filesystem store the sync can finish before our first
	// HTTP status poll, so requiring the transient in-progress state to be observed
	// would be inherently flaky. The accepted trigger above already guarantees the
	// sync ran; this only waits for it to finish before we assert queryability.
	require.Eventually(t, func() bool {
		inProgress, err := syncClient.SyncIndexesInProgress()
		return err == nil && !inProgress
	}, 15*time.Second, 100*time.Millisecond, "the index sync should complete")

	// With the new index downloaded, {app="b"} is now queryable from the store.
	require.Eventually(t, eventuallyHasLine(cliFrontendA, `{app="b"}`, lineB),
		10*time.Second, 250*time.Millisecond,
		"second forced flush should be queryable after the index sync")

	// 6. Cross-tenant isolation: tenant B was never flushed, so its data is absent
	//    from the store.
	require.Empty(t, queryLines(cliFrontendB, `{app="c"}`), "un-flushed tenant must not be queryable from the store")
}

// linesFromResponse flattens the log lines out of a streams query response.
func linesFromResponse(resp *client.Response) []string {
	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			lines = append(lines, val[1])
		}
	}
	return lines
}

// countShippedTSDBs counts TSDB index files under the shared object-storage
// directory. The ingester's local active index and the read-side download cache
// live under per-component data dirs (not the shared path), so anything found
// here was uploaded to object storage.
func countShippedTSDBs(t *testing.T, sharedPath string) int {
	t.Helper()
	var count int
	require.NoError(t, filepath.WalkDir(sharedPath, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".tsdb") {
			count++
		}
		return nil
	}))
	return count
}
