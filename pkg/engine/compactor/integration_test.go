package compactor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

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
	window      time.Time
	coordinator *coordinator
}

type scenarioInput struct {
	window        time.Time
	logs          []compactortest.Source
	sortSchema    []string
	logsobjConfig *logsobj.BuilderBaseConfig
}

func (s *scenarioInput) addLogSource(src compactortest.Source) {
	s.logs = append(s.logs, src)
}

// newCompactorScenario wraps compactortest.New without needing to export the coordinator
func newCompactionScenario(ctx context.Context, t *testing.T, input scenarioInput) *compactionScenario {
	t.Helper()
	seeded := compactortest.New(ctx, t, input.window, input.logs)

	s := &compactionScenario{
		ctx:         ctx,
		t:           t,
		stored:      seeded,
		window:      input.window,
		coordinator: newIntegrationCoordinatorWithLogsobjConfig(ctx, t, seeded.Bucket, input.window.Add(time.Hour), input.logsobjConfig),
	}
	s.coordinator.limits = integrationSortSchema(input.sortSchema)
	return s
}

// scenarioSnapshot is read back from the bucket after a step, rather than
// inferred from task results. Contents are only available for log indexes.
type scenarioSnapshot struct {
	indexes  []indexpointers.IndexPointer
	contents compactortest.Contents
}

func (s *compactionScenario) runLogMerge(tenant string) phaseOutcome {
	s.t.Helper()
	return s.coordinator.runLogMergePhase(s.ctx, tenant, s.window)
}

func (s *compactionScenario) runIndexMerge(tenant string) phaseOutcome {
	s.t.Helper()
	return s.coordinator.runIndexMergePhase(s.ctx, tenant, s.window)
}

func (s *compactionScenario) snapshot(tenant string) scenarioSnapshot {
	s.t.Helper()
	indexes := s.stored.Indexes(s.ctx, s.t, tenant)
	return scenarioSnapshot{indexes: indexes, contents: s.stored.ReadContents(s.ctx, s.t, tenant, indexes)}
}

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
				schema: []string{"label:app"}, shardCount: streams.ShardFactor, sourceIndexes: []int{0, 1},
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
				schema: []string{"label:cluster"}, shardCount: streams.ShardFactor, sourceIndexes: []int{0, 1},
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
				schema: []string{"label:app"}, sourceIndexes: []int{0},
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
				schema: []string{"label:app"}, shardCount: streams.ShardFactor / 2, sourceIndexes: []int{0, 1},
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

			bucket := objstore.NewInMemBucket()
			for i, sourcePath := range sourcePaths {
				layout := test.sourceLayouts[i]
				entries := fixtures.NewLogsFixtureBuilder(t,
					fixtures.WithSchemaLabels(layout.SchemaLabels...),
					fixtures.WithShardCount(layout.ShardCount),
				)
				entries.ForStream(`{app="api",cluster="prod"}`).
					Entry(int(base.Add(time.Second).Unix()), "{}", sourcePath+"/second").
					Entry(int(base.Unix()), "{}", sourcePath+"/first")
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
				reader, err := obj.Reader(ctx)
				require.NoError(t, err)
				require.NoError(t, bucket.Upload(ctx, sourcePath, reader))
				require.NoError(t, reader.Close())
			}
			var tocIndexes []testIndex
			for i, group := range test.indexGroups {
				indexPath := fmt.Sprintf("indexes/%02d/log-sources", i)
				var groupSources []string
				for _, sourceIndex := range group.sourceIndexes {
					groupSources = append(groupSources, sourcePaths[sourceIndex])
				}
				seedLogCompactionIndex(ctx, t, bucket, indexPath, tenant, groupSources, group.schema, group.shardCount, base)
				tocIndexes = append(tocIndexes, testIndex{
					path: indexPath, start: base, end: base.Add(time.Second),
					uncompressedLogsSize: uint64(len(groupSources) * 100),
				})
			}
			writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{tenant: tocIndexes})
			c := newIntegrationCoordinator(ctx, t, bucket, base)
			c.limits = integrationSortSchema(targetSchema)

			before := mustLoadTenant(ctx, t, bucket, window, tenant)
			require.Len(t, before, len(test.indexGroups))

			require.Equal(t, phaseOutcomeSwapped, c.runLogMergePhase(ctx, tenant, window))

			after := mustLoadTenant(ctx, t, bucket, window, tenant)
			require.Len(t, after, test.expectedIndexes)
			stored := &compactortest.Scenario{Bucket: bucket, Window: window}
			contents := stored.ReadContents(ctx, t, tenant, stored.Indexes(ctx, t, tenant))
			require.ElementsMatch(t, expectedSourceLogLines(sourcePaths), contents.Lines,
				"every source log line must remain reachable through the ToC and index")
			require.Equal(t, int64(len(contents.Lines)), contents.StatsRowCount,
				"index stats must account for every reachable log row")
			require.Equal(t, contents.StatsObjectPaths, contents.PostingsObjectPaths,
				"stats and postings must reference the same log objects")
			require.Equal(t, map[string]bool{"label:app": true}, contents.SortSchemas)
			for _, layout := range contents.Layouts {
				require.True(t, logsobj.EqualSortLayout(logsobj.TargetSortLayout(targetSchema), layout))
			}
			for _, entry := range after {
				require.True(t, entry.Start.Equal(base))
				require.True(t, entry.End.Equal(base.Add(time.Second)))
				require.Positive(t, entry.FileSize)
				exists, err := bucket.Exists(ctx, entry.Path)
				require.NoError(t, err)
				require.True(t, exists, "replacement index must exist")
			}
		})
	}
}

