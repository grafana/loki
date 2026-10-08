package compactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// fakeRunner records each runPlan invocation. Tests pass fakeRunner.run to the
// coordinator's dispatchers instead of a real scheduler and worker pair.
type fakeRunner struct {
	mu    sync.Mutex
	calls []runCall
	err   error // returned from every call when non-nil

	// failOnCall, when > 0, makes the Nth run invocation (1-based) return an
	// error while all others succeed. Used to simulate one failed job.
	failOnCall int

	// respond, when non-nil, decides the result of every call after run
	// records it. It overrides err, failOnCall and the default artifact.
	respond runFunc
}

type runCall struct {
	opts workflow.Options
	plan *physical.Plan
	path string
}

func (f *fakeRunner) run(ctx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
	f.mu.Lock()
	n := len(f.calls) + 1
	path := fmt.Sprintf("indexes/tenants/test/aa/artifact-%02d", n)
	f.calls = append(f.calls, runCall{opts: opts, plan: plan, path: path})
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(ctx, opts, plan)
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.failOnCall > 0 && n == f.failOnCall {
		return nil, errors.New("fakeRunner: forced failure on call")
	}
	return &v2.ResultArtifact{Path: path}, nil
}

func (f *fakeRunner) snapshot() []runCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runCall(nil), f.calls...)
}

func (f *fakeRunner) assertUniqueObjects(t *testing.T) {
	t.Helper()

	// An object is one run: many sections of the same path may share a run,
	// but that path must not appear in any other run or dispatched task.
	seen := map[string]struct{}{}
	for _, call := range f.calls {
		root, err := call.plan.Root()
		require.NoError(t, err)

		err = call.plan.Graph().Walk(root, func(n physical.Node) error {
			var runs []*compactionv2pb.RunRef
			switch n := n.(type) {
			case *physical.LogMerge:
				runs = n.Runs
			case *physical.IndexMerge:
				runs = n.Runs
			case *physical.IndexFilter:
				for _, path := range n.ObjectPaths {
					runs = append(runs, &compactionv2pb.RunRef{Sections: []*compactionv2pb.SectionRef{{ObjectPath: path}}})
				}
			default:
				return nil
			}
			for _, run := range runs {
				if run == nil {
					continue
				}
				runObjects := map[string]struct{}{}
				for _, sectionRef := range run.Sections {
					if sectionRef == nil {
						continue
					}
					runObjects[sectionRef.ObjectPath] = struct{}{}
				}
				for obj := range runObjects {
					_, exists := seen[obj]
					require.False(t, exists, "object %q appears in more than one task", obj)
					seen[obj] = struct{}{}
				}
			}
			return nil
		}, dag.PreOrderWalk)
		require.NoError(t, err)
	}
}

// fakeReplacer records each ReplaceIndexPointers invocation and returns
// configurable (swapped, err) tuples.
type fakeReplacer struct {
	mu      sync.Mutex
	calls   []replaceCall
	swapped bool
	err     error
}

type replaceCall struct {
	window     time.Time
	tenant     string
	oldPaths   []string
	newEntries []metastore.TableOfContentsEntry
}

func (f *fakeReplacer) ReplaceIndexPointers(
	_ context.Context,
	window time.Time,
	tenant string,
	oldPaths []string,
	newEntries []metastore.TableOfContentsEntry,
) (bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, replaceCall{window, tenant, append([]string(nil), oldPaths...), append([]metastore.TableOfContentsEntry(nil), newEntries...)})
	f.mu.Unlock()
	return f.swapped, f.err
}

func (f *fakeReplacer) snapshot() []replaceCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]replaceCall(nil), f.calls...)
}

// errBucket wraps a Bucket but fails Get with a non-not-found error, to exercise
// the transient-read-error path in discoverTenants.
type errBucket struct{ objstore.Bucket }

func (errBucket) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("errBucket: forced read failure")
}
func (errBucket) Iter(context.Context, string, func(string) error, ...objstore.IterOption) error {
	return errors.New("errBucket: forced list failure")
}
func (errBucket) IsObjNotFoundErr(error) bool { return false }

// newTestCoordinator builds a Coordinator wired to the supplied fakes plus a
// default-configured Config (loop-friendly TTLs, K=2 so two indexes split
// into two single-pile tasks).
func newTestCoordinator(t *testing.T, bucket objstore.Bucket, runner *fakeRunner, replacer *fakeReplacer, clock func() time.Time, limits Limits) *coordinator {
	t.Helper()
	if limits == nil {
		limits = newFakeLimits() // enables nothing by default
	}
	cfg := Config{
		Enabled:              true,
		PollingInterval:      5 * time.Minute,
		MaxRunsPerTask:       2,
		LogMaxRunsPerTask:    2,
		LogMinCompactionSize: 1,
		PlanVersion:          1,
		Scheduler:            SchedulerConfig{Endpoint: defaultEndpoint},
	}
	return &coordinator{
		cfg:             cfg,
		logger:          log.NewNopLogger(),
		bucket:          bucket,
		indexDispatcher: &planDispatcher{runPlan: runner.run, limit: 4},
		logDispatcher:   &planDispatcher{runPlan: runner.run},
		publisher:       &tocPublisher{writer: replacer, timeout: 30 * time.Second},
		clock:           clock,
		sleep:           sleepUntil,
		metrics:         newCoordinatorMetrics(prometheus.NewRegistry()),
		limits:          limits,

		logMergePlanningStrategy: newTestLogMergePlanningStrategy(t, cfg.LogMaxRunsPerTask),
	}
}

func newTestLogMergePlanningStrategy(t *testing.T, k int) *v2.SizeLeveledStrategy {
	t.Helper()
	s, err := v2.NewSizeLeveledStrategy(v2.DefaultSizeLevelBase, v2.DefaultSizeLevelRatio, k)
	require.NoError(t, err)
	return s
}

// fixedClock returns a clock function pinned to t.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func sectionRefNames(refs []*compactionv2pb.SectionRef) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = fmt.Sprintf("%s#%d", ref.ObjectPath, ref.SectionIndex)
	}
	return names
}

func mergeNodeRuns(t *testing.T, plan *physical.Plan) []*compactionv2pb.RunRef {
	t.Helper()
	root, err := plan.Root()
	require.NoError(t, err)
	switch n := root.(type) {
	case *physical.LogMerge:
		return n.Runs
	case *physical.IndexMerge:
		return n.Runs
	default:
		t.Fatalf("plan root is %T, want LogMerge or IndexMerge", root)
		return nil
	}
}

