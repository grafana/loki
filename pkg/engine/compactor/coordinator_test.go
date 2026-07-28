package compactor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	stats "github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// fakeRunner records each runPlan invocation. The Coordinator dispatches
// plans through a runFunc field so unit tests can swap this in without
// standing up a real scheduler + worker pair.
type fakeRunner struct {
	mu    sync.Mutex
	calls []runCall
	err   error // returned from every call when non-nil

	// failOnCall, when > 0, makes the Nth run invocation (1-based) return an
	// error while all others succeed. Used to simulate one failed job.
	failOnCall int
}

type runCall struct {
	opts workflow.Options
	plan *physical.Plan
	path string
}

func (f *fakeRunner) run(_ context.Context, opts workflow.Options, plan *physical.Plan) (arrow.RecordBatch, error) {
	f.mu.Lock()
	n := len(f.calls) + 1
	path := fmt.Sprintf("indexes/tenants/test/aa/artifact-%02d", n)
	f.calls = append(f.calls, runCall{opts: opts, plan: plan, path: path})
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.failOnCall > 0 && n == f.failOnCall {
		return nil, errors.New("fakeRunner: forced failure on call")
	}
	return v2.BuildResultRecord(memory.DefaultAllocator, []v2.ResultArtifact{{Path: path}}), nil
}

func (f *fakeRunner) snapshot() []runCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runCall(nil), f.calls...)
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
// the transient-read-error path in discover.
type errBucket struct{ objstore.Bucket }

func (errBucket) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("errBucket: forced read failure")
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
	return &coordinator{
		cfg: Config{
			Enabled:                   true,
			PollingInterval:           5 * time.Minute,
			MaxRunsPerTask:            2,
			LogMaxRunsPerTask:         2,
			LogMinCompactionSize:      1,
			ToCConsolidateTimeout:     30 * time.Second,
			MaxRunningCompactionTasks: 4,
			PlanVersion:               1,
			Scheduler:                 SchedulerConfig{Endpoint: defaultEndpoint},
		},
		logger:          log.NewNopLogger(),
		bucket:          bucket,
		runPlan:         runner.run,
		metastoreWriter: replacer,
		clock:           clock,
		sleep:           sleepUntil,
		metrics:         newCoordinatorMetrics(prometheus.NewRegistry()),
		limits:          limits,
	}
}

// fixedClock returns a clock function pinned to t.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func buildOverlappingPostingsIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string) {
	t.Helper()
	buildIndexWithPostings(ctx, t, bucket, tenant, path, 1<<20, []postings.Row{
		{Kind: postings.KindLabel, ObjectPath: path + ".log-0", ColumnName: "service_name", LabelValue: "a", MinTimestamp: 10, MaxTimestamp: 20},
		{Kind: postings.KindLabel, ObjectPath: path + ".log-1", ColumnName: "service_name", LabelValue: "z", MinTimestamp: 30, MaxTimestamp: 40},
	})
}

// TestRunTenantCycle_RaceLossIsSuccess verifies the (swapped=false, err=nil)
// path is treated as success: the cycle returns nil and the next cycle
// re-plans against the post-swap ToC.
func TestCompactTenant_RaceLossIsSuccess(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/aa/src-0")
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/bb/src-1")

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour)},
		},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: false, err: nil}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	// compactTenant surfaces the tenant error directly (the phase wrapper is
	// what swallows/logs it); assert on it via the lower API here.
	indexes, err := loadTenantIndexes(ctx, bucket, window)
	require.NoError(t, err)
	_, runErr := c.compactTenant(ctx, "acme", window, indexes["acme"])
	require.NoError(t, runErr)
}

// TestCompactTenant_HardSwapErrorPropagates verifies that a non-nil error
// from ReplaceIndexPointers is returned by compactTenant (wrapped), so
// callers can pin it. The phase wrapper logs and re-arms on such errors;
// compactTenant itself surfaces the wrapped error.
func TestCompactTenant_HardSwapErrorPropagates(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/aa/src-0")
	buildOverlappingPostingsIndex(ctx, t, bucket, "acme", "indexes/bb/src-1")

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour)},
		},
	})

	swapErr := errors.New("bucket: temporary failure")
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: false, err: swapErr}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	indexes, err := loadTenantIndexes(ctx, bucket, window)
	require.NoError(t, err)
	_, runErr := c.compactTenant(ctx, "acme", window, indexes["acme"])
	require.ErrorIs(t, runErr, swapErr)
}

