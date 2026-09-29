package compactor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// TestCoordinator_EndToEnd drives the coordinator against a real
// scheduler + worker pair wired in-process via wire.Local transport. Asserts:
//
//   - Merges create the expected number of index objects and report their paths.
//   - The coordinator atomically swaps the ToC: source paths removed, output paths
//     added with the right timestamps.
//   - Other tenants' rows survive byte-equivalent across the swap.
func TestCoordinator_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	// The acme postings ranges overlap; untouched verifies tenant-scoped ToC replacement.
	seed := map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(5 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(2 * time.Hour), end: window.Add(6 * time.Hour)},
			{path: "indexes/cc/src-2", start: window.Add(3 * time.Hour), end: window.Add(7 * time.Hour)},
		},
		"untouched": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(5 * time.Hour)},
			{path: "indexes/dd/idx-d-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
		},
	}
	writeToCWithIndexes(ctx, t, bucket, seed)

	seedSourceIndexObject(ctx, t, bucket, "indexes/aa/src-0", window.Add(time.Hour), "acme", "untouched")
	seedSourceIndexObject(ctx, t, bucket, "indexes/bb/src-1", window.Add(2*time.Hour), "acme")
	seedSourceIndexObject(ctx, t, bucket, "indexes/cc/src-2", window.Add(3*time.Hour), "acme")
	seedSourceIndexObject(ctx, t, bucket, "indexes/dd/idx-d-0", window.Add(time.Hour), "untouched")

	c := newIntegrationCoordinator(ctx, t, bucket, window.Add(time.Hour))
	c.cfg.MaxRunningCompactionTasks = 4

	// --- Cycle 1: 3 sources → ⌈P/K⌉ outputs ---
	preCycle1 := mustLoadTenant(ctx, t, bucket, window, "acme")
	require.Len(t, preCycle1, 3, "sanity: 3 source indexes seeded")
	_, runErr := c.compactTenantIndexes(ctx, "acme", window, preCycle1)
	require.NoError(t, runErr)

	postCycle1 := mustLoadTenants(ctx, t, bucket, window)
	require.Less(t, len(postCycle1["acme"]), 3,
		"cycle 1 must reduce acme's index count from 3 to fewer")
	require.Equal(t,
		[]string{"indexes/aa/src-0", "indexes/dd/idx-d-0"},
		pathsOf(postCycle1["untouched"]),
		"untouched tenant must be byte-identical across the swap")

	// The merged output objects must exist in the bucket after the swap.
	for _, entry := range postCycle1["acme"] {
		_, err := bucket.Attributes(ctx, entry.Path)
		require.NoError(t, err, "phase 1 output %q must exist after the swap", entry.Path)
	}
	// Pre-swap source paths must be gone from acme's section.
	acmePaths := pathsOf(postCycle1["acme"])
	for _, p := range []string{"indexes/aa/src-0", "indexes/bb/src-1", "indexes/cc/src-2"} {
		require.NotContains(t, acmePaths, p,
			"source path %q must be removed from acme's section after the swap", p)
	}

	// But the same path must remain present in any OTHER tenant's section that
	// also referenced it — ReplaceIndexPointers is scoped to one tenant.
	// indexes/aa/src-0 is shared between acme and untouched here.
	untouchedPaths := pathsOf(postCycle1["untouched"])
	for _, p := range []string{"indexes/aa/src-0", "indexes/dd/idx-d-0"} {
		require.Contains(t, untouchedPaths, p,
			"shared path %q must remain in untouched's section; the swap is tenant-scoped", p)
	}

	// --- Cycle 2: drive against the post-swap ToC. Should converge further. ---
	indexesC2 := mustLoadTenant(ctx, t, bucket, window, "acme")
	if len(indexesC2) > 1 {
		_, runErr := c.compactTenantIndexes(ctx, "acme", window, indexesC2)
		require.NoError(t, runErr)
		postCycle2 := mustLoadTenants(ctx, t, bucket, window)
		t.Logf("cycle 2: acme went from %d → %d indexes", len(indexesC2), len(postCycle2["acme"]))
		require.LessOrEqual(t, len(postCycle2["acme"]), len(postCycle1["acme"]),
			"cycle 2 must not increase index count")
	}

	// --- Cycle 3+: drive until convergence (≤1 index). Bounded by max-iters
	// so a regression doesn't infinite-loop. ---
	for i := range 5 {
		acmeIdx := mustLoadTenant(ctx, t, bucket, window, "acme")
		if len(acmeIdx) <= 1 {
			break
		}
		_, runErr := c.compactTenantIndexes(ctx, "acme", window, acmeIdx)
		require.NoError(t, runErr)
		t.Logf("convergence loop iter %d: acme → %d indexes", i,
			len(mustLoadTenant(ctx, t, bucket, window, "acme")))
	}
	final := mustLoadTenants(ctx, t, bucket, window)
	require.LessOrEqual(t, len(final["acme"]), 1,
		"after multiple cycles, acme must converge to ≤ 1 covering index")
	require.ElementsMatch(t,
		[]string{"indexes/aa/src-0", "indexes/dd/idx-d-0"},
		pathsOf(final["untouched"]),
		"untouched tenant must remain byte-identical across all cycles (including the path shared with acme)")
}

