package compactor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// tocReplacer is the subset of *metastore.TableOfContentsWriter the
// coordinator needs.
type tocReplacer interface {
	ReplaceIndexPointers(
		ctx context.Context,
		window time.Time,
		tenant string,
		oldPaths []string,
		newEntries []metastore.TableOfContentsEntry,
	) (bool, error)
}

// runFunc executes one single-root physical.Plan as a workflow.
// Injected via the coordinator's runPlan field so unit tests can swap in a
// recorder without standing up a scheduler + worker pair.
type runFunc func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (arrow.RecordBatch, error)

// coordinator drives the per-tenant compaction workers. Each iteration
// re-reads the ToC and re-plans, so a crash recovers on the next pass.
type coordinator struct {
	cfg             Config
	logger          log.Logger
	bucket          objstore.Bucket
	runPlan         runFunc
	metastoreWriter tocReplacer
	// clock is injected so tests can pin the current time; production
	// wiring sets it to time.Now.
	clock   func() time.Time
	metrics *coordinatorMetrics
	limits  Limits
}

// newCoordinator constructs a coordinator wired to a real
// *metastore.TableOfContentsWriter and a workflow.Runner. The runPlan field
// is set to the package-private runPlan helper closing over the supplied
// runner so unit tests can override it independently.
func newCoordinator(
	cfg Config,
	logger log.Logger,
	bucket objstore.Bucket,
	runner workflow.Runner,
	metastoreWriter *metastore.TableOfContentsWriter,
	reg prometheus.Registerer,
	limits Limits,
) *coordinator {
	return &coordinator{
		cfg:    cfg,
		logger: logger,
		bucket: bucket,
		runPlan: func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (arrow.RecordBatch, error) {
			return runPlan(ctx, logger, runner, opts, plan)
		},
		metastoreWriter: metastoreWriter,
		clock:           time.Now,
		metrics:         newCoordinatorMetrics(reg),
		limits:          limits,
	}
}

// Run reconciles the set of per-tenant workers against the current-window ToC
// and filtered by the per-tenant runtime config every PollingInterval until ctx
// is cancelled, then drains all workers.
func (c *coordinator) Run(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction coordinator",
		"polling_interval", c.cfg.PollingInterval,
		"plan_version", c.cfg.PlanVersion,
	)

	workers := make(map[string]context.CancelFunc)
	var wg sync.WaitGroup

	c.reconcile(ctx, workers, &wg)

	ticker := time.NewTicker(c.cfg.PollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			for _, cancel := range workers {
				cancel()
			}
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			c.reconcile(ctx, workers, &wg)
		}
	}
}

// reconcile brings the live worker set in line with the current-window ToC and
// the per-tenant enable override. workers is owned solely by the single Run
// goroutine, so it needs no synchronization.
func (c *coordinator) reconcile(ctx context.Context, workers map[string]context.CancelFunc, wg *sync.WaitGroup) {
	window := c.clock().UTC().Truncate(metastore.MetastoreWindowSize)
	discovered, ok := c.discover(ctx, window)

	// Per-tenant metric series are dropped by the worker goroutine on exit (see
	// startWorker), not here, so a still-draining worker cannot resurrect a
	// series after this cancel.
	for tenant, cancel := range workers {
		// A worker stays alive whenever any phase runs; runLog is irrelevant to
		// liveness because it implies runIndex.
		if runIndex, _ := c.limits.CompactionPhases(tenant); !runIndex {
			cancel()
			delete(workers, tenant)
		}
	}

	if !ok {
		return
	}

	for tenant := range discovered {
		if _, running := workers[tenant]; running {
			continue
		}
		if runIndex, _ := c.limits.CompactionPhases(tenant); !runIndex {
			continue
		}
		c.startWorker(ctx, workers, wg, tenant)
	}

	// Workers just started above are all in discovered, so this cancel-absent
	// pass can never cancel a freshly-started worker. Any new startWorker call
	// must keep that invariant (only start tenants present in discovered).
	for tenant, cancel := range workers {
		if _, present := discovered[tenant]; !present {
			cancel()
			delete(workers, tenant)
		}
	}
}

// startWorker launches a long-lived runTenantLoop goroutine for tenant and
// records its cancel func in workers. The goroutine deletes the tenant's
// per-tenant metric series as its final action.
func (c *coordinator) startWorker(ctx context.Context, workers map[string]context.CancelFunc, wg *sync.WaitGroup, tenant string) {
	wctx, cancel := context.WithCancel(ctx)
	workers[tenant] = cancel
	wg.Go(func() {
		defer c.metrics.deleteTenant(tenant)
		c.runTenantLoop(wctx, tenant)
	})
}