func TestCompactTenantLogs_DispatchesLogMergePlans(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"

	// Same tuple, overlapping times -> 2 runs -> still dispatches after the
	// terminal gate (total size 200 clears the test floor of 1).
	buildIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
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

	swaps := replacer.snapshot()
	require.Len(t, swaps, 1, "log path now swaps the ToC after dispatch")
	require.Equal(t, []string{convergedPath}, swaps[0].oldPaths)
}

func TestCompactTenantLogs_NoStatsRowsForTenantIsConverged(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/other-tenant"

	// Index has a stats section (so it flushes) but for a DIFFERENT tenant;
	// "acme" gets zero refs -> no tasks -> converged.
	buildIndexWithStats(ctx, t, bucket, "other", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)
	require.NoError(t, err)
	require.Empty(t, runner.snapshot())
}

func TestCompactTenantLogs_TerminalSingleRunSkips(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"

	// Single stat row -> P=1 -> terminal regardless of size.
	buildIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.Zero(t, stats.added)
	require.Empty(t, runner.snapshot(), "terminal window dispatches no plans")
	require.Empty(t, replacer.snapshot(), "terminal window performs no swap")
}

func TestCompactTenantLogs_TouchingRunsAreConverged(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	indexPath := "indexes/aa/converged"

	buildIndexWithStats(ctx, t, bucket, "acme", indexPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, UncompressedSize: 100},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 30, UncompressedSize: 100},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	stats, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{Path: indexPath})
	require.NoError(t, err)
	require.Equal(t, compactionStats{}, stats)
	require.Empty(t, runner.snapshot())
	require.Empty(t, replacer.snapshot())
}

func TestCompactTenantLogs_TerminalBelowFloorSkips(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"

	// Two overlapping same-tuple rows -> P=2, total size 30, below the 1GiB floor.
	buildIndexWithStats(ctx, t, bucket, "acme", convergedPath, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 10},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 20},
	})

	runner := &fakeRunner{}
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
	buildIndexWithStats(ctx, t, bucket, tenant, path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
	})
	return bucket
}

func TestCompactTenantLogs_SwapsToC(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.NotZero(t, stats.added)
	require.NotEmpty(t, runner.snapshot(), "non-terminal window dispatches plans")

	calls := replacer.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, []string{convergedPath}, calls[0].oldPaths)
	require.NotEmpty(t, calls[0].newEntries)
}

func TestCompactTenantLogs_SwapErrorFails(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{err: errors.New("boom")}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.Error(t, err)
}

func TestCompactTenantLogs_SwapRaceLossConverged(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: false}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.Zero(t, stats.added)
}

func TestCompactTenantLogs_DryRunSkipsSwap(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))
	c.cfg.DryRun = true

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.Zero(t, stats.added)
	require.NotEmpty(t, runner.snapshot(), "dry-run still dispatches")
	require.Empty(t, replacer.snapshot(), "dry-run must not swap the ToC")
}

func TestCompactTenantLogs_PartialFailureNoSwap(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{failOnCall: 1}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.Error(t, err)
	require.Empty(t, replacer.snapshot(), "partial failure must not swap the ToC")
}

func TestCompactTenantLogs_DeterministicOutputPaths(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}

	run := func() []metastore.TableOfContentsEntry {
		bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)
		runner := &fakeRunner{}
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
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "no swap for a single-index window")
}