func TestCoordinator_LogCompactionSortSchemaCompatibility(t *testing.T) {
	targetSchema := []string{"label:app"}
	type indexGroup struct {
		schema        []string
		shardCount    int64
		sourceIndexes []int
	}
	tests := []struct {
		name            string
		sourceLayouts   []logs.SortLayout
		indexGroups     []indexGroup
		expectedIndexes int
	}{
		{
			name: "matching schemas compact",
			sourceLayouts: []logs.SortLayout{
				logsobj.TargetSortLayout([]string{"label:app"}),
				logsobj.TargetSortLayout([]string{"label:app"}),
			},
			indexGroups: []indexGroup{{
				schema:        []string{"label:app"},
				shardCount:    streams.ShardFactor,
				sourceIndexes: []int{0, 1},
			}},
			expectedIndexes: 1,
		},
		{
			name: "mismatched schemas sort each object",
			sourceLayouts: []logs.SortLayout{
				logsobj.TargetSortLayout([]string{"label:cluster"}),
				logsobj.TargetSortLayout([]string{"label:cluster"}),
			},
			indexGroups: []indexGroup{{
				schema:        []string{"label:cluster"},
				shardCount:    streams.ShardFactor,
				sourceIndexes: []int{0, 1},
			}},
			expectedIndexes: 2,
		},
		{
			name: "matching and mismatched indexes progress together",
			sourceLayouts: []logs.SortLayout{
				logsobj.TargetSortLayout([]string{"label:app"}),
				logsobj.TargetSortLayout([]string{"label:app"}),
				logsobj.TargetSortLayout([]string{"label:cluster"}),
				logsobj.TargetSortLayout([]string{"label:cluster"}),
			},
			indexGroups: []indexGroup{
				{schema: []string{"label:app"}, shardCount: streams.ShardFactor, sourceIndexes: []int{0, 1}},
				{schema: []string{"label:cluster"}, shardCount: streams.ShardFactor, sourceIndexes: []int{2, 3}},
			},
			expectedIndexes: 3,
		},
		{
			name: "single legacy object is sorted despite being converged",
			sourceLayouts: []logs.SortLayout{
				{SchemaLabels: []string{"label:app"}, StreamOrder: logs.StreamOrderUnspecified, ShardCount: streams.ShardFactor},
			},
			indexGroups: []indexGroup{{
				schema:        []string{"label:app"},
				sourceIndexes: []int{0},
			}},
			expectedIndexes: 1,
		},
		{
			name: "legacy shard count triggers sorting",
			sourceLayouts: []logs.SortLayout{
				{SchemaLabels: []string{"label:app"}, StreamOrder: logs.StreamOrderStableHashV1, ShardCount: streams.ShardFactor / 2},
				{SchemaLabels: []string{"label:app"}, StreamOrder: logs.StreamOrderStableHashV1, ShardCount: streams.ShardFactor / 2},
			},
			indexGroups: []indexGroup{{
				schema:        []string{"label:app"},
				shardCount:    streams.ShardFactor / 2,
				sourceIndexes: []int{0, 1},
			}},
			expectedIndexes: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			const tenant = "acme"
			window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
			base := window.Add(time.Hour)
			allSourcePaths := []string{
				"objects/aa/source-a",
				"objects/bb/source-b",
				"objects/cc/source-c",
				"objects/dd/source-d",
			}
			sourcePaths := allSourcePaths[:len(test.sourceLayouts)]

			input := scenarioInput{window: window, sortSchema: targetSchema}
			for i, sourcePath := range sourcePaths {
				layout := test.sourceLayouts[i]
				entries := fixtures.NewLogsFixtureBuilder(t,
					fixtures.WithSchemaLabels(layout.SchemaLabels...),
					fixtures.WithShardCount(layout.ShardCount),
				)
				entries.ForStream(`{app="api",cluster="prod"}`).
					Entry(int(base.Add(time.Second).Unix()), "{}", sourcePath+"/second").
					Entry(int(base.Unix()), "{}", sourcePath+"/first")
				input.logs = append(input.logs, scenarioLogObject{
					path: sourcePath, tenant: tenant, layout: layout, entries: entries,
				})
			}

			for i, group := range test.indexGroups {
				indexPath := fmt.Sprintf("indexes/%02d/log-sources", i)
				var groupSources []string
				for _, sourceIndex := range group.sourceIndexes {
					groupSources = append(groupSources, sourcePaths[sourceIndex])
				}
				input.indexes = append(input.indexes, scenarioLogIndex{
					path: indexPath, tenant: tenant, sources: groupSources,
					schema: group.schema, shardCount: group.shardCount,
					start: base, end: base.Add(time.Second),
				})
			}
			scenario := newCompactionScenario(ctx, t, input)

			before := scenario.snapshot(tenant).indexes[tenant]
			require.Len(t, before, len(test.indexGroups))

			step := scenario.runLogMerge(tenant)
			require.Equal(t, phaseOutcomeSwapped, step.outcome)

			final := scenario.snapshot(tenant)
			after := final.indexes[tenant]
			require.Len(t, after, test.expectedIndexes)
			contents := final.contents
			require.ElementsMatch(t, expectedSourceLogLines(sourcePaths), contents.lines,
				"every source log line must remain reachable through the ToC and index")
			require.Equal(t, int64(len(contents.lines)), contents.statsRowCount,
				"index stats must account for every reachable log row")
			require.Equal(t, contents.statsObjectPaths, contents.postingsObjectPaths,
				"stats and postings must reference the same log objects")
			require.Equal(t, map[string]bool{"label:app": true}, contents.sortSchemas)
			for _, layout := range contents.layouts {
				require.True(t, logsobj.EqualSortLayout(logsobj.TargetSortLayout(targetSchema), layout))
			}
			for _, entry := range after {
				require.True(t, entry.Start.Equal(base))
				require.True(t, entry.End.Equal(base.Add(time.Second)))
				require.Positive(t, entry.FileSize)
				exists, err := scenario.bucket.Exists(ctx, entry.Path)
				require.NoError(t, err)
				require.True(t, exists, "replacement index must exist")
			}
		})
	}
}