// planObjectPaths returns the sorted object paths that a LogMerge,
// IndexMerge, IndexFilter, or SortObject plan reads.
func planObjectPaths(t *testing.T, plan *physical.Plan) []string {
	t.Helper()
	root, err := plan.Root()
	require.NoError(t, err)
	var paths []string
	switch n := root.(type) {
	case *physical.IndexFilter:
		paths = slices.Clone(n.ObjectPaths)
	case *physical.SortObject:
		paths = []string{n.SourceObjectPath}
	default:
		for _, run := range mergeNodeRuns(t, plan) {
			for _, section := range run.Sections {
				paths = append(paths, section.ObjectPath)
			}
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// callByActor returns the only call in calls that ran under actor.
func callByActor(t *testing.T, calls []runCall, actor string) runCall {
	t.Helper()
	var found []runCall
	for _, call := range calls {
		if slices.Equal(call.opts.Actor, []string{"compaction", actor}) {
			found = append(found, call)
		}
	}
	require.Len(t, found, 1, "want exactly one %s call", actor)
	return found[0]
}

func buildOverlappingPostingsIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string) {
	t.Helper()
	buildIndexWithPostings(ctx, t, bucket, tenant, path, 1<<20, []postings.Row{
		{Kind: postings.KindLabel, ObjectPath: path + ".log-0", ColumnName: "service_name", LabelValue: "a", MinTimestamp: 10, MaxTimestamp: 20},
		{Kind: postings.KindLabel, ObjectPath: path + ".log-1", ColumnName: "service_name", LabelValue: "z", MinTimestamp: 30, MaxTimestamp: 40},
	})
}

func buildCurrentIndexWithStats(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, rows []stats.Stat) {
	t.Helper()
	seen := make(map[string]bool)
	var postingRows []postings.Row
	for _, row := range rows {
		key := fmt.Sprintf("%s#%d", row.ObjectPath, row.SectionIndex)
		if seen[key] {
			continue
		}
		seen[key] = true
		postingRows = append(postingRows, postings.Row{
			Kind:         postings.KindLabel,
			ObjectPath:   row.ObjectPath,
			SectionIndex: row.SectionIndex,
			ColumnName:   "layout",
			LabelValue:   "current",
			ShardBuckets: streams.ShardFactor,
		})
	}
	buildIndex(ctx, t, bucket, testIndexObject{
		tenant:      tenant,
		path:        path,
		sectionSize: 1 << 21,
		stats:       rows,
		postings:    postingRows,
	})
}

func TestCompactTenantLogs_DispatchesLogMergePlans(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"

	// Two overlapping objects -> 2 runs. Each object's physical sections must
	// stay together and ordered in the dispatched task.
	buildCurrentIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 50},
		{ObjectPath: "logs/log-0", SectionIndex: 1, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 15, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 50},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 50},
		{ObjectPath: "logs/log-1", SectionIndex: 1, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 25, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 50},
	})

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)

	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)
	require.NoError(t, err)

	// A successful log-compaction reports its index/task deltas so the
	// indexes_added/removed and tasks metrics reflect the work (regression:
	// these were previously dropped, leaving the log-compaction path invisible).
	require.Equal(t, 1, stats.removed, "the single converged index is removed")
	require.Equal(t, 1, stats.added, "one merged index is added")
	require.Equal(t, 1, stats.dispatched, "one log-merge task is dispatched")

	dispatches := runner.snapshot()
	require.Len(t, dispatches, 1, "two runs -> one task -> one LogMerge plan")
	require.Equal(t, []string{"compaction", "log-merge"}, dispatches[0].opts.Actor)

	root, err := dispatches[0].plan.Root()
	require.NoError(t, err)
	node, ok := root.(*physical.LogMerge)
	require.True(t, ok)
	require.Equal(t, []string{"label:service_name"}, node.SortSchema)
	require.Len(t, node.Runs, 2)
	require.Equal(t, []string{"logs/log-0#0", "logs/log-0#1"}, sectionRefNames(node.Runs[0].Sections))
	require.Equal(t, []string{"logs/log-1#0", "logs/log-1#1"}, sectionRefNames(node.Runs[1].Sections))

	swaps := replacer.snapshot()
	require.Len(t, swaps, 1, "log path now swaps the ToC after dispatch")
	require.Equal(t, []string{convergedPath}, swaps[0].oldPaths)
}

func TestCompactTenantLogs_DispatchesSortObjectPlans(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	logSortSchema := "label:cluster"
	buildCurrentIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: logSortSchema,
			Labels: map[string]string{"cluster": "dev"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 2, UncompressedSize: 100},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: logSortSchema,
			Labels: map[string]string{"cluster": "prod"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 3, UncompressedSize: 200},
	})

	// The tenant's requested sort-schema must not match the log schema to trigger a sort.
	limits := newFakeLimits("acme")
	require.Equal(t, limits.SortSchemaLabels("acme"), []string{"label:service_name"})
	require.NotEqual(t, limits.SortSchemaLabels("acme"), logSortSchema)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), limits)

	result, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{
		Path: convergedPath,
	})
	require.NoError(t, err)
	require.Equal(t, compactionStats{removed: 1, added: 2, dispatched: 2}, result)

	calls := runner.snapshot()
	require.Len(t, calls, 2)
	sources := make(map[string]bool)
	for _, call := range calls {
		// Check a sort-object was dispatched with the Tenant's requested sort-schema
		require.Equal(t, []string{"compaction", "sort-object"}, call.opts.Actor)
		root, err := call.plan.Root()
		require.NoError(t, err)
		node, ok := root.(*physical.SortObject)
		require.True(t, ok)
		require.Equal(t, []string{"label:service_name"}, node.SortSchema)
		sources[node.SourceObjectPath] = true
	}
	require.Equal(t, map[string]bool{"logs/log-0": true, "logs/log-1": true}, sources)

	swaps := replacer.snapshot()
	require.Len(t, swaps, 1)
	require.Equal(t, []string{convergedPath}, swaps[0].oldPaths)
	requireEntriesMatchCalls(t, calls, swaps[0].newEntries)
}

func TestCompactTenant_DispatchesIndexMergePlans(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	// Two overlapping indexes, each with two physical sections. Sections of
	// the same index must stay together and ordered in the dispatched task.
	buildIndexWithPostingsSections(ctx, t, bucket, "acme", "indexes/a",
		[]postings.Row{{Kind: postings.KindLabel, ObjectPath: "logs/a-0", ColumnName: "service", LabelValue: "a", MinTimestamp: 10, MaxTimestamp: 20}},
		[]postings.Row{{Kind: postings.KindLabel, ObjectPath: "logs/a-1", ColumnName: "service", LabelValue: "z", MinTimestamp: 30, MaxTimestamp: 40}},
	)
	buildIndexWithPostingsSections(ctx, t, bucket, "acme", "indexes/b",
		[]postings.Row{{Kind: postings.KindLabel, ObjectPath: "logs/b-0", ColumnName: "service", LabelValue: "a", MinTimestamp: 15, MaxTimestamp: 25}},
		[]postings.Row{{Kind: postings.KindLabel, ObjectPath: "logs/b-1", ColumnName: "service", LabelValue: "z", MinTimestamp: 35, MaxTimestamp: 45}},
	)

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	stats, err := c.compactTenantIndexes(ctx, "acme", window, []indexEntry{
		{Path: "indexes/a", Start: window.Add(time.Hour), End: window.Add(2 * time.Hour)},
		{Path: "indexes/b", Start: window.Add(time.Hour), End: window.Add(2 * time.Hour)},
	})
	require.NoError(t, err)
	require.Equal(t, compactionStats{removed: 2, added: 1, dispatched: 1}, stats)

	dispatches := runner.snapshot()
	require.Len(t, dispatches, 1)
	require.Equal(t, []string{"compaction", "index-merge"}, dispatches[0].opts.Actor)

	runs := mergeNodeRuns(t, dispatches[0].plan)
	require.Len(t, runs, 2)
	require.Equal(t, []string{"indexes/a#0", "indexes/a#1"}, sectionRefNames(runs[0].Sections))
	require.Equal(t, []string{"indexes/b#0", "indexes/b#1"}, sectionRefNames(runs[1].Sections))

	swaps := replacer.snapshot()
	require.Len(t, swaps, 1)
	require.ElementsMatch(t, []string{"indexes/a", "indexes/b"}, swaps[0].oldPaths)
	requireEntriesMatchCalls(t, dispatches, swaps[0].newEntries)
}

// requireEntriesMatchCalls requires one published entry for each dispatched
// task output.
func requireEntriesMatchCalls(t *testing.T, calls []runCall, entries []metastore.TableOfContentsEntry) {
	t.Helper()
	callPaths := make([]string, len(calls))
	for i, call := range calls {
		callPaths[i] = call.path
	}
	entryPaths := make([]string, len(entries))
	for i, entry := range entries {
		entryPaths[i] = entry.Path
	}
	require.ElementsMatch(t, callPaths, entryPaths)
}