func TestCompactionScenario_FiveOverlappingLogFilesConverge(t *testing.T) {
	runFiveFileConvergence(t, nil, nil)
}

func TestCompactionScenario_WrongSortSchemaConverges(t *testing.T) {
	mismatchedSchemas := func(file int) []string {
		if file == 2 {
			return []string{"label:cluster"}
		}
		return []string{"label:app"}
	}
	runFiveFileConvergence(t, mismatchedSchemas, nil)
}

func TestCompactionScenario_WrongShardCountConverges(t *testing.T) {
	t.Skipf("Customizing the number of shard buckets is not yet supported in the indexer")
	mismatchedShardBuckets := func(file int) uint32 {
		if file == 2 {
			return streams.ShardFactor / 2
		}
		return streams.ShardFactor
	}
	runFiveFileConvergence(t, nil, mismatchedShardBuckets)
}

// runFiveFileConvergence creates five overlapping objects and expects them to
// converge into a single sorted run after any necessary re-sorting.
func runFiveFileConvergence(t *testing.T, schemaForFile func(int) []string, shardCountForFile func(int) uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const tenant = "acme"
	schema := []string{"label:app"}
	if schemaForFile == nil {
		schemaForFile = func(int) []string { return schema }
	}
	if shardCountForFile == nil {
		shardCountForFile = func(int) uint32 { return streams.ShardFactor }
	}

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
	for file := range 5 {
		sourceSchema := schemaForFile(file)
		shardCount := shardCountForFile(file)
		options := logs.BuilderOptions{
			PageSizeHint: 2048, PageMaxRowCount: 10000, BufferSize: 2048 * 8,
			StripeMergeLimit: 2, AppendStrategy: logs.AppendOrdered,
			SortOrder: logs.SortSchemaASC, SchemaLabels: sourceSchema,
			StreamOrder: logs.StreamOrderStableHashV1, ShardCount: shardCount,
		}

		entries := fixtures.NewLogsFixtureBuilder(t,
			fixtures.WithSchemaLabels(sourceSchema...), fixtures.WithShardCount(shardCount),
		)
		for app := 'a'; app <= 'z'; app++ {
			stream := entries.ForStream(fmt.Sprintf(`{app=%q}`, string(app)))
			for record := range 3 {
				stream.Entry(int(base.Unix())+record, "{}", fmt.Sprintf("file-%d/app-%c/record-%d:%s", file, app, record, payload))
			}
		}

		obj, closer := fixtures.DataObject(t,
			fixtures.StreamsSection(t, tenant, entries.Streams()),
			fixtures.LogsSection(t, tenant, entries.Logs(), fixtures.WithBuilderOptions(&options)),
		)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		path := fmt.Sprintf("objects/source-%d", file)
		input.addLogSource(compactortest.Source{Path: path, Object: obj})
	}

	scenario := newCompactionScenario(ctx, t, input)
	indexes := scenario.stored.Indexes(ctx, t, tenant)
	require.Len(t, indexes, 5)
	for file, source := range input.logs {
		indexPath := compactortest.IndexPath(file)
		var indexFound bool
		for _, entry := range indexes {
			if entry.Path == indexPath {
				indexFound = true
				break
			}
		}
		require.True(t, indexFound, "index for source %q must be in the ToC", source.Path)

		// Check the file pointed to by this index is the correct shape according to the index
		refs, indexedSchema, shardCount, err := logSectionRefsFor(ctx, scenario.stored.Bucket, tenant, indexPath)
		require.NoError(t, err)
		require.NotEmpty(t, refs)
		require.Equal(t, source.Path, refs[0].Ref.ObjectPath)
		require.Equal(t, schemaForFile(file), indexedSchema)
		require.Equal(t, int64(shardCountForFile(file)), shardCount)

		// Check the actual logs section pointed to by this index is the correct shape too
		obj, err := dataobj.FromBucket(ctx, scenario.stored.Bucket, source.Path, 0)
		require.NoError(t, err)
		foundLogs := false
		for _, section := range obj.Sections().Filter(logs.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			foundLogs = true
			opened, err := logs.Open(ctx, section)
			require.NoError(t, err)
			require.Equal(t, schemaForFile(file), opened.SortLayout().SchemaLabels)
			require.Equal(t, shardCountForFile(file), opened.SortLayout().ShardCount)
		}
		require.True(t, foundLogs, "source %q must contain tenant logs", source.Path)
	}

	// Run compaction
	converged := false
	for cycle := range 10 {
		indexStepOutcome := scenario.runIndexMerge(tenant)
		require.NotEqual(t, phaseOutcomeError, indexStepOutcome, "index phase failed in cycle %d", cycle)
		logsStepOutcome := scenario.runLogMerge(tenant)
		require.NotEqual(t, phaseOutcomeError, logsStepOutcome, "log phase failed in cycle %d", cycle)
		if indexStepOutcome == phaseOutcomeNoWork && logsStepOutcome == phaseOutcomeNoWork {
			converged = true
			break
		}
	}
	require.True(t, converged, "compaction did not reach a no-work cycle")

	final := scenario.snapshot(tenant)
	sourceLines := readScenarioSourceLines(t, ctx, scenario, input.logs, tenant)
	require.Len(t, sourceLines, 5*streamCount*3, "seeded source objects must contain all records")
	requireConvergedContents(t, final, schema, sourceLines)
	requireSingleSortedRun(t, ctx, scenario, final, tenant, schema, len(sourceLines))
}

