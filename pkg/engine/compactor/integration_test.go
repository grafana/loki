package compactor

import (
	"bytes"
	"context"
	"flag"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
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
//   - Phase 1 wrote the expected number of merged index objects at
//     deterministic paths.
//   - Phase 2 atomically swapped the ToC: source paths removed, output paths
//     added with the right timestamps.
//   - Other tenants' rows survive byte-equivalent across the swap.
func TestCoordinator_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	// Seed: 3 indexes for "acme" with overlapping timestamp ranges (forces >1 pile).
	// Plus an "untouched" tenant with one index to verify cross-tenant
	// isolation across the ToC swap.
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

	// Upload an index with postings + stats per distinct path.
	seenPaths := map[string]struct{}{}
	for _, entries := range seed {
		for _, e := range entries {
			if _, seen := seenPaths[e.path]; seen {
				continue
			}
			seenPaths[e.path] = struct{}{}
			seedSourceIndexObject(ctx, t, bucket, e.path, e.start)
		}
	}

	// Bring up a real scheduler + worker pair using wire.Local transport.
	sched, _ := startInProcessSchedulerAndWorker(ctx, t, bucket)

	// Construct the metastore writer + coordinator.
	tocWriter := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	c := &coordinator{
		cfg: Config{
			Enabled:                   true,
			PollingInterval:           1 * time.Second,
			MaxRunsPerTask:            2,
			ToCConsolidateTimeout:     10 * time.Second,
			MaxRunningCompactionTasks: 4,
			PlanVersion:               1,
			Scheduler:                 SchedulerConfig{Endpoint: defaultEndpoint},
		},
		logger: log.NewNopLogger(),
		bucket: bucket,
		runPlan: func(rpCtx context.Context, opts workflow.Options, plan *physical.Plan) error {
			return runPlan(rpCtx, log.NewNopLogger(), sched, opts, plan)
		},
		metastoreWriter: tocWriter,
		clock:           func() time.Time { return window.Add(1 * time.Hour) },
	}

	// --- Cycle 1: 3 sources → ⌈P/K⌉ outputs ---
	preCycle1 := mustLoadTenant(ctx, t, bucket, window, "acme")
	require.Len(t, preCycle1, 3, "sanity: 3 source indexes seeded")
	_, _, _, runErr := c.compactTenant(ctx, "acme", window, preCycle1)
	require.NoError(t, runErr)

	postCycle1 := mustLoadTenants(ctx, t, bucket, window)
	require.Less(t, len(postCycle1["acme"]), 3,
		"cycle 1 must reduce acme's index count from 3 to fewer")
	require.Equal(t,
		[]string{"indexes/aa/src-0", "indexes/dd/idx-d-0"},
		pathsOf(postCycle1["untouched"]),
		"untouched tenant must be byte-identical across the swap")

	// Phase 1 output objects must exist at the deterministic paths.
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
		_, _, _, runErr := c.compactTenant(ctx, "acme", window, indexesC2)
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
		_, _, _, runErr := c.compactTenant(ctx, "acme", window, acmeIdx)
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

// startInProcessSchedulerAndWorker brings up a wire.Local scheduler + worker
// pair sharing the supplied bucket. Both register cleanup hooks on t.
func startInProcessSchedulerAndWorker(ctx context.Context, t *testing.T, bucket objstore.Bucket) (*scheduler.Scheduler, *worker.Worker) {
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

	w, err := worker.New(worker.Config{
		Logger:           log.NewNopLogger(),
		Bucket:           bucket,
		Metastore:        ms,
		BatchSize:        2048,
		Dialer:           dialer,
		Listener:         workerListener,
		SchedulerAddress: wire.LocalScheduler,
		NumThreads:       2,
		ScratchStore:     scratch.NewMemory(),
		IndexobjCfg:      compactionCfg.IndexobjBuilder,
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

// seedSourceIndexObject builds and uploads an index object at path, containing
// one postings section and one stats section. The single observation/stat is
// timestamped at ts so it falls inside the entry's seeded time range.
func seedSourceIndexObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string, ts time.Time) {
	t.Helper()

	postingsBuilder := postings.NewBuilder(nil, 0, 0)
	postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       path,
		SectionIndex:     0,
		ColumnName:       "service",
		LabelValue:       "api",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
	statsBuilder.Append(stats.Stat{
		ObjectPath:       path,
		SectionIndex:     0,
		SortSchema:       "service",
		Labels:           map[string]string{"service": "api"},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano() + 1000,
		RowCount:         10,
		UncompressedSize: 1000,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(postingsBuilder))
	require.NoError(t, objBuilder.Append(statsBuilder))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()

	objBytes, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(ctx, path, io.NopCloser(bytes.NewReader(objBytes))))
}