func TestCompactTenant_DoesNotMergeIndexesAcrossSortSchemas(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	var entries []indexEntry

	for _, schema := range []string{"label:service_name", "label:cluster"} {
		for i := range 2 {
			path := fmt.Sprintf("indexes/%s/%d", schema, i)
			buildIndex(ctx, t, bucket, testIndexObject{
				tenant:      "acme",
				path:        path,
				sectionSize: 1 << 20,
				stats: []stats.Stat{{
					ObjectPath:       path + "/logs",
					SectionIndex:     0,
					SortSchema:       schema,
					Labels:           map[string]string{},
					MinTimestamp:     10,
					MaxTimestamp:     20,
					RowCount:         1,
					UncompressedSize: 100,
				}},
				postings: []postings.Row{
					{
						Kind:           postings.KindLabel,
						ObjectPath:     path + "/logs",
						SectionIndex:   0,
						ColumnName:     "common",
						LabelValue:     "a",
						MinTimestamp:   10,
						MaxTimestamp:   20,
						ShardBuckets:   streams.ShardFactor,
						MinShardBucket: 0,
						MaxShardBucket: streams.ShardFactor - 1,
					},
					{
						Kind:           postings.KindLabel,
						ObjectPath:     path + "/logs",
						SectionIndex:   0,
						ColumnName:     "common",
						LabelValue:     "z",
						MinTimestamp:   30,
						MaxTimestamp:   40,
						ShardBuckets:   streams.ShardFactor,
						MinShardBucket: 0,
						MaxShardBucket: streams.ShardFactor - 1,
					},
				},
			})
			entries = append(entries, indexEntry{Path: path, Start: window, End: window.Add(time.Hour)})
		}
	}

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	result, err := c.compactTenantIndexes(ctx, "acme", window, entries)
	require.NoError(t, err)
	require.Equal(t, compactionStats{removed: 4, added: 2, dispatched: 2}, result)

	calls := runner.snapshot()
	require.Len(t, calls, 2)
	for _, call := range calls {
		root, err := call.plan.Root()
		require.NoError(t, err)
		node, ok := root.(*physical.IndexMerge)
		require.True(t, ok)
		var schemas = make(map[string]bool)
		for _, run := range node.Runs {
			for _, section := range run.Sections {
				if strings.Contains(section.ObjectPath, "label:service_name") {
					schemas["label:service_name"] = true
				}
				if strings.Contains(section.ObjectPath, "label:cluster") {
					schemas["label:cluster"] = true
				}
			}
		}
		require.Len(t, schemas, 1, "one IndexMerge task must contain only one sort schema")
	}
	require.Len(t, replacer.snapshot(), 2, "each schema group is swapped independently")
}

func TestCompactTenantLogs_NoStatsRowsForTenantIsConverged(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/other-tenant"

	// Index has a stats section (so it flushes) but for a DIFFERENT tenant;
	// "acme" gets zero refs -> no tasks -> converged.
	buildCurrentIndexWithStats(ctx, t, bucket, "other", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)
	require.NoError(t, err)
	require.Empty(t, runner.snapshot())
}

func TestCompactTenantLogs_InternalObjectOverlapIsConverged(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"

	// Overlapping physical sections in one object are one planning unit and do
	// not trigger a rewrite by themselves.
	buildCurrentIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
		{ObjectPath: "logs/log-0", SectionIndex: 1, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.Zero(t, stats.added)
	require.Empty(t, runner.snapshot(), "terminal window dispatches no plans")
	require.Empty(t, replacer.snapshot(), "terminal window performs no swap")
}

func TestCompactTenantLogs_SamePrefixMergesRegardlessOfTimestamp(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	indexPath := "indexes/aa/converged"

	buildCurrentIndexWithStats(ctx, t, bucket, "acme", indexPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, UncompressedSize: 100},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 30, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	stats, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{Path: indexPath})
	require.NoError(t, err)
	require.Equal(t, compactionStats{removed: 1, added: 1, dispatched: 1}, stats)
	require.Len(t, runner.snapshot(), 1)
	require.Len(t, replacer.snapshot(), 1)
}

func TestCompactTenantLogs_TerminalBelowFloorSkips(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"

	// Two overlapping same-tuple rows -> P=2, total size 30, below the 1GiB floor.
	buildCurrentIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 10},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 20},
	})

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))
	c.cfg.LogMinCompactionSize = 1 << 30 // 1GiB floor; 30 bytes is below it

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.Zero(t, stats.added)
	require.Empty(t, runner.snapshot())
	require.Empty(t, replacer.snapshot())
}

func twoRunConvergedBucket(ctx context.Context, t *testing.T, tenant, path string) objstore.Bucket {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	buildCurrentIndexWithStats(ctx, t, bucket, tenant, path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
	})
	return bucket
}

func overlappingIndexesBucket(ctx context.Context, t *testing.T, window time.Time, tenant string, paths ...string) objstore.Bucket {
	t.Helper()
	if len(paths) == 0 {
		paths = []string{"indexes/aa/src-0", "indexes/bb/src-1"}
	}
	bucket := objstore.NewInMemBucket()
	entries := make([]testIndex, 0, len(paths))
	for i, path := range paths {
		buildOverlappingPostingsIndex(ctx, t, bucket, tenant, path)
		entries = append(entries, testIndex{
			path:  path,
			start: window.Add(time.Duration(i+1) * time.Hour),
			end:   window.Add(time.Duration(i+2) * time.Hour),
		})
	}
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{tenant: entries})
	return bucket
}

// TestCompactJobToCEdgeCases covers the shared post-plan contract of compactTenant
// and compactTenantLogs: dispatch jobs, then swap / skip / fail the ToC.
func TestCompactJobToCEdgeCases(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	type compactKind struct {
		name string
		seed func(ctx context.Context, t *testing.T, window time.Time) (objstore.Bucket, func(*coordinator) (compactionStats, error))
	}
	kinds := []compactKind{
		{
			name: "logs",
			seed: func(ctx context.Context, t *testing.T, window time.Time) (objstore.Bucket, func(*coordinator) (compactionStats, error)) {
				path := "indexes/aa/converged"
				bucket := twoRunConvergedBucket(ctx, t, "acme", path)
				entry := indexEntry{Path: path, Start: window.Add(time.Hour), End: window.Add(2 * time.Hour)}
				return bucket, func(c *coordinator) (compactionStats, error) {
					return c.compactTenantLogs(ctx, "acme", window, entry)
				}
			},
		},
		{
			name: "index",
			seed: func(ctx context.Context, t *testing.T, window time.Time) (objstore.Bucket, func(*coordinator) (compactionStats, error)) {
				bucket := overlappingIndexesBucket(ctx, t, window, "acme")
				return bucket, func(c *coordinator) (compactionStats, error) {
					indexes, err := loadTenantIndexes(ctx, bucket, window, "acme")
					require.NoError(t, err)
					return c.compactTenantIndexes(ctx, "acme", window, indexes)
				}
			},
		},
	}

	edges := []struct {
		name       string
		failOnCall int
		swapped    bool
		swapErr    error
		wantErr    bool
		wantSwap   bool
		wantAdded  int
	}{
		{name: "swap_ok", swapped: true, wantSwap: true, wantAdded: 1},
		{name: "swap_error", swapErr: errors.New("boom"), wantErr: true, wantSwap: true},
		{name: "race_loss", swapped: false, wantSwap: true, wantAdded: 0},
		{name: "job_failure", failOnCall: 1, swapped: true, wantErr: true},
	}

	for _, kind := range kinds {
		for _, edge := range edges {
			t.Run(kind.name+"/"+edge.name, func(t *testing.T) {
				ctx := context.Background()
				bucket, run := kind.seed(ctx, t, window)
				runner := &fakeRunner{failOnCall: edge.failOnCall}
				defer runner.assertUniqueObjects(t)
				replacer := &fakeReplacer{swapped: edge.swapped, err: edge.swapErr}
				c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

				stats, err := run(c)
				if edge.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.NotEmpty(t, runner.snapshot(), "jobs must be dispatched")

				swaps := replacer.snapshot()
				if edge.wantSwap {
					require.Len(t, swaps, 1)
					require.NotEmpty(t, swaps[0].newEntries)
				} else {
					require.Empty(t, swaps)
				}
				if !edge.wantErr {
					require.Equal(t, edge.wantAdded, stats.added)
				}
			})
		}
	}
}

// TestCompact_SplitsWhenRunsExceedK checks that multiple tasks are dispatched when the planning steps detect more Runs than allowed Runs per task (2)
func TestCompact_SplitsWhenRunsExceedK(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	t.Run("logs", func(t *testing.T) {
		ctx := context.Background()
		path := "indexes/aa/converged"
		bucket := objstore.NewInMemBucket()
		buildCurrentIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
			{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
			{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
			{ObjectPath: "logs/log-2", SectionIndex: 0, SortSchema: "label:service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 25, MaxTimestamp: 50, RowCount: 1, UncompressedSize: 100},
		})

		runner := &fakeRunner{}
		defer runner.assertUniqueObjects(t)
		replacer := &fakeReplacer{swapped: true}
		c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

		stats, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{
			Path: path, Start: window.Add(time.Hour), End: window.Add(2 * time.Hour),
		})
		require.NoError(t, err)
		require.Equal(t, 2, stats.dispatched, "3 overlapping runs with K=2 must split into 2 tasks")
		require.Equal(t, 2, stats.added)

		dispatches := runner.snapshot()
		require.Len(t, dispatches, 2)
		require.Equal(t, 3, countPlannedObjects(t, dispatches))
	})

	t.Run("index", func(t *testing.T) {
		ctx := context.Background()
		bucket := overlappingIndexesBucket(ctx, t, window, "acme",
			"indexes/aa/src-0", "indexes/bb/src-1", "indexes/cc/src-2")

		runner := &fakeRunner{}
		defer runner.assertUniqueObjects(t)
		replacer := &fakeReplacer{swapped: true}
		c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

		indexes, err := loadTenantIndexes(ctx, bucket, window, "acme")
		require.NoError(t, err)
		stats, err := c.compactTenantIndexes(ctx, "acme", window, indexes)
		require.NoError(t, err)
		require.Equal(t, 2, stats.dispatched, "3 overlapping runs with K=2 must split into 2 tasks")
		require.Equal(t, 2, stats.added)

		dispatches := runner.snapshot()
		require.Len(t, dispatches, 2)
		require.Equal(t, 3, countPlannedObjects(t, dispatches))
	})
}

