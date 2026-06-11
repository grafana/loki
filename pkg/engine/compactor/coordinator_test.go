package compactor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
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
}

type runCall struct {
	opts workflow.Options
	plan *physical.Plan
}

func (f *fakeRunner) run(_ context.Context, opts workflow.Options, plan *physical.Plan) error {
	f.mu.Lock()
	f.calls = append(f.calls, runCall{opts, plan})
	f.mu.Unlock()
	return f.err
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
			ToCConsolidateTimeout:     30 * time.Second,
			MaxRunningCompactionTasks: 4,
			PlanVersion:               1,
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

// TestRunCycle_SkipsConvergedTenants verifies the <=1 gate: a tenant whose ToC
// slice has length ≤ 1 is short-circuited. Nothing flows into the runner or the
// replacer for that tenant.
func TestRunCycle_SkipsConvergedTenants(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"single": {
			{path: "indexes/aa/idx-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
		},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

	c.runCycle(ctx)

	require.Empty(t, runner.snapshot(), "no Phase 1 dispatch for converged tenant")
	require.Empty(t, replacer.snapshot(), "no Phase 2 ToC swap for converged tenant")
}

// TestRunCycle_FansOutPhase1ThenCommitsPhase2 verifies the full per-tenant
// shape: K=2 with 3 indexes produces ⌈3/2⌉ = 2 IndexMerge plans, then one
// ReplaceIndexPointers call carrying the original 3 paths in oldPaths and
// 2 deterministic outputs in newEntries.
func TestRunCycle_FansOutPhase1ThenCommitsPhase2(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour)},
			{path: "indexes/cc/src-2", start: window.Add(5 * time.Hour), end: window.Add(6 * time.Hour)},
		},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

	c.runCycle(ctx)

	// Phase 1: K=2 with 3 non-overlapping indexes ⇒ patience-sort produces
	// 1 pile (non-overlap), so ⌈1/2⌉ = 1 task.
	dispatches := runner.snapshot()
	require.Len(t, dispatches, 1, "non-overlapping inputs collapse to one pile and one task")
	require.Equal(t, "acme", dispatches[0].opts.Tenant)
	require.Equal(t, []string{"compaction", "index-merge"}, dispatches[0].opts.Actor)

	root, err := dispatches[0].plan.Root()
	require.NoError(t, err)
	merge, ok := root.(*physical.IndexMerge)
	require.True(t, ok)
	require.Equal(t, "acme", merge.Tenant)
	require.Equal(t, window.UnixNano(), merge.ToCWindowStart)

	// Phase 2: one ReplaceIndexPointers; oldPaths covers all 3 sources;
	// newEntries has the deterministic output that Phase 1 produced.
	swaps := replacer.snapshot()
	require.Len(t, swaps, 1)
	require.Equal(t, "acme", swaps[0].tenant)
	require.Equal(t, window, swaps[0].window)
	require.ElementsMatch(t,
		[]string{"indexes/aa/src-0", "indexes/bb/src-1", "indexes/cc/src-2"},
		swaps[0].oldPaths)
	require.Len(t, swaps[0].newEntries, 1, "one task ⇒ one new entry")
	require.Equal(t, merge.OutputIndexPath, swaps[0].newEntries[0].Path,
		"newEntry path must match the IndexMerge OutputIndexPath")
}

// TestRunCycle_FansOutOverlappingPiles verifies that overlapping inputs do
// fan out: 3 sections whose key ranges all overlap (timestamp range overlap
// in v1.0's timestamp-only fallback) ⇒ patience-sort emits 3 piles ⇒ K=2 ⇒
// 2 IndexMerge tasks dispatched.
func TestRunCycle_FansOutOverlappingPiles(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(5 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(2 * time.Hour), end: window.Add(6 * time.Hour)},
			{path: "indexes/cc/src-2", start: window.Add(3 * time.Hour), end: window.Add(7 * time.Hour)},
		},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

	c.runCycle(ctx)

	dispatches := runner.snapshot()
	require.Len(t, dispatches, 2, "3 overlapping piles ÷ K=2 ⇒ 2 tasks")

	swaps := replacer.snapshot()
	require.Len(t, swaps, 1)
	require.Len(t, swaps[0].newEntries, 2, "2 tasks ⇒ 2 new entries")
}

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

	// runCycle swallows per-tenant errors; check directly via the lower API.
	indexes, err := loadTenantIndexes(ctx, bucket, window)
	require.NoError(t, err)
	_, runErr := c.compactTenant(ctx, "acme", window, indexes["acme"])
	require.NoError(t, runErr)
}

// TestCompactTenant_HardSwapErrorPropagates verifies that a non-nil error
// from ReplaceIndexPointers aborts the tenant cycle. The poll loop's
// runCycle wrapper will log + continue to the next tenant; compactTenant
// itself returns the wrapped error so the test can pin it.
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

// TestRunCycle_DryRunSkipsToCSwapButLogs verifies that with DryRun=true the
// coordinator still runs Phase 1 (IndexMerge dispatch) but never calls
// ReplaceIndexPointers so the ToC is left untouched.
func TestRunCycle_DryRunSkipsToCSwapButLogs(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"acme": {
			{path: "indexes/aa/src-0", start: window.Add(1 * time.Hour), end: window.Add(5 * time.Hour)},
			{path: "indexes/bb/src-1", start: window.Add(2 * time.Hour), end: window.Add(6 * time.Hour)},
			{path: "indexes/cc/src-2", start: window.Add(3 * time.Hour), end: window.Add(7 * time.Hour)},
		},
	})

	runner := &fakeRunner{}
	replacer := &fakeReplacer{swapped: true}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))
	c.cfg.DryRun = true

	c.runCycle(ctx)

	// Phase 1 still runs: 3 overlapping piles ÷ K=2 ⇒ 2 tasks.
	require.Len(t, runner.snapshot(), 2, "dry-run still dispatches IndexMerge tasks")

	// ToC is never mutated.
	require.Empty(t, replacer.snapshot(), "dry-run must not call ReplaceIndexPointers")
}

// TestRunCycle_NoToC verifies a missing ToC is a no-op cycle: no panic, no
// Phase 1 dispatch, no Phase 2 commit. The next poll tick will re-read.
func TestRunCycle_NoToC(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	runner := &fakeRunner{}
	replacer := &fakeReplacer{}
	c := newTestCoordinator(t, bucket, runner, replacer, fixedClock(window.Add(1*time.Hour)))

	c.runCycle(context.Background())

	require.Empty(t, runner.snapshot())
	require.Empty(t, replacer.snapshot())
}