func newIntegrationCoordinator(ctx context.Context, t *testing.T, bucket objstore.Bucket, now time.Time) *coordinator {
	return newIntegrationCoordinatorWithLogsobjConfig(ctx, t, bucket, now, nil)
}

func newIntegrationCoordinatorWithLogsobjConfig(ctx context.Context, t *testing.T, bucket objstore.Bucket, now time.Time, logsobjConfig *logsobj.BuilderBaseConfig) *coordinator {
	t.Helper()
	sched, _ := startInProcessSchedulerAndWorker(ctx, t, bucket, logsobjConfig)
	return &coordinator{
		cfg: Config{
			MaxRunsPerTask:               2,
			LogMaxRunsPerTask:            2,
			ToCConsolidateTimeout:        10 * time.Second,
			LogMaxRunningCompactionTasks: 1,
		},
		logger: log.NewNopLogger(),
		bucket: bucket,
		runPlan: func(runCtx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			return runPlan(runCtx, log.NewNopLogger(), sched, opts, plan)
		},
		metastoreWriter: metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger()),
		clock:           func() time.Time { return now },
		metrics:         newCoordinatorMetrics(prometheus.NewRegistry()),
	}
}

// startInProcessSchedulerAndWorker brings up a wire.Local scheduler + worker
// pair sharing the supplied bucket. Both register cleanup hooks on t.
func startInProcessSchedulerAndWorker(ctx context.Context, t *testing.T, bucket objstore.Bucket, logsobjConfig *logsobj.BuilderBaseConfig) (*scheduler.Scheduler, *worker.Worker) {
	t.Helper()

	schedulerListener := &wire.Local{Address: wire.LocalScheduler}
	workerListener := &wire.Local{Address: wire.LocalWorker}
	dialer := wire.NewLocalDialer(schedulerListener, workerListener)

	sched, err := scheduler.New(scheduler.Config{
		Logger:   log.NewNopLogger(),
		Listener: schedulerListener,
	})
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(ctx, sched.Service()))
	t.Cleanup(func() {
		stopCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = services.StopAndAwaitTerminated(stopCtx, sched.Service())
	})

	ms := metastore.NewObjectMetastore(bucket, metastore.Config{}, log.NewNopLogger(),
		metastore.NewObjectMetastoreMetrics(prometheus.NewRegistry()))

	var compactionCfg Config
	compactionCfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
	if logsobjConfig != nil {
		compactionCfg.LogsobjBuilder = *logsobjConfig
	}

	w, err := worker.New(worker.Config{
		Logger:           log.NewNopLogger(),
		Bucket:           bucket,
		DataBucket:       bucket,
		Metastore:        ms,
		BatchSize:        2048,
		Dialer:           dialer,
		Listener:         workerListener,
		SchedulerAddress: wire.LocalScheduler,
		NumThreads:       2,
		ScratchStore:     scratch.NewMemory(),
		IndexobjCfg:      compactionCfg.IndexobjBuilder,
		LogsobjCfg:       compactionCfg.LogsobjBuilder,
	})
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(ctx, w.Service()))
	t.Cleanup(func() {
		stopCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = services.StopAndAwaitTerminated(stopCtx, w.Service())
	})

	return sched, w
}