func TestCompactTenantLogs_SizeLevelTrigger(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	const sourcePath = "indexes/aa/levels"
	stat := func(path string, section, minTS, maxTS, size int64) stats.Stat {
		return stats.Stat{ObjectPath: path, SectionIndex: section, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: minTS, MaxTimestamp: maxTS, RowCount: 1, UncompressedSize: size}
	}

	tests := []struct {
		name         string
		rows         []stats.Stat
		wantMerges   [][]string
		wantFiltered []string
		// wantFilterRange is the ToC time range of the filtered index, in
		// Unix nanoseconds.
		wantFilterRange [2]int64
	}{
		{
			name: "skips overlapping runs when no size level holds k runs",
			rows: []stats.Stat{stat("logs/small", 0, 10, 30, 100), stat("logs/large", 0, 10, 30, 20<<30)},
		},
		{
			name: "merges the full level and filters the source index for a lone run",
			rows: []stats.Stat{
				stat("logs/small-a", 0, 10, 30, 100), stat("logs/small-b", 0, 10, 30, 100),
				stat("logs/large", 0, 20, 50, 20<<30),
			},
			wantMerges:      [][]string{{"logs/small-a", "logs/small-b"}},
			wantFiltered:    []string{"logs/large"},
			wantFilterRange: [2]int64{20, 50},
		},
		{
			name: "filters a lone object once and spans all its sections",
			rows: []stats.Stat{
				stat("logs/small-a", 0, 10, 30, 100), stat("logs/small-b", 0, 10, 30, 100),
				stat("logs/large", 0, 20, 30, 10<<30), stat("logs/large", 1, 25, 60, 10<<30),
			},
			wantMerges:      [][]string{{"logs/small-a", "logs/small-b"}},
			wantFiltered:    []string{"logs/large"},
			wantFilterRange: [2]int64{20, 60},
		},
		{
			name: "keeps the unmerged runs of every level in one index filter",
			rows: []stats.Stat{
				stat("logs/small-a", 0, 10, 30, 100), stat("logs/small-b", 0, 10, 30, 100),
				stat("logs/small-c", 0, 5, 15, 100), stat("logs/large", 0, 40, 70, 20<<30),
			},
			wantMerges:      [][]string{{"logs/small-a", "logs/small-b"}},
			wantFiltered:    []string{"logs/large", "logs/small-c"},
			wantFilterRange: [2]int64{5, 70},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			bucket := objstore.NewInMemBucket()
			buildCurrentIndexWithStats(ctx, t, bucket, "acme", sourcePath, test.rows)
			runner := &fakeRunner{}
			defer runner.assertUniqueObjects(t)
			replacer := &fakeReplacer{swapped: true}
			c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

			got, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{Path: sourcePath})
			require.NoError(t, err)

			var merges [][]string
			var filtered []string
			entryRanges := make(map[string][2]int64)
			for _, call := range runner.snapshot() {
				switch call.opts.Actor[len(call.opts.Actor)-1] {
				case "log-merge":
					merges = append(merges, planObjectPaths(t, call.plan))
				case "index-filter":
					root, err := call.plan.Root()
					require.NoError(t, err)
					require.Equal(t, sourcePath, root.(*physical.IndexFilter).SourceIndexPath)
					filtered = planObjectPaths(t, call.plan)
				}
			}
			require.ElementsMatch(t, test.wantMerges, merges)
			require.Equal(t, test.wantFiltered, filtered)

			calls := runner.snapshot()
			if len(calls) == 0 {
				require.Equal(t, compactionStats{}, got)
				require.Empty(t, replacer.snapshot())
				return
			}
			require.Equal(t, compactionStats{removed: 1, added: len(calls), dispatched: len(calls)}, got)
			swaps := replacer.snapshot()
			require.Len(t, swaps, 1)
			for _, entry := range swaps[0].newEntries {
				entryRanges[entry.Path] = [2]int64{entry.StartTime.UnixNano(), entry.EndTime.UnixNano()}
			}
			callPaths := make([]string, len(calls))
			for i, call := range calls {
				callPaths[i] = call.path
			}
			require.ElementsMatch(t, callPaths, slices.Collect(maps.Keys(entryRanges)))
			if test.wantFiltered != nil {
				require.Equal(t, test.wantFilterRange, entryRanges[callByActor(t, calls, "index-filter").path])
			}
		})
	}
}

func TestCompactionPublicationRequiresCompleteResults(t *testing.T) {
	window := imWindow()
	ctx := context.Background()
	taskErr := errors.New("task failed")
	failActor := func(actor string) func(workflow.Options, *physical.Plan) bool {
		return func(opts workflow.Options, _ *physical.Plan) bool {
			return slices.Equal(opts.Actor, []string{"compaction", actor})
		}
	}
	// failObject must not call require, because the runner calls it from
	// dispatcher goroutines.
	failObject := func(path string) func(workflow.Options, *physical.Plan) bool {
		return func(_ workflow.Options, plan *physical.Plan) bool {
			root, err := plan.Root()
			if err != nil {
				return false
			}
			switch n := root.(type) {
			case *physical.SortObject:
				return n.SourceObjectPath == path
			case *physical.IndexMerge:
				for _, run := range n.Runs {
					for _, section := range run.Sections {
						if section.ObjectPath == path {
							return true
						}
					}
				}
			}
			return false
		}
	}
	logIndex := func(t *testing.T, schema string, labels map[string]string) (objstore.Bucket, func(*coordinator) (compactionStats, error)) {
		bucket := objstore.NewInMemBucket()
		rows := make([]stats.Stat, 3)
		for i := range rows {
			rows[i] = stats.Stat{ObjectPath: fmt.Sprintf("logs/%d", i), SortSchema: schema, Labels: labels,
				MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100}
		}
		buildCurrentIndexWithStats(ctx, t, bucket, "acme", "indexes/source", rows)
		return bucket, func(c *coordinator) (compactionStats, error) {
			return c.compactTenantLogs(ctx, "acme", window, indexEntry{Path: "indexes/source"})
		}
	}

	indexMerge := func(t *testing.T) (objstore.Bucket, func(*coordinator) (compactionStats, error)) {
		bucket := overlappingIndexesBucket(ctx, t, window, "acme", "indexes/a", "indexes/b", "indexes/c")
		indexes, err := loadTenantIndexes(ctx, bucket, window, "acme")
		require.NoError(t, err)
		return bucket, func(c *coordinator) (compactionStats, error) {
			return c.compactTenantIndexes(ctx, "acme", window, indexes)
		}
	}
	equalRuns := func(t *testing.T) (objstore.Bucket, func(*coordinator) (compactionStats, error)) {
		return logIndex(t, "label:service_name", map[string]string{"service_name": "auth"})
	}
	wrongSortSchema := func(t *testing.T) (objstore.Bucket, func(*coordinator) (compactionStats, error)) {
		return logIndex(t, "label:cluster", map[string]string{"cluster": "dev"})
	}

	tests := []struct {
		name       string
		seed       func(t *testing.T) (objstore.Bucket, func(*coordinator) (compactionStats, error))
		fail       func(workflow.Options, *physical.Plan) bool
		wantActors []string
	}{
		{name: "index merge publishes nothing when one task fails", seed: indexMerge, fail: failObject("indexes/c"),
			wantActors: []string{"index-merge", "index-merge"}},
		{name: "log compaction publishes nothing when the index filter fails", seed: equalRuns, fail: failActor("index-filter"),
			wantActors: []string{"log-merge", "index-filter"}},
		{name: "log compaction publishes nothing when a merge fails in the same batch as the index filter", seed: equalRuns, fail: failActor("log-merge"),
			wantActors: []string{"log-merge", "index-filter"}},
		{name: "sort object publishes nothing when one task fails", seed: wrongSortSchema, fail: failObject("logs/2"),
			wantActors: []string{"sort-object", "sort-object", "sort-object"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bucket, run := test.seed(t)
			var failed atomic.Bool
			runner := &fakeRunner{}
			runner.respond = func(_ context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
				if test.fail(opts, plan) {
					failed.Store(true)
					return nil, taskErr
				}
				return &v2.ResultArtifact{Path: "indexes/out"}, nil
			}
			replacer := &fakeReplacer{swapped: true}
			c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window), newFakeLimits("acme"))

			got, err := run(c)
			require.True(t, failed.Load(), "fail predicate matched no dispatched task")
			var actors []string
			for _, call := range runner.snapshot() {
				actors = append(actors, call.opts.Actor[len(call.opts.Actor)-1])
			}
			require.ElementsMatch(t, test.wantActors, actors)
			require.ErrorIs(t, err, taskErr)
			require.Equal(t, compactionStats{}, got)
			require.Empty(t, replacer.snapshot())
		})
	}
}

