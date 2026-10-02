package compactor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/compactor/internal/compactortest"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// compactionScenario owns the durable inputs and scheduler/worker used by one
// test. Steps read the current ToC, so consecutive runs use persisted state.
type compactionScenario struct {
	ctx         context.Context
	t           *testing.T
	stored      *compactortest.Scenario
	coordinator *coordinator
}

type scenarioInput struct {
	window        time.Time
	logs          []compactortest.Source
	sortSchema    []string
	logsobjConfig *logsobj.BuilderBaseConfig
}

// newCompactionScenario wraps compactortest.New without needing to export the coordinator.
func newCompactionScenario(ctx context.Context, t *testing.T, input scenarioInput) *compactionScenario {
	t.Helper()
	seeded := compactortest.New(ctx, t, input.window, input.logs)

	s := &compactionScenario{
		ctx:         ctx,
		t:           t,
		stored:      seeded,
		coordinator: newIntegrationCoordinator(ctx, t, seeded.Bucket, input.window.Add(time.Hour), input.logsobjConfig),
	}
	s.coordinator.limits = integrationSortSchema(input.sortSchema)
	return s
}

func (s *compactionScenario) compactUntilIdle(tenant string) {
	s.t.Helper()
	for cycle := range 10 {
		indexOutcome := s.coordinator.runIndexMergePhase(s.ctx, tenant, s.stored.Window)
		require.NotEqual(s.t, phaseOutcomeError, indexOutcome, "index phase failed in cycle %d", cycle)
		logOutcome := s.coordinator.runLogMergePhase(s.ctx, tenant, s.stored.Window)
		require.NotEqual(s.t, phaseOutcomeError, logOutcome, "log phase failed in cycle %d", cycle)
		if indexOutcome == phaseOutcomeNoWork && logOutcome == phaseOutcomeNoWork {
			return
		}
	}
	s.t.Fatal("compaction did not reach a no-work cycle")
}

// TestCoordinator_IndexCompactionCycles drives the coordinator against a real
// scheduler + worker pair wired in-process via wire.Local transport. Asserts:
//
//   - Merges create the expected number of index objects and report their paths.
//   - The coordinator atomically swaps the ToC: source paths removed, output paths
//     added with the right timestamps.
//   - Other tenants' rows survive byte-equivalent across the swap.
func TestCoordinator_IndexCompactionCycles(t *testing.T) {
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

	c := newIntegrationCoordinator(ctx, t, bucket, window.Add(time.Hour), nil)
	c.indexDispatcher.limit = 4

	// --- Cycle 1: 3 sources → ⌈P/K⌉ outputs ---
	initial := mustLoadTenantIndexes(ctx, t, bucket, window)
	require.Equal(t, []string{"indexes/aa/src-0", "indexes/dd/idx-d-0"}, pathsOf(initial["untouched"]))
	require.Len(t, initial["acme"], 3, "sanity: 3 source indexes seeded")
	_, runErr := c.compactTenantIndexes(ctx, "acme", window, initial["acme"])
	require.NoError(t, runErr)

	postCycle1 := mustLoadTenantIndexes(ctx, t, bucket, window)
	require.Less(t, len(postCycle1["acme"]), 3,
		"cycle 1 must reduce acme's index count from 3 to fewer")
	require.Equal(t, initial["untouched"], postCycle1["untouched"],
		"other tenant's index entries, including the shared source path, must survive the swap")

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

	// Drive subsequent cycles from persisted state, bounded to prevent hangs.
	for cycle := range 6 {
		before := mustLoadTenantIndexes(ctx, t, bucket, window)["acme"]
		if len(before) <= 1 {
			break
		}
		_, runErr := c.compactTenantIndexes(ctx, "acme", window, before)
		require.NoError(t, runErr)
		after := mustLoadTenantIndexes(ctx, t, bucket, window)["acme"]
		require.LessOrEqual(t, len(after), len(before), "cycle %d must not increase index count", cycle+2)
		t.Logf("cycle %d: acme went from %d → %d indexes", cycle+2, len(before), len(after))
	}
	final := mustLoadTenantIndexes(ctx, t, bucket, window)
	require.Equal(t, len(final["acme"]), 1,
		"after multiple cycles, acme tenant must converge to 1 covering index")
	require.Equal(t, initial["untouched"], final["untouched"],
		"other tenant's index entries must survive every cycle")
}