type integrationSortSchema []string

func (s integrationSortSchema) SortSchemaLabels(string) []string { return s }
func (integrationSortSchema) CompactionPhases(string) (bool, bool) {
	return true, true
}

type logCompactionContents struct {
	statsObjectPaths    map[string]bool
	postingsObjectPaths map[string]bool
	sortSchemas         map[string]bool
	layouts             []logs.SortLayout
	statsRowCount       int64
	lines               []string
}

func readLogCompactionContents(
	ctx context.Context,
	t *testing.T,
	bucket objstore.Bucket,
	tocEntries []indexEntry,
	tenant string,
) logCompactionContents {
	t.Helper()

	contents := logCompactionContents{
		statsObjectPaths:    make(map[string]bool),
		postingsObjectPaths: make(map[string]bool),
		sortSchemas:         make(map[string]bool),
	}
	for _, tocEntry := range tocEntries {
		indexObj, err := dataobj.FromBucket(ctx, bucket, tocEntry.Path, 0)
		require.NoError(t, err)

		for _, section := range indexObj.Sections().Filter(stats.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			statsSection, err := stats.Open(ctx, section)
			require.NoError(t, err)
			reader := stats.NewRowReader(ctx, statsSection)
			for reader.Next() {
				row := reader.At()
				contents.statsObjectPaths[row.ObjectPath] = true
				contents.sortSchemas[row.SortSchema] = true
				contents.statsRowCount += row.RowCount
			}
			require.NoError(t, reader.Err())
			require.NoError(t, reader.Close())
		}

		for _, section := range indexObj.Sections().Filter(postings.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			postingsSection, err := postings.Open(ctx, section)
			require.NoError(t, err)
			inner := postings.NewReader(postings.ReaderOptions{Columns: postingsSection.Columns()})
			require.NoError(t, inner.Open(ctx))
			reader := postings.NewRowReader(ctx, inner)
			for reader.Next() {
				row := reader.At()
				contents.postingsObjectPaths[row.ObjectPath] = true
			}
			require.NoError(t, reader.Err())
			require.NoError(t, reader.Close())
		}
	}

	for objectPath := range contents.statsObjectPaths {
		logObj, err := dataobj.FromBucket(ctx, bucket, objectPath, 0)
		require.NoError(t, err)
		for _, section := range logObj.Sections().Filter(logs.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			logsSection, err := logs.Open(ctx, section)
			require.NoError(t, err)
			contents.layouts = append(contents.layouts, logsSection.SortLayout())
		}
		for _, record := range fixtures.ReadTenantLogs(t, ctx, logObj, tenant) {
			contents.lines = append(contents.lines, string(record.Line))
		}
	}

	return contents
}