func TestReplaceLogIndex(t *testing.T) {
	t.Run("fail when there are no replacement entries", func(t *testing.T) {
		replacer := &fakeReplacer{swapped: true}
		c := newTestCoordinator(t, objstore.NewInMemBucket(), &fakeRunner{}, replacer, time.Now, nil)
		_, err := c.replaceLogIndex(context.Background(), "acme", time.Now(), indexEntry{Path: "indexes/source"}, nil)
		require.Error(t, err)
		require.Empty(t, replacer.snapshot())
	})
}

func countPlannedObjects(t *testing.T, calls []runCall) int {
	t.Helper()
	objects := map[string]struct{}{}
	for _, call := range calls {
		for _, path := range planObjectPaths(t, call.plan) {
			objects[path] = struct{}{}
		}
	}
	return len(objects)
}

func TestCompactTenantLogs_DeterministicOutputPaths(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}

	run := func() []metastore.TableOfContentsEntry {
		bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)
		runner := &fakeRunner{}
		defer runner.assertUniqueObjects(t)
		replacer := &fakeReplacer{swapped: true}
		c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))
		_, err := c.compactTenantLogs(ctx, "acme", window, entry)
		require.NoError(t, err)
		calls := replacer.snapshot()
		require.Len(t, calls, 1)
		return calls[0].newEntries
	}

	first := run()
	second := run()

	require.Equal(t, len(first), len(second))
	for i := range first {
		require.Equal(t, first[i].Path, second[i].Path, "output index paths must be deterministic across cycles")
	}
}

func TestPhaseFlip(t *testing.T) {
	require.Equal(t, phaseLogMerge, phaseIndexMerge.flip())
	require.Equal(t, phaseIndexMerge, phaseLogMerge.flip())
}

func imWindow() time.Time {
	return time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
}

func TestRunIndexMergePhase_SingleIndexIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/a")
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {{path: "indexes/a", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)}},
	})
	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "no swap for a single-index window")
}

func TestRunIndexMergePhase_SingleIndexWithOverlappingSectionsIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	path := "indexes/a"
	buildIndexWithPostingsSections(ctx, t, bucket, "acme", path,
		[]postings.Row{
			{Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: "service", LabelValue: "a", MinTimestamp: 10, MaxTimestamp: 20},
			{Kind: postings.KindLabel, ObjectPath: "logs/b", ColumnName: "service", LabelValue: "z", MinTimestamp: 30, MaxTimestamp: 40},
		},
		[]postings.Row{
			{Kind: postings.KindLabel, ObjectPath: "logs/c", ColumnName: "service", LabelValue: "b", MinTimestamp: 15, MaxTimestamp: 25},
			{Kind: postings.KindLabel, ObjectPath: "logs/d", ColumnName: "service", LabelValue: "y", MinTimestamp: 25, MaxTimestamp: 35},
		},
	)
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {{path: path, start: window.Add(time.Hour), end: window.Add(2 * time.Hour)}},
	})

	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(3*time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "sections from one physical object are already converged")
	require.Zero(t, testutil.ToFloat64(c.metrics.unconsolidatedBacklog.WithLabelValues("acme")))
	require.Zero(t, testutil.ToFloat64(c.metrics.oldestBacklogLogAgeSeconds.WithLabelValues("acme")))
}

func TestCompactTenant_TouchingSectionsAreConverged(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	for _, path := range []string{"indexes/a", "indexes/b"} {
		buildIndexWithPostings(ctx, t, bucket, "acme", path, 1<<20, []postings.Row{{
			Kind: postings.KindLabel, ObjectPath: path + ".log", ColumnName: "service", LabelValue: "api", MinTimestamp: 10, MaxTimestamp: 20,
		}})
	}

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	stats, err := c.compactTenantIndexes(ctx, "acme", window, []indexEntry{{Path: "indexes/a"}, {Path: "indexes/b"}})

	require.NoError(t, err)
	require.Equal(t, compactionStats{}, stats)
	require.Empty(t, runner.snapshot())
	require.Empty(t, replacer.snapshot())
	require.Zero(t, testutil.ToFloat64(c.metrics.unconsolidatedBacklog.WithLabelValues("acme")))
}

func TestCompactTenant_PostingTimestampsDetectOverlap(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	rowsByPath := map[string][]postings.Row{
		"indexes/a": {
			{Kind: postings.KindLabel, ObjectPath: "logs/a-0", ColumnName: "service", LabelValue: "api", MinTimestamp: 10, MaxTimestamp: 10},
			{Kind: postings.KindLabel, ObjectPath: "logs/a-1", ColumnName: "service", LabelValue: "api", MinTimestamp: 30, MaxTimestamp: 30},
		},
		"indexes/b": {
			{Kind: postings.KindLabel, ObjectPath: "logs/b-0", ColumnName: "service", LabelValue: "api", MinTimestamp: 20, MaxTimestamp: 20},
			{Kind: postings.KindLabel, ObjectPath: "logs/b-1", ColumnName: "service", LabelValue: "api", MinTimestamp: 40, MaxTimestamp: 40},
		},
	}
	for path, rows := range rowsByPath {
		buildIndexWithPostings(ctx, t, bucket, "acme", path, 1<<20, rows)
	}

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	_, err := c.compactTenantIndexes(ctx, "acme", window, []indexEntry{
		{Path: "indexes/a", Start: window, End: window.Add(time.Hour)},
		{Path: "indexes/b", Start: window, End: window.Add(time.Hour)},
	})

	require.NoError(t, err)
	require.Len(t, runner.snapshot(), 1)
	require.Len(t, replacer.snapshot(), 1)
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.unconsolidatedBacklog.WithLabelValues("acme")))
}

func TestCompactTenant_FailsOnIncompleteDiscovery(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/a")

	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	_, err := c.compactTenantIndexes(ctx, "acme", window, []indexEntry{{Path: "indexes/a"}, {Path: "indexes/missing"}})

	require.ErrorContains(t, err, "discover index section bounds")
	require.Empty(t, runner.snapshot())
	require.Empty(t, replacer.snapshot())
}

func TestRunIndexMergePhase_MissingToCIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket() // no ToC written
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
}

func TestRunIndexMergePhase_MultiIndexSwaps(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/a")
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/b")
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/a", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/b", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
		},
	})
	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeSwapped, c.runIndexMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 1)
}

func logMergeBucket(ctx context.Context, t *testing.T, window time.Time, tenant string, paths []string) objstore.Bucket {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	entries := make([]testIndex, 0, len(paths))
	for _, p := range paths {
		buildIndex(ctx, t, bucket, testIndexObject{
			tenant:      tenant,
			path:        p,
			sectionSize: 1 << 20,
			stats: []stats.Stat{
				{ObjectPath: p + ".log-0", SectionIndex: 0, SortSchema: "label:service_name",
					Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
				{ObjectPath: p + ".log-1", SectionIndex: 0, SortSchema: "label:service_name",
					Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
			},
			postings: []postings.Row{
				{Kind: postings.KindLabel, ObjectPath: p + ".log-0", ColumnName: "service_name", LabelValue: "a", MinTimestamp: 10, MaxTimestamp: 20, ShardBuckets: streams.ShardFactor},
				{Kind: postings.KindLabel, ObjectPath: p + ".log-1", ColumnName: "service_name", LabelValue: "z", MinTimestamp: 30, MaxTimestamp: 40, ShardBuckets: streams.ShardFactor},
			},
		})
		entries = append(entries, testIndex{path: p, start: window.Add(time.Hour), end: window.Add(2 * time.Hour)})
	}
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{tenant: entries})
	return bucket
}

func TestRunLogMergePhase_ZeroEntriesIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{}) // no acme entries
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeNoWork, c.runLogMergePhase(ctx, "acme", window))
}