func TestCoordinator_LogCompactionSortSchemaCompatibility(t *testing.T) {
	targetSchema := []string{"label:app"}
	clusterSchema := []string{"label:cluster"}
	targetLayout := logsobj.TargetSortLayout(targetSchema)
	clusterLayout := logsobj.TargetSortLayout(clusterSchema)
	legacyStreamOrder := logs.SortLayout{SchemaLabels: targetSchema, StreamOrder: logs.StreamOrderUnspecified, ShardCount: streams.ShardFactor}
	legacyShardCount := logs.SortLayout{SchemaLabels: targetSchema, StreamOrder: logs.StreamOrderStableHashV1, ShardCount: streams.ShardFactor / 2}

	type indexGroup struct {
		schema     []string
		shardCount int64
		layouts    []logs.SortLayout
	}
	tests := []struct {
		name            string
		indexGroups     []indexGroup
		expectedIndexes int
	}{
		{
			name: "matching schemas compact",
			indexGroups: []indexGroup{{
				schema: targetSchema, shardCount: streams.ShardFactor, layouts: []logs.SortLayout{targetLayout, targetLayout},
			}},
			expectedIndexes: 1,
		},
		{
			name: "mismatched schemas sort each object",
			indexGroups: []indexGroup{{
				schema: clusterSchema, shardCount: streams.ShardFactor, layouts: []logs.SortLayout{clusterLayout, clusterLayout},
			}},
			expectedIndexes: 2,
		},
		{
			name: "matching and mismatched indexes progress together",
			indexGroups: []indexGroup{
				{schema: targetSchema, shardCount: streams.ShardFactor, layouts: []logs.SortLayout{targetLayout, targetLayout}},
				{schema: clusterSchema, shardCount: streams.ShardFactor, layouts: []logs.SortLayout{clusterLayout, clusterLayout}},
			},
			expectedIndexes: 3,
		},
		{
			name: "single legacy object is sorted despite being converged",
			indexGroups: []indexGroup{{
				schema: targetSchema, layouts: []logs.SortLayout{legacyStreamOrder},
			}},
			expectedIndexes: 1,
		},
		{
			name: "legacy shard count triggers sorting",
			indexGroups: []indexGroup{{
				schema: targetSchema, shardCount: streams.ShardFactor / 2, layouts: []logs.SortLayout{legacyShardCount, legacyShardCount},
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

			bucket := objstore.NewInMemBucket()
			var sourceLines []string
			var tocIndexes []testIndex
			for i, group := range test.indexGroups {
				groupSources := make([]string, len(group.layouts))
				for j, layout := range group.layouts {
					sourcePath := fmt.Sprintf("objects/group-%d-source-%d", i, j)
					groupSources[j] = sourcePath
					first, second := sourcePath+"/first", sourcePath+"/second"
					sourceLines = append(sourceLines, first, second)

					entries := fixtures.NewLogsFixtureBuilder(t, fixtures.WithSchemaLabels(layout.SchemaLabels...))
					entries.ForStream(`{app="api",cluster="prod"}`).
						Entry(int(base.Add(time.Second).Unix()), "{}", second).
						Entry(int(base.Unix()), "{}", first)

					source := newScenarioLogSource(t, sourcePath, tenant, entries, layout)
					reader, err := source.Object.Reader(ctx)
					require.NoError(t, err)
					require.NoError(t, errors.Join(bucket.Upload(ctx, source.Path, reader), reader.Close()))
				}
				indexPath := fmt.Sprintf("indexes/%02d/log-sources", i)
				tocIndexes = append(tocIndexes, seedLogCompactionIndex(ctx, t, bucket, indexPath, tenant, groupSources, group.schema, group.shardCount, base))
			}
			writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{tenant: tocIndexes})
			c := newIntegrationCoordinator(ctx, t, bucket, base, nil)
			c.limits = integrationSortSchema(targetSchema)

			before := mustLoadTenantIndexes(ctx, t, bucket, window)[tenant]
			require.Len(t, before, len(test.indexGroups))

			require.Equal(t, phaseOutcomeSwapped, c.runLogMergePhase(ctx, tenant, window))

			after := mustLoadTenantIndexes(ctx, t, bucket, window)[tenant]
			require.Len(t, after, test.expectedIndexes)
			stored := &compactortest.Scenario{Bucket: bucket, Window: window}
			contents := stored.ReadReachableContents(ctx, t, tenant)
			requireLogContents(t, contents, targetSchema, sourceLines)
			for _, entry := range after {
				require.True(t, entry.Start.Equal(base))
				require.True(t, entry.End.Equal(base.Add(time.Second)))
				exists, err := bucket.Exists(ctx, entry.Path)
				require.NoError(t, err)
				require.True(t, exists, "replacement index %q must exist", entry.Path)
			}
		})
	}
}

func seedLogCompactionIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, path, tenant string, sourcePaths, sortSchema []string, shardCount int64, ts time.Time) testIndex {
	t.Helper()
	streamLabels := labels.FromStrings("app", "api", "cluster", "prod")
	postingsBuilder := postings.NewBuilder(nil, 0, 0, math.MaxInt)
	postingsBuilder.SetTenant(tenant)
	statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
	statsBuilder.SetTenant(tenant)
	schemaLabels := make(map[string]string, len(sortSchema))
	for _, key := range sortSchema {
		_, name, _ := strings.Cut(key, ":")
		schemaLabels[name] = streamLabels.Get(name)
	}
	for _, sourcePath := range sourcePaths {
		statsBuilder.Append(stats.Stat{
			ObjectPath: sourcePath, SectionIndex: 0, SortSchema: strings.Join(sortSchema, ","),
			Labels: schemaLabels, MinTimestamp: ts.UnixNano(), MaxTimestamp: ts.Add(time.Second).UnixNano(),
			RowCount: 2, UncompressedSize: 100, ShardBucket: streams.ShardBucket(streamLabels),
		})
		postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: sourcePath, ShardBuckets: shardCount, SectionIndex: 0,
			ColumnName: "app", LabelValue: "api", StreamID: 0,
			Timestamp: ts, UncompressedSize: 100,
		})
	}
	storeIntegrationObject(ctx, t, bucket, path, postingsBuilder, statsBuilder)
	return testIndex{path: path, start: ts, end: ts.Add(time.Second)}
}

func TestE2ECompactionConvergence(t *testing.T) {
	fiveSourceLayouts := func() []logs.SortLayout {
		layout := logsobj.TargetSortLayout([]string{"label:app"})
		return []logs.SortLayout{layout, layout, layout, layout, layout}
	}

	t.Run("equal sort layouts", func(t *testing.T) {
		runConvergenceTest(t, fiveSourceLayouts())
	})

	t.Run("mismatched sort schemas", func(t *testing.T) {
		// mismatched schemas will converge after sorting
		layouts := fiveSourceLayouts()
		layouts[2].SchemaLabels = []string{"label:cluster"}
		runConvergenceTest(t, layouts)
	})
}

// runConvergenceTest creates one overlapping object for every provided logs.SortLayout and expects them to
// converge into a single sorted run after any necessary re-sorting.
func runConvergenceTest(t *testing.T, layouts []logs.SortLayout) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const tenant = "acme"
	schema := []string{"label:app"}

	const streamCount = 26
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	base := window.Add(time.Hour)
	// Small outputs and moderately sized lines exercise rollover across objects.
	outputConfig := logsobj.BuilderBaseConfig{
		TargetPageSize: 2048, TargetSectionSize: 8 * 1024,
		TargetObjectSize: 16 * 1024, BufferSize: 2048,
		SectionStripeMergeLimit: 2, EstimatedCompressionRatio: 1,
	}
	input := scenarioInput{window: window, sortSchema: schema, logsobjConfig: &outputConfig}
	payload := strings.Repeat("A", 1024)
	for file, layout := range layouts {
		entries := fixtures.NewLogsFixtureBuilder(t,
			fixtures.WithSchemaLabels(layout.SchemaLabels...),
		)
		for app := 'a'; app <= 'z'; app++ {
			stream := entries.ForStream(fmt.Sprintf(`{app=%q}`, string(app)))
			for record := range 3 {
				stream.Entry(int(base.Unix())+record, "{}", fmt.Sprintf("file-%d/app-%c/record-%d:%s", file, app, record, payload))
			}
		}

		input.logs = append(input.logs, newScenarioLogSource(t, fmt.Sprintf("objects/source-%d", file), tenant, entries, layout))
	}

	scenario := newCompactionScenario(ctx, t, input)
	indexes := scenario.stored.Indexes(ctx, t, tenant)
	require.Len(t, indexes, 5)
	var sourceLines []string
	for fileIdx, source := range input.logs {
		indexPath := compactortest.IndexPath(fileIdx)
		require.True(t, slices.ContainsFunc(indexes, func(index indexpointers.IndexPointer) bool {
			return index.Path == indexPath
		}), "index for source %q must be in the ToC", source.Path)

		// Check the file pointed to by this index is the correct shape according to the index
		refs, indexedSchema, shardCount, err := logSectionRefsFor(ctx, scenario.stored.Bucket, tenant, indexPath)
		require.NoError(t, err)
		require.NotEmpty(t, refs)
		require.Equal(t, source.Path, refs[0].Ref.ObjectPath)
		require.Equal(t, layouts[fileIdx].SchemaLabels, indexedSchema)
		require.Equal(t, int64(layouts[fileIdx].ShardCount), shardCount)

		// Check the actual logs section is the correct shape too
		obj, err := dataobj.FromBucket(ctx, scenario.stored.Bucket, source.Path, 0)
		require.NoError(t, err)
		tenantSections, err := sections.ForTenant(obj.Sections(), tenant)
		require.NoError(t, err)
		require.NotEmpty(t, tenantSections.Logs, "source %q must contain tenant logs", source.Path)
		for _, section := range tenantSections.Logs {
			opened, err := logs.Open(ctx, section)
			require.NoError(t, err)
			require.Equal(t, layouts[fileIdx], opened.SortLayout(), "source %q must preserve its complete sort layout", source.Path)
		}
		for _, record := range fixtures.ReadTenantLogs(t, obj, tenant) {
			sourceLines = append(sourceLines, string(record.Line))
		}
	}
	require.Len(t, sourceLines, 5*streamCount*3, "seeded source objects must contain all records")

	scenario.compactUntilIdle(tenant)

	finalContents := scenario.stored.ReadReachableContents(ctx, t, tenant)
	requireLogContents(t, finalContents, schema, sourceLines)
	requireSingleSortedRun(t, scenario, scenario.stored.Indexes(ctx, t, tenant), tenant, schema, len(sourceLines))
}