func readScenarioSourceLines(t *testing.T, ctx context.Context, scenario *compactionScenario, sources []compactortest.Source, tenant string) []string {
	t.Helper()
	var sourceLines []string
	for _, source := range sources {
		obj, err := dataobj.FromBucket(ctx, scenario.stored.Bucket, source.Path, 0)
		require.NoError(t, err)
		for _, record := range fixtures.ReadTenantLogs(t, ctx, obj, tenant) {
			sourceLines = append(sourceLines, string(record.Line))
		}
	}
	return sourceLines
}

func requireConvergedContents(t *testing.T, final scenarioSnapshot, schema []string, sourceLines []string) {
	t.Helper()
	require.Len(t, final.indexes, 1, "all overlapping indexes should consolidate")
	require.ElementsMatch(t, sourceLines, final.contents.Lines, "all source records must be reachable exactly once")
	require.Equal(t, int64(len(sourceLines)), final.contents.StatsRowCount)
	require.Equal(t, final.contents.StatsObjectPaths, final.contents.PostingsObjectPaths)
	require.Equal(t, map[string]bool{strings.Join(schema, ","): true}, final.contents.SortSchemas)
	require.NotEmpty(t, final.contents.Layouts)
	for _, layout := range final.contents.Layouts {
		require.True(t, logsobj.EqualSortLayout(logsobj.TargetSortLayout(schema), layout))
	}
}

func requireSingleSortedRun(t *testing.T, ctx context.Context, scenario *compactionScenario, final scenarioSnapshot, tenant string, schema []string, wantRows int) {
	t.Helper()
	var sections []v2.Section[logSortPrefix]
	for _, entry := range final.indexes {
		refs, indexedSchema, shardCount, err := logSectionRefsFor(ctx, scenario.stored.Bucket, tenant, entry.Path)
		require.NoError(t, err)
		require.Equal(t, schema, indexedSchema)
		require.Equal(t, int64(streams.ShardFactor), shardCount)
		sections = append(sections, refs...)
	}
	require.NotEmpty(t, sections)
	runs := v2.CalculateRuns(sections, compareLogSortPrefix)
	require.Len(t, runs, 1, "reachable log objects must form one globally ordered schema run")
	outputPaths := make(map[string]struct{})
	for _, section := range sections {
		outputPaths[section.Ref.ObjectPath] = struct{}{}
	}
	require.Greater(t, len(outputPaths), 1, "compaction should produce more than one reachable log file")

	var last logSortPrefix
	haveLast := false
	var orderedRows int
	for _, sectionRef := range runs[0].Sections() {
		obj, err := dataobj.FromBucket(ctx, scenario.stored.Bucket, sectionRef.ObjectPath, 0)
		require.NoError(t, err)
		streamKeys := make(map[int64]logSortPrefix)
		for _, stream := range fixtures.ReadTenantStreams(t, ctx, obj, tenant) {
			values := make([]string, len(schema))
			for i, label := range schema {
				kind, name, ok := strings.Cut(label, ":")
				require.True(t, ok && kind == "label" && name != "")
				values[i] = stream.Labels.Get(name)
			}
			streamKeys[stream.ID] = logSortPrefix{shard: uint32(stream.ShardBucket), labels: values}
		}
		require.GreaterOrEqual(t, sectionRef.SectionIndex, int64(0))
		for _, record := range fixtures.ReadTenantLogSection(t, ctx, obj, tenant, int(sectionRef.SectionIndex)) {
			key, ok := streamKeys[record.StreamID]
			require.True(t, ok, "log stream %d must be present in %q", record.StreamID, sectionRef.ObjectPath)
			if haveLast {
				require.LessOrEqual(t, compareLogSortPrefix(last, key), 0,
					"schema order regressed at %q in %q section %d. Prev sort key %q, current sort key %q", record.Line, sectionRef.ObjectPath, sectionRef.SectionIndex, last, key)
			}
			last, haveLast = key, true
			orderedRows++
		}
	}
	require.Equal(t, wantRows, orderedRows)
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

func seedLogCompactionIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, path, tenant string, sourcePaths, sortSchema []string, shardCount int64, ts time.Time) {
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