func TestRunIndexMergePhase_SingleIndexWithOverlappingSectionsSwaps(t *testing.T) {
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

	require.Equal(t, phaseOutcomeSwapped, c.runIndexMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 1)
	require.Equal(t, 1.0, testutil.ToFloat64(c.metrics.unconsolidatedBacklog.WithLabelValues("acme")))
	require.Equal(t, time.Hour.Seconds(), testutil.ToFloat64(c.metrics.oldestBacklogLogAgeSeconds.WithLabelValues("acme")))
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
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	stats, err := c.compactTenant(ctx, "acme", window, []indexEntry{{Path: "indexes/a"}, {Path: "indexes/b"}})

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
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	_, err := c.compactTenant(ctx, "acme", window, []indexEntry{
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
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	_, err := c.compactTenant(ctx, "acme", window, []indexEntry{{Path: "indexes/a"}, {Path: "indexes/missing"}})

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
				{Kind: postings.KindLabel, ObjectPath: p + ".log-0", ColumnName: "service_name", LabelValue: "a", MinTimestamp: 10, MaxTimestamp: 20},
				{Kind: postings.KindLabel, ObjectPath: p + ".log-1", ColumnName: "service_name", LabelValue: "z", MinTimestamp: 30, MaxTimestamp: 40},
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
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	require.Equal(t, phaseOutcomeSwapped, c.runLogMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 2, "one swap per index")
	require.Positive(t, testutil.ToFloat64(c.metrics.indexesAddedTotal.WithLabelValues("acme")))
	require.Positive(t, testutil.ToFloat64(c.metrics.tasksTotal.WithLabelValues("acme")))
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
	replacer := &fakeReplacer{swapped: true}

	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	started := make(chan struct{}, 1)
	c.runPlan = func(ctx context.Context, _ workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
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
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme", "bravo"))
	c.cfg.MaxRunningCompactionTasks = 1

	var mu sync.Mutex
	starts := map[string]int{}
	c.runPlan = func(ctx context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
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

// reconcileHarness parks every dispatched plan on ctx cancellation so a started
// worker stays observable and a cancelled worker's goroutine actually exits.
func reconcileHarness(t *testing.T, bucket objstore.Bucket, clock func() time.Time, limits Limits) (*coordinator, func() []string) {
	t.Helper()
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{swapped: true}, clock, limits)
	c.cfg.MaxRunningCompactionTasks = 1

	var mu sync.Mutex
	live := map[string]int{}
	c.runPlan = func(ctx context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
		mu.Lock()
		live[opts.Tenant]++
		mu.Unlock()
		<-ctx.Done()
		mu.Lock()
		live[opts.Tenant]--
		mu.Unlock()
		return nil, ctx.Err()
	}
	tenantsWithLiveDispatch := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for tn, n := range live {
			if n > 0 {
				out = append(out, tn)
			}
		}
		sort.Strings(out)
		return out
	}
	return c, tenantsWithLiveDispatch
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

func TestReconcile_FiltersToEnabledTenants(t *testing.T) {
	ctx := t.Context()
	window := imWindow()
	bucket := seededToC(ctx, t, window, "acme", "bravo")

	c, liveTenants := reconcileHarness(t, bucket, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)

	require.Eventually(t, func() bool {
		return len(liveTenants()) == 1 && liveTenants()[0] == "acme"
	}, 2*time.Second, 5*time.Millisecond, "only the enabled tenant runs a worker")
	require.Contains(t, workers, "acme")
	require.NotContains(t, workers, "bravo")
}

func TestReconcile_EnabledButAbsentFromToC_NoWorker(t *testing.T) {
	ctx := t.Context()
	window := imWindow()
	bucket := seededToC(ctx, t, window, "acme") // bravo enabled but not in ToC

	c, liveTenants := reconcileHarness(t, bucket, fixedClock(window.Add(time.Hour)), newFakeLimits("acme", "bravo"))
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)

	require.Eventually(t, func() bool {
		return len(liveTenants()) == 1 && liveTenants()[0] == "acme"
	}, 2*time.Second, 5*time.Millisecond)
	require.NotContains(t, workers, "bravo", "an enabled tenant not in the ToC gets no worker")
}

func TestReconcile_RemovedFromToC_CancelsWorker(t *testing.T) {
	ctx := t.Context()
	window := imWindow()

	limits := newFakeLimits("acme", "bravo")
	bucketBoth := seededToC(ctx, t, window, "acme", "bravo")
	c, liveTenants := reconcileHarness(t, bucketBoth, fixedClock(window.Add(time.Hour)), limits)
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)
	require.Eventually(t, func() bool { return len(liveTenants()) == 2 }, 2*time.Second, 5*time.Millisecond)

	// Point the coordinator at a ToC that no longer lists bravo.
	c.bucket = seededToC(ctx, t, window, "acme")
	c.reconcile(ctx, workers, &wg)

	require.Eventually(t, func() bool {
		return len(liveTenants()) == 1 && liveTenants()[0] == "acme"
	}, 2*time.Second, 5*time.Millisecond, "tenant removed from ToC has its worker cancelled")
	require.NotContains(t, workers, "bravo")
}

func TestReconcile_DisabledDuringToCReadFailure_DefersUntilNextSuccess(t *testing.T) {
	ctx := t.Context()
	window := imWindow()

	limits := newFakeLimits("acme")
	bucket := seededToC(ctx, t, window, "acme")
	c, liveTenants := reconcileHarness(t, bucket, fixedClock(window.Add(time.Hour)), limits)
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)
	require.Eventually(t, func() bool { return len(liveTenants()) == 1 }, 2*time.Second, 5*time.Millisecond)

	// Disable the tenant AND make the ToC read fail. A read failure is not
	// authoritative, so the worker persists rather than being torn down on a
	// possibly-transient error.
	limits.setIndex("acme", false)
	c.bucket = errBucket{objstore.NewInMemBucket()} // Get returns a non-not-found error
	c.reconcile(ctx, workers, &wg)

	time.Sleep(50 * time.Millisecond)
	require.Equal(t, []string{"acme"}, liveTenants(),
		"disable is deferred while the ToC read fails; the worker keeps backing off")
	require.Contains(t, workers, "acme")

	// Restore a readable ToC: the next reconcile is authoritative and applies
	// the deferred disable.
	c.bucket = bucket
	c.reconcile(ctx, workers, &wg)

	require.Eventually(t, func() bool { return len(liveTenants()) == 0 }, 2*time.Second, 5*time.Millisecond,
		"disable takes effect on the next successful ToC read")
	require.NotContains(t, workers, "acme")

	// Once the worker goroutine has fully exited, its deferred cleanup must have
	// dropped the tenant's series.
	wg.Wait()
	require.Equal(t, 0, testutil.CollectAndCount(c.metrics.unconsolidatedBacklog),
		"a cancelled worker's per-tenant series is deleted on exit")
}

func TestReconcile_ReadError_PreservesWorkers(t *testing.T) {
	ctx := t.Context()
	window := imWindow()

	limits := newFakeLimits("acme")
	bucket := seededToC(ctx, t, window, "acme")
	c, liveTenants := reconcileHarness(t, bucket, fixedClock(window.Add(time.Hour)), limits)
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)
	require.Eventually(t, func() bool { return len(liveTenants()) == 1 }, 2*time.Second, 5*time.Millisecond)

	// Transient read failure with the tenant still enabled: worker must persist.
	c.bucket = errBucket{objstore.NewInMemBucket()}
	c.reconcile(ctx, workers, &wg)

	time.Sleep(50 * time.Millisecond)
	require.Equal(t, []string{"acme"}, liveTenants(), "read error must not tear down running workers")
	require.Contains(t, workers, "acme")
}