func TestRunLogMergePhase_PerIndexSwaps(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeSwapped, c.runLogMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 2, "one swap per index")
	require.Positive(t, testutil.ToFloat64(c.metrics.indexesAddedTotal.WithLabelValues("acme")))
	require.Positive(t, testutil.ToFloat64(c.metrics.tasksTotal.WithLabelValues("acme")))
}

// TestRunLogMergePhase_PartialIndexFailureRetries checks that a partial failure is recorded but retried so progress continues.
func TestRunLogMergePhase_PartialIndexFailureRetries(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	runner := &fakeRunner{failOnCall: 1}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeError, c.runLogMergePhase(ctx, "acme", window),
		"a mixed success+failure cycle must retry the phase rather than flip")
	require.Len(t, runner.snapshot(), 2, "both indexes are attempted")
	require.Len(t, replacer.snapshot(), 1, "the successful index still swaps")
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.tenantLogCyclesTotal.WithLabelValues("compacted", "acme")),
		"partial progress is recorded as compacted, not failed")
	require.Zero(t, testutil.ToFloat64(c.metrics.tenantLogCyclesTotal.WithLabelValues("failed", "acme")))
}

func TestRunLogMergePhase_CancelledMidIterationStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	cancel() // cancel before running: the phase must not proceed
	require.Equal(t, phaseOutcomeError, c.runLogMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "cancelled phase performs no swap")
}

func TestRun_CancelDrainsGoroutines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	window := imWindow()
	bucket := seededToC(ctx, t, window, "acme")
	runner := &fakeRunner{}
	defer runner.assertUniqueObjects(t)
	replacer := &fakeReplacer{swapped: true}

	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	started := make(chan struct{}, 1)
	runner.respond = func(ctx context.Context, _ workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never started; drain test would be vacuous")
	}

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not drain within 2 seconds; possible goroutine leak")
	}
}

func TestRun_StartsOneWorkerPerTenant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	window := imWindow()
	bucket := seededToC(ctx, t, window, "acme", "bravo")

	replacer := &fakeReplacer{swapped: true}
	runner := &fakeRunner{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme", "bravo"))
	c.indexDispatcher.limit = 1

	var mu sync.Mutex
	starts := map[string]int{}
	runner.respond = func(ctx context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
		mu.Lock()
		starts[opts.Tenant]++
		mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}

	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return starts["acme"] >= 1 && starts["bravo"] >= 1
	}, 2*time.Second, 5*time.Millisecond, "both enabled tenants must start a worker")

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	require.Equal(t, map[string]int{"acme": 1, "bravo": 1}, starts,
		"one section per tenant must start exactly one worker per tenant")
	mu.Unlock()

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func seededToC(ctx context.Context, t *testing.T, window time.Time, tenants ...string) objstore.Bucket {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	entries := map[string][]testIndex{}
	for _, tenant := range tenants {
		paths := []string{"indexes/" + tenant + "/0", "indexes/" + tenant + "/1"}
		for _, path := range paths {
			buildOverlappingPostingsIndex(ctx, t, bucket, tenant, path)
		}
		entries[tenant] = []testIndex{
			{path: paths[0], start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
			{path: paths[1], start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
		}
	}
	writeToCWithIndexes(ctx, t, bucket, entries)
	return bucket
}

// seedWindowToC writes a two-index ToC entry per tenant into bucket for the
// given window, so multiple windows can coexist in one bucket. The window is
// encoded into each path to keep paths distinct across windows.
func seedWindowToC(ctx context.Context, t *testing.T, bucket objstore.Bucket, window time.Time, tenants ...string) {
	t.Helper()
	entries := map[string][]testIndex{}
	for _, tn := range tenants {
		prefix := "[REDACTED]" + window.Format("20060102T150405Z") + "/" + tn
		buildOverlappingPostingsIndex(ctx, t, bucket, tn, prefix+"/0")
		buildOverlappingPostingsIndex(ctx, t, bucket, tn, prefix+"/1")
		entries[tn] = []testIndex{
			{path: prefix + "/0", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
			{path: prefix + "/1", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
		}
	}
	writeToCWithIndexes(ctx, t, bucket, entries)
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDiscoverTenants(t *testing.T) {
	current := imWindow()
	prev := current.Add(-metastore.MetastoreWindowSize)
	newC := func(t *testing.T, bucket objstore.Bucket) *coordinator {
		c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(current.Add(time.Hour)), nil)
		c.cfg.WindowLookback = 1
		return c
	}

	t.Run("unions the tenants of every window", func(t *testing.T) {
		ctx := t.Context()
		bucket := objstore.NewInMemBucket()
		seedWindowToC(ctx, t, bucket, current, "acme")
		seedWindowToC(ctx, t, bucket, prev, "bravo")

		tenants, err := newC(t, bucket).discoverTenants(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"acme", "bravo"}, keys(tenants))
	})

	t.Run("skips a window with no ToCs and returns the other tenants", func(t *testing.T) {
		ctx := t.Context()
		bucket := objstore.NewInMemBucket()
		seedWindowToC(ctx, t, bucket, prev, "bravo")

		tenants, err := newC(t, bucket).discoverTenants(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"bravo"}, keys(tenants))
	})

	t.Run("ignores a shared ToC from before the per-tenant layout", func(t *testing.T) {
		ctx := t.Context()
		bucket := objstore.NewInMemBucket()
		seedWindowToC(ctx, t, bucket, prev, "bravo")
		legacyPath := strings.TrimSuffix(metastore.TableOfContentsWindowPrefix(current), "/") + ".toc"
		require.NoError(t, bucket.Upload(ctx, legacyPath, strings.NewReader("legacy")))

		tenants, err := newC(t, bucket).discoverTenants(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"bravo"}, keys(tenants))
	})

	t.Run("fails when listing ToCs fails", func(t *testing.T) {
		tenants, err := newC(t, errBucket{objstore.NewInMemBucket()}).discoverTenants(t.Context())
		require.ErrorContains(t, err, "forced list failure")
		require.Nil(t, tenants)
	})
}

func TestRunTenant(t *testing.T) {
	t.Run("deletes the tenant's metrics when the loop exits", func(t *testing.T) {
		c := newTestCoordinator(t, objstore.NewInMemBucket(), &fakeRunner{}, &fakeReplacer{}, time.Now, newFakeLimits("acme"))
		c.metrics.unconsolidatedBacklog.WithLabelValues("acme").Set(1)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		c.runTenant(ctx, "acme")

		require.Zero(t, testutil.CollectAndCount(c.metrics.unconsolidatedBacklog))
	})
}

func TestWindows_LookbackCountsBackFromCurrent(t *testing.T) {
	current := imWindow()
	newC := func(lookback int) *coordinator {
		return &coordinator{cfg: Config{WindowLookback: lookback}, clock: fixedClock(current.Add(time.Hour))}
	}

	require.Equal(t, []time.Time{
		current,
		current.Add(-metastore.MetastoreWindowSize),
		current.Add(-2 * metastore.MetastoreWindowSize),
	}, newC(2).windows(), "lookback N yields the current window plus N older ones, newest first")
}

func TestWorseOutcome(t *testing.T) {
	require.Equal(t, phaseOutcomeError, worstOutcome(phaseOutcomeNoWork, phaseOutcomeError))
	require.Equal(t, phaseOutcomeError, worstOutcome(phaseOutcomeSwapped, phaseOutcomeError))
	require.Equal(t, phaseOutcomeSwapped, worstOutcome(phaseOutcomeNoWork, phaseOutcomeSwapped))
	require.Equal(t, phaseOutcomeNoWork, worstOutcome(phaseOutcomeNoWork, phaseOutcomeNoWork))
}

// TestRunTenantLoop_ErrorRetries pins the flip-flop state machine: a failing
// phase re-arms the same phase (it must never flip), while a successful phase
// flips to the other one. The loop starts on IndexMerge.
func TestRunTenantLoop_ErrorRetries(t *testing.T) {
	window := imWindow()

	t.Run("a failing phase retries and never flips", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
		replacer := &fakeReplacer{swapped: true}
		runner := &fakeRunner{}
		c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

		var mu sync.Mutex
		var phases []string
		runner.respond = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
			mu.Lock()
			phases = append(phases, opts.Actor[1])
			mu.Unlock()
			return nil, errors.New("dispatch boom")
		}

		done := make(chan struct{})
		go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

		// Repeated failed cycles prove the loop keeps retrying the same phase
		// rather than giving up or flipping.
		require.Eventually(t, func() bool {
			return testutil.ToFloat64(c.metrics.cyclesTotal.WithLabelValues("failed")) >= 3
		}, 2*time.Second, 5*time.Millisecond, "a failing phase must retry")
		cancel()
		<-done

		mu.Lock()
		defer mu.Unlock()
		require.NotEmpty(t, phases)
		for _, p := range phases {
			require.Equal(t, "index-merge", p, "a failing phase must re-arm itself, never flip to log-merge")
		}
		require.Empty(t, replacer.snapshot(), "a failing phase never swaps the ToC")
	})

	t.Run("a successful phase flips index-merge <-> log-merge", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
		replacer := &fakeReplacer{swapped: true}
		limits := newFakeLimits("acme")
		limits.setLog("acme", true) // both phases enabled so the flip is exercised
		runner := &fakeRunner{}
		c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), limits)

		var mu sync.Mutex
		var phases []string
		// Collapse the dispatches within a cycle to a single entry so the slice
		// records the per-cycle phase order.
		runner.respond = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
			mu.Lock()
			if len(phases) == 0 || phases[len(phases)-1] != opts.Actor[1] {
				phases = append(phases, opts.Actor[1])
			}
			mu.Unlock()
			return &v2.ResultArtifact{Path: "indexes/tenants/acme/aa/x"}, nil
		}

		done := make(chan struct{})
		go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

		require.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(phases) >= 3
		}, 2*time.Second, 5*time.Millisecond)
		cancel()
		<-done

		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, []string{"index-merge", "log-merge", "index-merge"}, phases[:3],
			"successful phases flip between index-merge and log-merge")
	})
}