func expectedSourceLogLines(sourcePaths []string) []string {
	lines := make([]string, 0, len(sourcePaths)*2)
	for _, sourcePath := range sourcePaths {
		lines = append(lines, sourcePath+"/first", sourcePath+"/second")
	}
	return lines
}

func mustLoadTenants(ctx context.Context, t *testing.T, b objstore.Bucket, window time.Time) tenantIndexes {
	t.Helper()
	got, err := loadTenantIndexes(ctx, b, window)
	require.NoError(t, err)
	return got
}

func mustLoadTenant(ctx context.Context, t *testing.T, b objstore.Bucket, window time.Time, tenant string) []indexEntry {
	t.Helper()
	return mustLoadTenants(ctx, t, b, window)[tenant]
}

func pathsOf(entries []indexEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

// seedSourceIndexObject builds and uploads tenant-tagged postings and stats sections.
func seedSourceIndexObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string, ts time.Time, tenants ...string) {
	t.Helper()

	sections := make([]dataobj.SectionBuilder, 0, 2*len(tenants))
	for _, tenant := range tenants {
		postingsBuilder := postings.NewBuilder(nil, 0, 0, math.MaxInt)
		postingsBuilder.SetTenant(tenant)
		for streamID, value := range []string{"a", "z"} {
			postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath:       path,
				SectionIndex:     0,
				ColumnName:       "service",
				LabelValue:       value,
				StreamID:         int64(streamID),
				Timestamp:        ts,
				UncompressedSize: 100,
			})
		}
		sections = append(sections, postingsBuilder)

		statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
		statsBuilder.SetTenant(tenant)
		statsBuilder.Append(stats.Stat{
			ObjectPath:       path,
			SectionIndex:     0,
			SortSchema:       "label:service",
			Labels:           map[string]string{"service": "api"},
			MinTimestamp:     ts.UnixNano(),
			MaxTimestamp:     ts.UnixNano() + 1000,
			RowCount:         10,
			UncompressedSize: 1000,
		})
		sections = append(sections, statsBuilder)
	}

	storeIntegrationObject(ctx, t, bucket, path, sections...)
}

func storeIntegrationObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string, sections ...dataobj.SectionBuilder) {
	t.Helper()
	obj, closer := fixtures.DataObject(t, sections...)
	defer closer.Close()

	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, bucket.Upload(ctx, path, reader))
}