func TestReconcile_Idempotent_NoDuplicateWorkers(t *testing.T) {
	ctx := t.Context()
	window := imWindow()
	bucket := seededToC(ctx, t, window, "acme")

	c, liveTenants := reconcileHarness(t, bucket, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)
	require.Eventually(t, func() bool { return len(liveTenants()) == 1 }, 2*time.Second, 5*time.Millisecond)

	first := workers["acme"]
	c.reconcile(ctx, workers, &wg) // second tick, same ToC

	time.Sleep(50 * time.Millisecond)
	require.Len(t, workers, 1, "a second reconcile must not add a duplicate worker")
	require.Equal(t, []string{"acme"}, liveTenants())
	require.Equal(t, reflect.ValueOf(first).Pointer(), reflect.ValueOf(workers["acme"]).Pointer(),
		"the existing worker must be left in place, not replaced")
}

func TestReconcile_StartAndCancelSameTick(t *testing.T) {
	ctx := t.Context()
	window := imWindow()

	limits := newFakeLimits("acme", "bravo")
	c, liveTenants := reconcileHarness(t, seededToC(ctx, t, window, "acme"), fixedClock(window.Add(time.Hour)), limits)
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	defer func() {
		for _, cf := range workers {
			cf()
		}
		wg.Wait()
	}()

	c.reconcile(ctx, workers, &wg)
	require.Eventually(t, func() bool {
		return len(liveTenants()) == 1 && liveTenants()[0] == "acme"
	}, 2*time.Second, 5*time.Millisecond)

	// Next tick: ToC now lists bravo only. acme is removed and bravo started in
	// the same reconcile.
	c.bucket = seededToC(ctx, t, window, "bravo")
	c.reconcile(ctx, workers, &wg)

	require.Eventually(t, func() bool {
		return len(liveTenants()) == 1 && liveTenants()[0] == "bravo"
	}, 2*time.Second, 5*time.Millisecond, "one start and one cancel in a single tick")
	require.Contains(t, workers, "bravo")
	require.NotContains(t, workers, "acme")
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
		c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

		var mu sync.Mutex
		var phases []string
		c.runPlan = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
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
		c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), limits)

		var mu sync.Mutex
		var phases []string
		// Collapse the dispatches within a cycle to a single entry so the slice
		// records the per-cycle phase order.
		c.runPlan = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
			mu.Lock()
			if len(phases) == 0 || phases[len(phases)-1] != opts.Actor[1] {
				phases = append(phases, opts.Actor[1])
			}
			mu.Unlock()
			return v2.BuildResultRecord(memory.DefaultAllocator, []v2.ResultArtifact{{Path: "indexes/tenants/acme/aa/x"}}), nil
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
	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour), UncompressedLogsSize: 0}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)
	require.NoError(t, err)

	calls := replacer.snapshot()
	require.Len(t, calls, 1)
	require.NotEmpty(t, calls[0].newEntries)
	for _, e := range calls[0].newEntries {
		require.Equal(t, uint64(0), e.UncompressedLogsSize,
			"an unknown converged row must not be healed into a positive size from legacy line-only stats")
	}
}