// TestNextBackoff pins the backoff policy: productive phases reset to the floor,
// while no-work and error phases apply the current wait and double it toward the
// ceiling.
func TestNextBackoff(t *testing.T) {
	const (
		minB = 1 * time.Second
		maxB = 8 * time.Second
	)

	t.Run("swapped resets to min", func(t *testing.T) {
		wait, next := nextBackoff(phaseOutcomeSwapped, 4*time.Second, minB, maxB)
		require.Equal(t, minB, wait, "a productive phase waits only the floor")
		require.Equal(t, minB, next, "a productive phase resets the carried backoff")
	})

	t.Run("no-work grows exponentially and caps at max", func(t *testing.T) {
		cur := minB
		var waits []time.Duration
		for range 5 {
			var w time.Duration
			w, cur = nextBackoff(phaseOutcomeNoWork, cur, minB, maxB)
			waits = append(waits, w)
		}
		require.Equal(t, []time.Duration{
			1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second,
		}, waits, "consecutive no-work waits double until capped at max")
	})

	t.Run("error grows like no-work", func(t *testing.T) {
		wait, next := nextBackoff(phaseOutcomeError, 2*time.Second, minB, maxB)
		require.Equal(t, 2*time.Second, wait)
		require.Equal(t, 4*time.Second, next)
	})
}

// TestRunTenantLoop_BacksOffWhenIdle proves the loop applies an exponentially
// growing wait to a tenant with nothing to do (empty ToC), so a converged or
// empty tenant stops hammering object storage. The injected sleep records the
// waits without blocking and cancels the loop once enough are captured.
func TestRunTenantLoop_BacksOffWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	window := imWindow()
	bucket := objstore.NewInMemBucket() // no ToC: every phase is no-work
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	c.cfg.MinBackoff = 1 * time.Second
	c.cfg.MaxBackoff = 8 * time.Second

	const want = 5
	var mu sync.Mutex
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) {
		mu.Lock()
		waits = append(waits, d)
		enough := len(waits) >= want
		mu.Unlock()
		if enough {
			cancel()
		}
	}

	done := make(chan struct{})
	go func() { c.runTenantLoop(ctx, "acme"); close(done) }()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(waits), want)
	require.Equal(t, []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second,
	}, waits[:want], "an idle tenant backs off exponentially up to the max")
}

// TestCompactTenantLogs_UnknownConvergedRowKeepsReplacementsUnknown guards the
// upgrade path: an index already in storage has a legacy ToC row (unknown, 0)
// but positive internal section stats from the old line-only statsCalculation.
// summing those undercounts would yield a positive total that looks
// exact, laundering the unknown into a falsely-known value. The replacement
// rows must stay 0 so we can still identify and backfill these indexes later.
func TestCompactTenantLogs_UnknownConvergedRowKeepsReplacementsUnknown(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	// Internal section stats are positive (100 + 100), so the size computation would
	// otherwise publish 200.
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	// Converged ToC row is unknown (0), i.e. a legacy pre-upgrade index.
	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)
	require.NoError(t, err)

	calls := replacer.snapshot()
	require.Len(t, calls, 1)
	require.NotEmpty(t, calls[0].newEntries)
}

func TestCompactTenantLogs_PublishesGlobalTimeRange(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	indexPath := "indexes/converged"
	buildCurrentIndexWithStats(ctx, t, bucket, "acme", indexPath, []stats.Stat{
		{ObjectPath: "logs/a", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 500, MaxTimestamp: 1000, UncompressedSize: 100},
		{ObjectPath: "logs/a", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "billing"}, MinTimestamp: 100, MaxTimestamp: 900, UncompressedSize: 100},
		{ObjectPath: "logs/b", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 550, MaxTimestamp: 950, UncompressedSize: 100},
		{ObjectPath: "logs/b", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "billing"}, MinTimestamp: 200, MaxTimestamp: 800, UncompressedSize: 100},
	})

	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	_, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{Path: indexPath})
	require.NoError(t, err)

	calls := replacer.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, time.Unix(0, 100).UTC(), calls[0].newEntries[0].StartTime)
	require.Equal(t, time.Unix(0, 1000).UTC(), calls[0].newEntries[0].EndTime)
}

func TestRunsToCEntry(t *testing.T) {
	section := func(minTS, maxTS int64) *compactionv2pb.SectionRef {
		return &compactionv2pb.SectionRef{MinTimestamp: minTS, MaxTimestamp: maxTS}
	}

	tests := []struct {
		name               string
		runs               []*compactionv2pb.RunRef
		wantStart, wantEnd int64
	}{
		{
			name: "spans the earliest start to the latest end across runs and sections",
			runs: []*compactionv2pb.RunRef{
				{Sections: []*compactionv2pb.SectionRef{section(40, 40), section(10, 25)}},
				{Sections: []*compactionv2pb.SectionRef{section(25, 50)}},
			},
			wantStart: 10, wantEnd: 50,
		},
		{
			name:      "uses the bounds of the only section of a single run",
			runs:      []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{section(20, 30)}}},
			wantStart: 20, wantEnd: 30,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, metastore.TableOfContentsEntry{
				StartTime: time.Unix(0, test.wantStart).UTC(),
				EndTime:   time.Unix(0, test.wantEnd).UTC(),
			}, runsToCEntry(test.runs))
		})
	}
}