func newScenarioLogSource(t *testing.T, path, tenant string, entries *fixtures.LogFixtureBuilder, layout logs.SortLayout) compactortest.Source {
	t.Helper()
	options := logs.BuilderOptions{
		PageSizeHint: 2048, PageMaxRowCount: 10000, BufferSize: 2048 * 8,
		StripeMergeLimit: 2, AppendStrategy: logs.AppendOrdered,
		SortOrder: logs.SortSchemaASC, SchemaLabels: layout.SchemaLabels,
		StreamOrder: layout.StreamOrder, ShardCount: layout.ShardCount,
	}
	obj, closer := fixtures.DataObject(t,
		fixtures.StreamsSection(t, tenant, entries.Streams()),
		fixtures.LogsSection(t, tenant, entries.Logs(), fixtures.WithBuilderOptions(&options)),
	)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	return compactortest.Source{Path: path, Object: obj}
}

func requireLogContents(t *testing.T, contents compactortest.Contents, schema []string, sourceLines []string) {
	t.Helper()
	require.ElementsMatch(t, sourceLines, contents.Lines, "all source records must be reachable exactly once")
	require.Equal(t, int64(len(sourceLines)), contents.StatsRowCount, "index stats must account for every reachable log row")
	require.Equal(t, contents.StatsObjectPaths, contents.PostingsObjectPaths, "stats and postings must reference the same log objects")
	require.Equal(t, map[string]bool{strings.Join(schema, ","): true}, contents.SortSchemas)
	require.NotEmpty(t, contents.LogSectionLayouts)
	expectedLayouts := slices.Repeat([]logs.SortLayout{logsobj.TargetSortLayout(schema)}, len(contents.LogSectionLayouts))
	require.True(t, slices.EqualFunc(expectedLayouts, contents.LogSectionLayouts, logsobj.EqualSortLayout), "every section must use the target layout")
}