// TestCompactTenantLogs_KnownConvergedRowKeepsComputedSize is the complement:
// when the converged ToC row is known (nonzero), the computed replacement size
// is trustworthy and must be persisted rather than zeroed.
func TestCompactTenantLogs_KnownConvergedRowKeepsComputedSize(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	convergedPath := "indexes/aa/converged"
	bucket := twoRunConvergedBucket(ctx, t, "acme", convergedPath)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)), newFakeLimits("acme"))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour), UncompressedLogsSize: 200}
	_, err := c.compactTenantLogs(ctx, "acme", window, entry)
	require.NoError(t, err)

	calls := replacer.snapshot()
	require.Len(t, calls, 1)
	require.Len(t, calls[0].newEntries, 1)
	require.Equal(t, uint64(200), calls[0].newEntries[0].UncompressedLogsSize,
		"a known converged row keeps the computed section sum (100+100)")
}

func TestCompactTenantLogs_PublishesGlobalTimeRange(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	indexPath := "indexes/converged"
	buildIndexWithStats(ctx, t, bucket, "acme", indexPath, []stats.Stat{
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
	_, err := c.compactTenantLogs(ctx, "acme", window, indexEntry{Path: indexPath, UncompressedLogsSize: 400})
	require.NoError(t, err)

	calls := replacer.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, time.Unix(0, 100).UTC(), calls[0].newEntries[0].StartTime)
	require.Equal(t, time.Unix(0, 1000).UTC(), calls[0].newEntries[0].EndTime)
}

func TestTaskBounds_AndUncompressedLogsSize(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	// Task 1: sections with distinct timestamps
	task1Min := window.UnixNano()
	task1Mid := window.Add(30 * time.Minute).UnixNano()
	task1Max := window.Add(2 * time.Hour).UnixNano()

	// Task 2: sections with different distinct timestamps
	task2Min := window.Add(10 * time.Minute).UnixNano()
	task2Max := window.Add(3 * time.Hour).UnixNano()

	tasks := []*compactionv2pb.TaskSpec{
		{
			Runs: []*compactionv2pb.RunRef{
				{
					Sections: []*compactionv2pb.SectionRef{
						{MinTimestamp: task1Max, MaxTimestamp: task1Max, UncompressedSize: 100},
						{MinTimestamp: task1Min, MaxTimestamp: task1Mid, UncompressedSize: 200},
					},
				},
				{
					Sections: []*compactionv2pb.SectionRef{
						{MinTimestamp: task1Mid, MaxTimestamp: task1Max, UncompressedSize: 150},
					},
				},
			},
		},
		{
			Runs: []*compactionv2pb.RunRef{
				{
					Sections: []*compactionv2pb.SectionRef{
						{MinTimestamp: task2Max, MaxTimestamp: task2Max, UncompressedSize: 300},
						{MinTimestamp: task2Min, MaxTimestamp: task2Min, UncompressedSize: 50},
					},
				},
			},
		},
	}

	min1, max1 := taskBounds(tasks[0])
	require.Equal(t, task1Min, min1, "first task StartTime = min across sections")
	require.Equal(t, task1Max, max1, "first task EndTime = max across sections")
	require.Equal(t, uint64(450), taskUncompressedLogsSize(tasks[0]), "first task sum: 100+200+150")

	min2, max2 := taskBounds(tasks[1])
	require.Equal(t, task2Min, min2, "second task StartTime = min across sections")
	require.Equal(t, task2Max, max2, "second task EndTime = max across sections")
	require.Equal(t, uint64(350), taskUncompressedLogsSize(tasks[1]), "second task sum: 300+50")
}

// TestMakeTocEntries_UnknownSizePropagates verifies that a size of 0 (which
// means "unknown", e.g. a legacy ToC row written before sizes were recorded)
// poisons the whole task's sum. Publishing a partial sum would look exact even
// though the true total is larger, so an unknown input must yield an unknown
// (zero) output.
func TestTaskUncompressedLogsSize_UnknownSizePropagates(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	minTS := window.UnixNano()
	maxTS := window.Add(time.Hour).UnixNano()

	tasks := []*compactionv2pb.TaskSpec{
		{
			Runs: []*compactionv2pb.RunRef{
				{
					Sections: []*compactionv2pb.SectionRef{
						{MinTimestamp: minTS, MaxTimestamp: maxTS, UncompressedSize: 0},    // legacy: unknown
						{MinTimestamp: minTS, MaxTimestamp: maxTS, UncompressedSize: 4096}, // known
					},
				},
			},
		},
	}

	require.Equal(t, uint64(0), taskUncompressedLogsSize(tasks[0]),
		"an unknown (0) input section must propagate as unknown, not a misleading partial sum")
}

func TestMakeIndexTocEntries_UsesInputIndexes(t *testing.T) {
	window := imWindow()
	tasks := []*compactionv2pb.TaskSpec{{
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{
				{ObjectPath: "indexes/a", SectionIndex: 1},
				{ObjectPath: "indexes/a", SectionIndex: 2},
			}},
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "indexes/b", SectionIndex: 0}}},
		},
	}}
	inputs := []indexEntry{
		{Path: "indexes/a", Start: window.Add(time.Hour), End: window.Add(3 * time.Hour), UncompressedLogsSize: 100},
		{Path: "indexes/b", Start: window.Add(2 * time.Hour), End: window.Add(4 * time.Hour), UncompressedLogsSize: 200},
	}

	entries, err := makeIndexTocEntries(tasks, []string{"indexes/output"}, inputs)
	require.NoError(t, err)
	require.Equal(t, []metastore.TableOfContentsEntry{{
		Path:                 "indexes/output",
		StartTime:            window.Add(time.Hour),
		EndTime:              window.Add(4 * time.Hour),
		UncompressedLogsSize: 300,
	}}, entries)
}