// discover reads the current-window ToC and returns the set of tenants it
// references. ok is false on any read error (missing ToC or transient), which
// tells reconcile to skip new-worker starts and absence-driven cancellation and
// leave the running set untouched: only a successfully read ToC is authoritative
// enough to conclude a tenant was removed. Membership is by map key.
func (c *coordinator) discover(ctx context.Context, window time.Time) (map[string]struct{}, bool) {
	indexes, err := loadTenantIndexes(ctx, c.bucket, window)
	if err != nil {
		if c.bucket.IsObjNotFoundErr(err) {
			level.Debug(c.logger).Log("msg", "no ToC for window; leaving workers as-is",
				"window", window, "err", err)
		} else {
			level.Warn(c.logger).Log("msg", "discover: load tenant indexes failed; leaving workers as-is",
				"window", window, "err", err)
		}
		return nil, false
	}
	out := make(map[string]struct{}, len(indexes))
	for tenant := range indexes {
		out[tenant] = struct{}{}
	}
	return out, true
}

// compactionStats reports the results of a single tenant compaction. The zero
// value (all fields 0) represents a no-op success.
type compactionStats struct {
	removed    int
	added      int
	dispatched int
}

// compactTenantLogs dispatches LogMerge tasks for a single index and swaps the
// ToC. Stats are zero-valued on any no-op (terminal index or race-loss swap).
func (c *coordinator) compactTenantLogs(
	ctx context.Context,
	tenant string,
	window time.Time,
	converged indexEntry,
) (compactionStats, error) {
	sections, sortSchema, err := logSectionRefsFor(ctx, c.bucket, tenant, converged.Path)
	if err != nil {
		return compactionStats{}, fmt.Errorf("reading log section refs: %w", err)
	}

	runs := v2.CalculateRuns(sections, compareSortKey)
	if v2.IsConverged(sections, compareSortKey) || v2.BelowMinCompactionSize(runs, uint64(c.cfg.LogMinCompactionSize)) {
		level.Debug(c.logger).Log("msg", "log-compaction: window not worth compacting, skipping",
			"tenant", tenant, "window", window)
		return compactionStats{}, nil
	}

	tasks := v2.Plan(runs, tenant, c.cfg.LogMaxRunsPerTask, sortSchema)

	resultEntries := make([]*metastore.TableOfContentsEntry, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	if c.cfg.LogMaxRunningCompactionTasks > 0 {
		g.SetLimit(c.cfg.LogMaxRunningCompactionTasks)
	}
	for i, ts := range tasks {
		g.Go(func() error {
			plan := buildLogMergePlan(tenant, window, ts)
			opts := workflow.Options{Tenant: tenant, Actor: []string{"compaction", "log-merge"}}
			rec, err := c.runPlan(gctx, opts, plan)
			if err != nil {
				return err
			}
			if rec == nil {
				return nil
			}
			artifacts, err := v2.ReadResultRecord(rec)
			if err != nil {
				return err
			}
			if len(artifacts) == 0 {
				return nil
			}
			if len(artifacts) > 1 {
				return fmt.Errorf("log-merge job produced %d artifacts, want 1", len(artifacts))
			}
			minTS, maxTS := taskBounds(ts)
			resultEntries[i] = &metastore.TableOfContentsEntry{
				Path:                 artifacts[0].Path,
				StartTime:            time.Unix(0, minTS).UTC(),
				EndTime:              time.Unix(0, maxTS).UTC(),
				UncompressedLogsSize: taskUncompressedLogsSize(ts),
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute log-merge tasks: %w", err)
	}

	newEntries := make([]metastore.TableOfContentsEntry, 0, len(resultEntries))
	for _, e := range resultEntries {
		if e != nil {
			newEntries = append(newEntries, *e)
		}
	}
	if len(newEntries) == 0 {
		return compactionStats{}, nil
	}

	// A converged row of 0 uncompressed size means unknown. Legacy objects
	// carry positive-but-wrong internal stats (line length only, omitting
	// structured metadata), so persisting 0 keeps the row unknown and lets us
	// later distinguish and backfill those indexes without rescanning.
	if converged.UncompressedLogsSize == 0 {
		for i := range newEntries {
			newEntries[i].UncompressedLogsSize = 0
		}
	}

	oldPaths := []string{converged.Path}
	stats := compactionStats{
		removed:    len(oldPaths),
		added:      len(newEntries),
		dispatched: len(tasks),
	}

	if c.cfg.DryRun {
		return compactionStats{dispatched: len(tasks)}, nil
	}

	c.fillFileSizes(ctx, newEntries)

	phase2Ctx, cancel := context.WithTimeout(ctx, c.cfg.ToCConsolidateTimeout)
	defer cancel()
	swapped, err := c.metastoreWriter.ReplaceIndexPointers(phase2Ctx, window, tenant, oldPaths, newEntries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("failed to replace index pointers after log-compaction: %w", err)
	}
	if !swapped {
		level.Debug(c.logger).Log("msg", "log-compaction ToC replace race-loss / already-converged",
			"tenant", tenant, "window", window)
		return compactionStats{}, nil
	}
	return stats, nil
}

// compactTenant performs the per-(tenant, window) IndexMerge. Stats are
// zero-valued on any no-op success (no tasks planned or a race-loss swap).
func (c *coordinator) compactTenant(
	ctx context.Context,
	tenant string,
	window time.Time,
	entries []indexEntry,
) (compactionStats, error) {
	// Plan.
	sections := sectionRefsFor(entries)
	runs := v2.CalculateRuns(sections, compareSortKey)
	tasks := v2.Plan(runs, tenant, c.cfg.MaxRunsPerTask, nil)
	if len(tasks) == 0 {
		level.Debug(c.logger).Log("msg", "tenant cycle: planner produced no tasks",
			"tenant", tenant, "window", window)
		return compactionStats{}, nil
	}

	resultEntries := make([]*metastore.TableOfContentsEntry, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	if c.cfg.MaxRunningCompactionTasks > 0 {
		g.SetLimit(c.cfg.MaxRunningCompactionTasks)
	}
	for i, ts := range tasks {
		g.Go(func() error {
			plan := buildIndexMergePlan(tenant, window, ts)
			opts := workflow.Options{Tenant: tenant, Actor: []string{"compaction", "index-merge"}}
			rec, err := c.runPlan(gctx, opts, plan)
			if err != nil {
				return err
			}
			if rec == nil {
				return nil
			}
			artifacts, err := v2.ReadResultRecord(rec)
			if err != nil {
				return err
			}
			if len(artifacts) == 0 {
				return nil
			}
			if len(artifacts) > 1 {
				return fmt.Errorf("index-merge job produced %d artifacts, want 1", len(artifacts))
			}
			minTS, maxTS := taskBounds(ts)
			resultEntries[i] = &metastore.TableOfContentsEntry{
				Path:                 artifacts[0].Path,
				StartTime:            time.Unix(0, minTS).UTC(),
				EndTime:              time.Unix(0, maxTS).UTC(),
				UncompressedLogsSize: taskUncompressedLogsSize(ts),
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute compaction tasks: %w", err)
	}

	oldPaths := make([]string, len(entries))
	for i, e := range entries {
		oldPaths[i] = e.Path
	}

	newEntries := make([]metastore.TableOfContentsEntry, 0, len(resultEntries))
	for _, e := range resultEntries {
		if e != nil {
			newEntries = append(newEntries, *e)
		}
	}
	if len(newEntries) == 0 {
		return compactionStats{}, nil
	}

	if c.cfg.DryRun {
		return compactionStats{}, nil
	}

	c.fillFileSizes(ctx, newEntries)

	phase2Ctx, cancel := context.WithTimeout(ctx, c.cfg.ToCConsolidateTimeout)
	defer cancel()
	swapped, err := c.metastoreWriter.ReplaceIndexPointers(phase2Ctx, window, tenant, oldPaths, newEntries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("failed to replace index pointers after compaction: %w", err)
	}
	if !swapped {
		level.Debug(c.logger).Log("msg", "ToC replace race-loss / already-converged",
			"tenant", tenant, "window", window)
		return compactionStats{}, nil
	}
	level.Info(c.logger).Log("msg", "tenant cycle complete",
		"tenant", tenant, "window", window,
		"removed_indexes", len(oldPaths),
		"added_indexes", len(newEntries),
	)
	return compactionStats{
		removed:    len(oldPaths),
		added:      len(newEntries),
		dispatched: len(tasks),
	}, nil
}

// taskBounds returns the min/max timestamp (unix nanos) across all sections
// in a task's runs.
func taskBounds(task *compactionv2pb.TaskSpec) (minTS, maxTS int64) {
	first := true
	for _, run := range task.Runs {
		for _, sec := range run.Sections {
			if first {
				minTS, maxTS, first = sec.MinTimestamp, sec.MaxTimestamp, false
				continue
			}
			if sec.MinTimestamp < minTS {
				minTS = sec.MinTimestamp
			}
			if sec.MaxTimestamp > maxTS {
				maxTS = sec.MaxTimestamp
			}
		}
	}
	return minTS, maxTS
}

// taskUncompressedLogsSize sums UncompressedSize across every section in the
// task. A section size of 0 means "unknown" (e.g. a legacy ToC row written
// before sizes were recorded); a single unknown input poisons the whole total,
// so the result is 0 unless every contributing section is known.
func taskUncompressedLogsSize(task *compactionv2pb.TaskSpec) uint64 {
	var total uint64
	for _, run := range task.Runs {
		for _, sec := range run.Sections {
			if sec.UncompressedSize == 0 {
				return 0
			}
			total += uint64(sec.UncompressedSize)
		}
	}
	return total
}

// fileSizeStatConcurrency bounds concurrent bucket.Attributes calls when
// filling in index FileSize before a ToC replace.
const fileSizeStatConcurrency = 16

// fillFileSizes stats each entry's object and sets FileSize. Best-effort: when
// the stat fails (missing or not-yet-visible object) the entry keeps its zero
// FileSize and is persisted as-is.
//
// Stats run concurrently (bounded by fileSizeStatConcurrency) because each
// bucket.Attributes call can take tens of milliseconds; serializing hundreds
// of entries would dominate the cycle. Each goroutine writes a distinct slice
// element, so the concurrent writes do not race.
func (c *coordinator) fillFileSizes(ctx context.Context, entries []metastore.TableOfContentsEntry) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fileSizeStatConcurrency)
	for i := range entries {
		g.Go(func() error {
			start := time.Now()
			attrs, err := c.bucket.Attributes(gctx, entries[i].Path)
			c.metrics.observeFileSizeStat(time.Since(start))
			if err != nil {
				level.Warn(c.logger).Log("msg", "attributes for output failed", "path", entries[i].Path, "err", err)
				return nil
			}
			if attrs.Size > 0 {
				entries[i].FileSize = uint64(attrs.Size)
			}
			return nil
		})
	}
	_ = g.Wait()
}

// phase is the current step of a tenant's flip-flop worker.
type phase int

const (
	phaseIndexMerge phase = iota
	phaseLogMerge
)

func (p phase) flip() phase {
	if p == phaseIndexMerge {
		return phaseLogMerge
	}
	return phaseIndexMerge
}

// phaseOutcome is the result of running one phase; it drives the flip-vs-retry
// decision.
type phaseOutcome int

const (
	phaseOutcomeError   phaseOutcome = iota // re-arm same phase
	phaseOutcomeNoWork                      // success, nothing to do; flip
	phaseOutcomeSwapped                     // ToC swap applied/observed; flip
)

// runIndexMergePhase runs IndexMerge for the tenant's current window and swaps
// the ToC.
func (c *coordinator) runIndexMergePhase(ctx context.Context, tenant string, window time.Time) phaseOutcome {
	start := c.clock()
	entries, ok := c.tenantEntries(ctx, tenant, window)
	if !ok {
		return phaseOutcomeError
	}

	c.metrics.observeEntries(tenant, entries, c.clock())

	if len(entries) <= 1 {
		c.metrics.observeTenantCycle(tenant, "converged", c.clock().Sub(start), compactionStats{})
		return phaseOutcomeNoWork
	}

	stats, err := c.compactTenant(ctx, tenant, window, entries)
	dur := c.clock().Sub(start)
	if err != nil {
		// Only the coordinator context being cancelled means shutdown. A
		// DeadlineExceeded from the child ToCConsolidateTimeout context is an
		// ordinary phase failure and must be logged and retried, not silently
		// swallowed as if the worker were draining.
		if ctx.Err() != nil {
			return phaseOutcomeError
		}
		level.Warn(c.logger).Log("msg", "index-merge phase failed",
			"tenant", tenant, "window", window, "err", err)
		c.metrics.observeTenantCycle(tenant, "failed", dur, compactionStats{})
		return phaseOutcomeError
	}
	// compactTenant returns zero stats for every no-op success; a real swap sets
	// added > 0.
	if stats.added == 0 {
		c.metrics.observeTenantCycle(tenant, "converged", dur, compactionStats{})
		return phaseOutcomeNoWork
	}
	c.metrics.observeTenantCycle(tenant, "compacted", dur, stats)
	return phaseOutcomeSwapped
}

// tenantEntries reads the current-window ToC and returns the tenant's entries.
// A missing ToC yields (nil, true) — no work, not an error. Any other read
// error yields (nil, false).
func (c *coordinator) tenantEntries(ctx context.Context, tenant string, window time.Time) ([]indexEntry, bool) {
	indexes, err := loadTenantIndexes(ctx, c.bucket, window)
	if err != nil {
		if c.bucket.IsObjNotFoundErr(err) {
			level.Debug(c.logger).Log("msg", "no ToC for window",
				"tenant", tenant, "window", window, "err", err)
			return nil, true
		}
		level.Warn(c.logger).Log("msg", "phase: load tenant indexes failed",
			"tenant", tenant, "window", window, "err", err)
		return nil, false
	}
	return indexes[tenant], true
}

// runLogMergePhase schedules one LogMerge task per index file for the [tenant]
// in the current [window]. Any error retries. Retries are safe because swapping
// an index that already swapped is a no-op. Context cancellation is not an
// error.
func (c *coordinator) runLogMergePhase(ctx context.Context, tenant string, window time.Time) phaseOutcome {
	start := c.clock()
	entries, ok := c.tenantEntries(ctx, tenant, window)
	if !ok {
		return phaseOutcomeError
	}
	if len(entries) == 0 {
		c.metrics.observeTenantLogCycle(tenant, "converged", c.clock().Sub(start), compactionStats{})
		return phaseOutcomeNoWork
	}

	var agg compactionStats
	anySwapped := false
	anyError := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return phaseOutcomeError
		}
		stats, err := c.compactTenantLogs(ctx, tenant, window, entry)
		if err != nil {
			// Only shut down when the coordinator context is cancelled. A
			// DeadlineExceeded from this index's child ToCConsolidateTimeout is
			// an ordinary per-index failure: record it and move on so a single
			// slow swap doesn't skip the remaining indexes.
			if ctx.Err() != nil {
				return phaseOutcomeError
			}
			level.Warn(c.logger).Log("msg", "log-merge phase: index failed",
				"tenant", tenant, "window", window, "index", entry.Path, "err", err)
			anyError = true
			continue
		}
		if stats.added > 0 {
			anySwapped = true
			agg.removed += stats.removed
			agg.added += stats.added
			agg.dispatched += stats.dispatched
		}
	}

	dur := c.clock().Sub(start)
	switch {
	case anySwapped && anyError:
		c.metrics.observeTenantLogCycle(tenant, "compacted", dur, agg)
		return phaseOutcomeError
	case anyError:
		c.metrics.observeTenantLogCycle(tenant, "failed", dur, compactionStats{})
		return phaseOutcomeError
	case anySwapped:
		c.metrics.observeTenantLogCycle(tenant, "compacted", dur, agg)
		return phaseOutcomeSwapped
	default:
		c.metrics.observeTenantLogCycle(tenant, "converged", dur, compactionStats{})
		return phaseOutcomeNoWork
	}
}

// runTenantLoop runs the IndexMerge<->LogMerge cycle for one tenant until ctx
// is cancelled. It never returns an error and never sleeps: on error it retries
// the same phase, otherwise it flips. It re-reads the per-tenant phase
// enablement each iteration and skips the LogMerge phase when log compaction is
// disabled, so an index-only tenant runs IndexMerge exclusively.
func (c *coordinator) runTenantLoop(ctx context.Context, tenant string) {
	p := phaseIndexMerge
	for {
		if ctx.Err() != nil {
			return
		}
		runIndex, runLog := c.limits.CompactionPhases(tenant)
		if !runIndex && !runLog {
			return
		}
		if p == phaseLogMerge && !runLog {
			p = p.flip()
			continue
		}

		window := c.clock().UTC().Truncate(metastore.MetastoreWindowSize)

		start := c.clock()
		var outcome phaseOutcome
		switch p {
		case phaseIndexMerge:
			outcome = c.runIndexMergePhase(ctx, tenant, window)
		case phaseLogMerge:
			outcome = c.runLogMergePhase(ctx, tenant, window)
		}
		if ctx.Err() != nil {
			return
		}
		c.metrics.observeCycle(cycleOutcome(outcome), c.clock().Sub(start))

		if outcome != phaseOutcomeError {
			p = p.flip()
		}
	}
}

// cycleOutcome maps a phaseOutcome to a cyclesTotal outcome label. The label set
// is now compacted|converged|failed (reduced from the old poll-loop set).
func cycleOutcome(o phaseOutcome) string {
	switch o {
	case phaseOutcomeSwapped:
		return "compacted"
	case phaseOutcomeError:
		return "failed"
	default:
		return "converged"
	}
}