func requireSingleSortedRun(t *testing.T, scenario *compactionScenario, indexes []indexpointers.IndexPointer, tenant string, schema []string, wantRows int) {
	t.Helper()
	require.Len(t, indexes, 1, "all overlapping indexes should consolidate")

	sections, indexedSchema, shardCount, err := logSectionRefsFor(t.Context(), scenario.stored.Bucket, tenant, indexes[0].Path)
	require.NoError(t, err)
	require.Equal(t, schema, indexedSchema)
	require.Equal(t, int64(streams.ShardFactor), shardCount)
	require.NotEmpty(t, sections)

	runs := v2.CalculateRuns(sections, compareLogSortPrefix)
	require.Len(t, runs, 1, "reachable log objects must form one globally ordered schema run")

	outputPaths := make(map[string]struct{})
	var orderedKeys []logSortPrefix
	for _, sectionRef := range runs[0].Sections() {
		outputPaths[sectionRef.ObjectPath] = struct{}{}
		obj, err := dataobj.FromBucket(t.Context(), scenario.stored.Bucket, sectionRef.ObjectPath, 0)
		require.NoError(t, err)
		streamKeys := make(map[int64]logSortPrefix)
		for _, stream := range fixtures.ReadTenantStreams(t, obj, tenant) {
			streamKeys[stream.ID] = scenarioStreamSortKey(t, stream, schema)
		}
		require.GreaterOrEqual(t, sectionRef.SectionIndex, int64(0))
		for _, record := range fixtures.ReadTenantLogSection(t, obj, tenant, int(sectionRef.SectionIndex)) {
			require.Contains(t, streamKeys, record.StreamID, "log stream must be present in %q", sectionRef.ObjectPath)
			orderedKeys = append(orderedKeys, streamKeys[record.StreamID])
		}
	}
	require.Greater(t, len(outputPaths), 1, "compaction should produce more than one reachable log file")
	require.Len(t, orderedKeys, wantRows)
	require.True(t, slices.IsSortedFunc(orderedKeys, compareLogSortPrefix),
		"log sort keys must be nondecreasing across object and section boundaries: %v", orderedKeys)
}

func scenarioStreamSortKey(t *testing.T, stream streams.Stream, schema []string) logSortPrefix {
	t.Helper()
	values := make([]string, len(schema))
	for i, label := range schema {
		kind, name, ok := strings.Cut(label, ":")
		require.True(t, ok, "schema key %q must be qualified", label)
		require.Equal(t, "label", kind, "schema key %q must reference a stream label", label)
		require.NotEmpty(t, name, "schema key %q must name a label", label)
		values[i] = stream.Labels.Get(name)
	}
	return logSortPrefix{shard: uint32(stream.ShardBucket), labels: values}
}

// newIntegrationCoordinator owns an in-process scheduler and worker sharing the
// supplied bucket. Cleanup hooks stop the worker before the scheduler.
func newIntegrationCoordinator(ctx context.Context, t *testing.T, bucket objstore.Bucket, now time.Time, logsobjConfig *logsobj.BuilderBaseConfig) *coordinator {
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
	activeServices := []services.Service{sched.Service()}
	t.Cleanup(func() {
		for _, service := range slices.Backward(activeServices) {
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = services.StopAndAwaitTerminated(stopCtx, service)
			cancel()
		}
	})

	ms := metastore.NewObjectMetastore(bucket, metastore.Config{}, log.NewNopLogger(),
		metastore.NewObjectMetastoreMetrics(prometheus.NewRegistry()))

	var compactionCfg Config
	flagext.DefaultValues(&compactionCfg)
	if logsobjConfig != nil {
		compactionCfg.LogsobjBuilder = *logsobjConfig
	}
	compactionCfg.LogMinCompactionSize = 0 // disabled
	compactionCfg.ToCConsolidateTimeout = 10 * time.Second
	require.NoError(t, compactionCfg.Validate())

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
	activeServices = append(activeServices, w.Service())

	run := func(runCtx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
		return runPlan(runCtx, log.NewNopLogger(), sched, opts, plan)
	}
	return &coordinator{
		cfg:             compactionCfg,
		logger:          log.NewNopLogger(),
		bucket:          bucket,
		indexDispatcher: &planDispatcher{runPlan: run, limit: compactionCfg.MaxRunningCompactionTasks},
		logDispatcher:   &planDispatcher{runPlan: run, limit: compactionCfg.LogMaxRunningCompactionTasks},
		publisher: &tocPublisher{
			writer:  metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger()),
			timeout: compactionCfg.ToCConsolidateTimeout,
			dryRun:  compactionCfg.DryRun,
		},
		clock:   func() time.Time { return now },
		metrics: newCoordinatorMetrics(prometheus.NewRegistry()),
	}
}

type integrationSortSchema []string

func (s integrationSortSchema) SortSchemaLabels(string) []string { return s }
func (integrationSortSchema) CompactionPhases(string) (bool, bool) {
	return true, true
}

// mustLoadTenantIndexes loads the ToC of every tenant in the window, keyed by tenant.
func mustLoadTenantIndexes(ctx context.Context, t *testing.T, b objstore.Bucket, window time.Time) map[string][]indexEntry {
	t.Helper()
	tenants, err := metastore.ListTableOfContentsTenants(ctx, b, window)
	require.NoError(t, err)
	out := make(map[string][]indexEntry, len(tenants))
	for _, tenant := range tenants {
		got, err := loadTenantIndexes(ctx, b, window, tenant)
		require.NoError(t, err)
		out[tenant] = got
	}
	return out
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