func TestMakeIndexTocEntries_UnknownInputSizePropagates(t *testing.T) {
	tasks := []*compactionv2pb.TaskSpec{{
		Runs: []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{
			{ObjectPath: "indexes/a"},
			{ObjectPath: "indexes/b"},
		}}},
	}}
	inputs := []indexEntry{
		{Path: "indexes/a", UncompressedLogsSize: 100},
		{Path: "indexes/b", UncompressedLogsSize: 0},
	}

	entries, err := makeIndexTocEntries(tasks, []string{"indexes/output"}, inputs)
	require.NoError(t, err)
	require.Zero(t, entries[0].UncompressedLogsSize)
}

func TestFillFileSizes_StatsObjectAndSetsSize(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	outputPath := "indexes/test/output"
	testData := []byte("test data for size calculation")
	err := bucket.Upload(ctx, outputPath, bytes.NewReader(testData))
	require.NoError(t, err)

	entries := []metastore.TableOfContentsEntry{
		{Path: outputPath},
	}

	c := &coordinator{
		logger: log.NewNopLogger(),
		bucket: bucket,
	}

	c.fillFileSizes(ctx, entries)

	require.Equal(t, uint64(len(testData)), entries[0].FileSize)
}

func TestFillFileSizes_MissingObjectZeroSize(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	entries := []metastore.TableOfContentsEntry{
		{Path: "indexes/test/nonexistent"},
	}

	c := &coordinator{
		logger: log.NewNopLogger(),
		bucket: bucket,
	}

	c.fillFileSizes(ctx, entries)

	require.Equal(t, uint64(0), entries[0].FileSize, "missing object should leave FileSize as zero")
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
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), newFakeLimits("acme"))

	var mu sync.Mutex
	var phases []string
	c.runPlan = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
		mu.Lock()
		phases = append(phases, opts.Actor[1])
		mu.Unlock()
		return nil, nil
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
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), limits)

	var mu sync.Mutex
	var phases []string
	c.runPlan = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
		mu.Lock()
		if len(phases) == 0 || phases[len(phases)-1] != opts.Actor[1] {
			phases = append(phases, opts.Actor[1])
		}
		mu.Unlock()
		return nil, nil
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
	// runPlan returns nil (success) on every call, so the loop flips every
	// cycle and the phase order is deterministic; the exact prefix is safe to
	// assert. The loop starts on IndexMerge.
	require.Equal(t, []string{"index-merge", "log-merge", "index-merge"}, phases[:3],
		"log-enabled tenant flips between index-merge and log-merge")
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
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)), limits)

	var mu sync.Mutex
	var phases []string
	sawLog := make(chan struct{})
	var closeOnce sync.Once
	c.runPlan = func(_ context.Context, opts workflow.Options, _ *physical.Plan) (arrow.RecordBatch, error) {
		mu.Lock()
		phases = append(phases, opts.Actor[1])
		mu.Unlock()
		if opts.Actor[1] == "log-merge" {
			closeOnce.Do(func() { close(sawLog) })
		}
		return nil, nil
	}

	done := make(chan struct{})
	go func() { c.runTenantLoop(ctx, "acme"); close(done) }()

	// Wait for at least one log-merge, then disable log compaction.
	select {
	case <-sawLog:
	case <-time.After(2 * time.Second):
		t.Fatal("expected at least one log-merge dispatch before disabling")
	}
	limits.setLog("acme", false)

	// Record how many phases exist at the cutoff, then let the loop run more.
	mu.Lock()
	cutoff := len(phases)
	mu.Unlock()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(phases) >= cutoff+4 // several more cycles after disabling
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	// The mid-run disable is not instantaneous: an in-flight log-merge cycle
	// may still complete. Assert that dispatches eventually settle to
	// index-merge only — i.e. the tail after the cutoff contains no log-merge.
	tail := phases[cutoff:]
	require.NotEmpty(t, tail)
	for _, p := range tail[len(tail)-4:] {
		require.Equal(t, "index-merge", p, "no log-merge after log compaction is disabled")
	}
}
