package compactor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
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
}

func (f *fakeRunner) run(_ context.Context, opts workflow.Options, plan *physical.Plan) error {
	f.mu.Lock()
	f.calls = append(f.calls, runCall{opts, plan})
	n := len(f.calls)
	f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if f.failOnCall > 0 && n == f.failOnCall {
		return errors.New("fakeRunner: forced failure on call")
	}
	return nil
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

// newTestCoordinator builds a Coordinator wired to the supplied fakes plus a
// default-configured Config (loop-friendly TTLs, K=2 so two indexes split
// into two single-pile tasks).
func newTestCoordinator(t *testing.T, bucket objstore.Bucket, runner *fakeRunner, replacer *fakeReplacer, clock func() time.Time) *coordinator {
	t.Helper()
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
		metrics:         newCoordinatorMetrics(prometheus.NewRegistry()),
	}
}

// fixedClock returns a clock function pinned to t.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestRunTenantCycle_RaceLossIsSuccess verifies the (swapped=false, err=nil)
// path is treated as success: the cycle returns nil and the next cycle
// re-plans against the post-swap ToC.
func TestCompactTenant_RaceLossIsSuccess(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour)},
		},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: false, err: nil}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour)},
		},
	})

	swapErr := errors.New("bucket: temporary failure")
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: false, err: swapErr}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
	require.NotEmpty(t, node.OutputIndexPath)

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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

	entry := indexEntry{Path: convergedPath, Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)}
	stats, err := c.compactTenantLogs(ctx, "acme", window, entry)

	require.NoError(t, err)
	require.Zero(t, stats.added)
	require.Empty(t, runner.snapshot(), "terminal window dispatches no plans")
	require.Empty(t, replacer.snapshot(), "terminal window performs no swap")
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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))
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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))
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
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

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
		c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))
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
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {{path: "[REDACTED]", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)}},
	})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "no swap for a single-index window")
}

func TestRunIndexMergePhase_MissingToCIsNoWork(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket() // no ToC written
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeNoWork, c.runIndexMergePhase(ctx, "acme", window))
}

func TestRunIndexMergePhase_MultiIndexSwaps(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "[REDACTED]", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
			{path: "[REDACTED]", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)},
		},
	})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeSwapped, c.runIndexMergePhase(ctx, "acme", window))
	require.Len(t, replacer.snapshot(), 1)
}

func logMergeBucket(ctx context.Context, t *testing.T, window time.Time, tenant string, paths []string) objstore.Bucket {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	entries := make([]testIndex, 0, len(paths))
	for _, p := range paths {
		buildIndexWithStats(ctx, t, bucket, tenant, p, []stats.Stat{
			{ObjectPath: p + ".log", SectionIndex: 0, SortSchema: "service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 30, RowCount: 1, UncompressedSize: 100},
			{ObjectPath: p + ".log", SectionIndex: 0, SortSchema: "service_name",
				Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 20, MaxTimestamp: 40, RowCount: 1, UncompressedSize: 100},
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
	c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window.Add(time.Hour)))

	require.Equal(t, phaseOutcomeNoWork, c.runLogMergePhase(ctx, "acme", window))
}

func TestRunLogMergePhase_PerIndexSwaps(t *testing.T) {
	ctx := context.Background()
	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

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
	c := newTestCoordinator(t, bucket, &fakeRunner{}, replacer, fixedClock(window.Add(time.Hour)))

	cancel() // cancel before running: the phase must not proceed
	require.Equal(t, phaseOutcomeError, c.runLogMergePhase(ctx, "acme", window))
	require.Empty(t, replacer.snapshot(), "cancelled phase performs no swap")
}

func TestRun_CancelDrainsGoroutines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	window := imWindow()
	bucket := objstore.NewInMemBucket()
	runner := &fakeRunner{}
	replacer := &fakeReplacer{}

	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))
	c.cfg.CompactionTenants = []string{"acme"}

	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not drain within 2 seconds; possible goroutine leak")
	}
}

func TestRunTenantLoop_ErrorRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	window := imWindow()
	bucket := logMergeBucket(ctx, t, window, "acme", []string{"indexes/a", "indexes/b"})
	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}

	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))

	callCount := 0
	prevRunPlan := c.runPlan
	c.runPlan = func(ctx context.Context, opts workflow.Options, plan *physical.Plan) error {
		callCount++
		if callCount >= 4 {
			cancel()
		}
		return prevRunPlan(ctx, opts, plan)
	}

	c.runTenantLoop(ctx, "acme")

	require.GreaterOrEqual(t, callCount, 3, "loop made multiple dispatch cycles")
}

func TestRun_DedupesTenants(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	window := imWindow()
	bucket := objstore.NewInMemBucket()
	runner := &fakeRunner{}
	replacer := &fakeReplacer{}

	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(time.Hour)))
	c.cfg.CompactionTenants = []string{"acme", "acme", "bravo", "acme"}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()
}