func TestIndexTaskBounds(t *testing.T) {
	window := imWindow()
	inputsByPath := map[string]indexEntry{
		"indexes/a": {Path: "indexes/a", Start: window.Add(time.Hour), End: window.Add(3 * time.Hour)},
		"indexes/b": {Path: "indexes/b", Start: window.Add(2 * time.Hour), End: window.Add(4 * time.Hour)},
	}

	t.Run("spans the earliest start and latest end of the merged source indexes", func(t *testing.T) {
		task := &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{
				{ObjectPath: "indexes/a", SectionIndex: 1},
				{ObjectPath: "indexes/a", SectionIndex: 2},
			}},
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "indexes/b", SectionIndex: 0}}},
		}}

		start, end, err := indexTaskBounds(task, inputsByPath)
		require.NoError(t, err)
		require.Equal(t, window.Add(time.Hour), start)
		require.Equal(t, window.Add(4*time.Hour), end)
	})

	t.Run("fails when the task references an index missing from the inputs", func(t *testing.T) {
		task := &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "indexes/missing"}}},
		}}

		_, _, err := indexTaskBounds(task, inputsByPath)
		require.ErrorContains(t, err, `unknown index "indexes/missing"`)
	})

	t.Run("fails when the task has no sections", func(t *testing.T) {
		_, _, err := indexTaskBounds(&compactionv2pb.TaskSpec{}, inputsByPath)
		require.ErrorContains(t, err, "no source indexes")
	})
}

// TestRunTenantLoop_IndexOnly verifies that a tenant with only index
// compaction enabled runs IndexMerge cycles and never dispatches a LogMerge
// task, because runTenantLoop skips the LogMerge phase when runLog is false.
func TestRunTenantLoop_IndexOnly(t *testing.T) {
	window := imWindow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bucket := logMergeBucket(ctx, t, window, "acme", []string{"a", "b"})
	replacer := &fakeReplacer{swapped: true}
	runner := &fakeRunner{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	var mu sync.Mutex
	var phases []string
	runner.respond = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
		mu.Lock()
		phases = append(phases, opts.Actor[1])
		mu.Unlock()
		return &v2.ResultArtifact{Path: "indexes/aa/bb"}, nil
	}

	done := make(chan struct{})
	go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(phases) >= 3
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, phases)
	for _, p := range phases {
		require.Equal(t, "index-merge", p, "index-only tenant must never dispatch a log-merge task")
	}
}

// TestRunTenantLoop_LogEnabledRunsBothPhases verifies that enabling log
// compaction (which implies index) restores the flip-flop: dispatches
// alternate index-merge <-> log-merge.
func TestRunTenantLoop_LogEnabledRunsBothPhases(t *testing.T) {
	window := imWindow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bucket := logMergeBucket(ctx, t, window, "acme", []string{"a", "b"})
	replacer := &fakeReplacer{swapped: true}
	limits := newFakeLimits("acme")
	limits.setLog("acme", true)
	runner := &fakeRunner{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), limits)

	var mu sync.Mutex
	var phases []string
	runner.respond = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
		mu.Lock()
		if len(phases) == 0 || phases[len(phases)-1] != opts.Actor[1] {
			phases = append(phases, opts.Actor[1])
		}
		mu.Unlock()
		return &v2.ResultArtifact{Path: "indexes/aa/bb"}, nil
	}

	done := make(chan struct{})
	go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(phases) >= 3
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	// runPlan reports a swapped artifact (success) on every call, so the loop
	// flips every cycle and the phase order is deterministic; the exact prefix
	// is safe to assert. The loop starts on IndexMerge.
	require.Equal(t, []string{"index-merge", "log-merge", "index-merge"}, phases[:3],
		"log-enabled tenant flips between index-merge and log-merge")
}

func TestRunTenantLoop_RunsMultipleIndexMergesPerLogMerge(t *testing.T) {
	window := imWindow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bucket := logMergeBucket(ctx, t, window, "acme", []string{"a", "b"})
	replacer := &fakeReplacer{swapped: true}
	limits := newFakeLimits("acme")
	limits.setLog("acme", true)
	runner := &fakeRunner{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), limits)

	var mu sync.Mutex
	var phases []string
	runner.respond = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
		mu.Lock()
		phases = append(phases, opts.Actor[1])
		if opts.Actor[1] == "log-merge" {
			cancel()
		}
		mu.Unlock()
		return &v2.ResultArtifact{Path: "indexes/aa/bb"}, nil
	}

	done := make(chan struct{})
	go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("tenant loop did not reach log merge")
	}

	mu.Lock()
	defer mu.Unlock()
	expectation := []string{}
	for range indexMergeIterations {
		expectation = append(expectation, "index-merge")
	}
	expectation = append(expectation, "log-merge")

	// expect: index-merge, index-merge, index-merge, log-merge
	require.Equal(t, expectation, phases)
}

// disableObservingLimits wraps a Limits to pin down exactly when runTenantLoop
// saw log compaction get disabled. It captures count() the first time
// CompactionPhases reports runLog=false, from inside that call, before
// runTenantLoop acts on the result. That gives a boundary tied to the loop's
// own observation of the disable, not to when the test called setLog — which
// races with the loop and can be one or more iterations behind.
type disableObservingLimits struct {
	Limits
	count func() int

	mu       sync.Mutex
	observed int
}

func newDisableObservingLimits(limits Limits, count func() int) *disableObservingLimits {
	return &disableObservingLimits{Limits: limits, count: count, observed: -1}
}

func (d *disableObservingLimits) CompactionPhases(userID string) (runIndex, runLog bool) {
	runIndex, runLog = d.Limits.CompactionPhases(userID)
	if !runLog {
		d.mu.Lock()
		if d.observed < 0 {
			d.observed = d.count()
		}
		d.mu.Unlock()
	}
	return runIndex, runLog
}

func (d *disableObservingLimits) observedAt() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.observed
}

// TestRunTenantLoop_DisablingLogMidRunStopsLogMerge verifies that turning off
// log compaction while the loop runs stops further log-merge dispatches on the
// next iteration while index-merge continues, because runTenantLoop re-reads
// CompactionPhases every cycle.
func TestRunTenantLoop_DisablingLogMidRunStopsLogMerge(t *testing.T) {
	window := imWindow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bucket := logMergeBucket(ctx, t, window, "acme", []string{"a", "b"})
	replacer := &fakeReplacer{swapped: true}
	limits := newFakeLimits("acme")
	limits.setLog("acme", true)
	runner := &fakeRunner{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), limits)

	var mu sync.Mutex
	var phases []string
	sawLog := make(chan struct{})
	var closeOnce sync.Once
	runner.respond = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
		mu.Lock()
		phases = append(phases, opts.Actor[1])
		mu.Unlock()
		if opts.Actor[1] == "log-merge" {
			closeOnce.Do(func() { close(sawLog) })
		}
		return &v2.ResultArtifact{Path: "indexes/aa/bb"}, nil
	}

	observingLimits := newDisableObservingLimits(limits, func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(phases)
	})
	c.limits = observingLimits

	done := make(chan struct{})
	go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

	// Wait for at least one log-merge, then disable log compaction.
	select {
	case <-sawLog:
	case <-time.After(2 * time.Second):
		t.Fatal("expected at least one log-merge dispatch before disabling")
	}
	limits.setLog("acme", false)

	// Wait for runTenantLoop to observe the disable and keep dispatching
	// index-merge afterwards, proving the loop doesn't just stall.
	require.Eventually(t, func() bool {
		boundary := observingLimits.observedAt()
		if boundary < 0 {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		return len(phases)-boundary >= 3
	}, 2*time.Second, 5*time.Millisecond, "expected index-merge dispatches to continue after disabling")
	cancel()
	<-done

	boundary := observingLimits.observedAt()
	require.GreaterOrEqual(t, boundary, 0, "expected runTenantLoop to observe log compaction disabled")

	mu.Lock()
	defer mu.Unlock()
	tail := phases[boundary:]
	require.NotEmpty(t, tail, "expected index-merge dispatches to continue after disabling log compaction")
	for _, p := range tail {
		require.Equal(t, "index-merge", p, "no log-merge dispatch after runTenantLoop observed log compaction disabled")
	}
}

func TestAssignArtifactPaths(t *testing.T) {
	t.Run("sets each entry path from the artifact at the same index", func(t *testing.T) {
		entries := make([]metastore.TableOfContentsEntry, 2)
		artifacts := []v2.ResultArtifact{{Path: "a"}, {Path: "b"}}

		require.NoError(t, assignArtifactPaths(entries, artifacts))
		require.Equal(t, "a", entries[0].Path)
		require.Equal(t, "b", entries[1].Path)
	})

	t.Run("returns an error and leaves entries unchanged when counts differ", func(t *testing.T) {
		entries := make([]metastore.TableOfContentsEntry, 2)
		artifacts := []v2.ResultArtifact{{Path: "a"}}

		require.Error(t, assignArtifactPaths(entries, artifacts))
		require.Empty(t, entries[0].Path)
		require.Empty(t, entries[1].Path)
	})
}
