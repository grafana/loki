package compactor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
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

	// Experiment knobs, derived from cfg in newCoordinator. tenantFilter (from
	// cfg.Tenants) restricts + force-enables tenants; targetWindow/hasTargetWindow
	// (from cfg.TargetWindow) pin the window set; runOnce exits after convergence.
	tenantFilter    map[string]struct{}
	targetWindow    time.Time
	hasTargetWindow bool
	runOnce         bool
	// logMergeRetryBackoff is the base backoff before a LogMerge task retry;
	// newCoordinator sets it to logMergeTaskRetryBackoff. Tests may leave it zero
	// to retry without sleeping.
	logMergeRetryBackoff time.Duration
}

const (
	// logMergeTaskRetries is the number of additional attempts for a failing
	// LogMerge task before the phase fails. Retries cover transient worker drops
	// (e.g. a restarting/OOMed worker) so one flaky task does not fail the phase.
	logMergeTaskRetries = 10
	// logMergeTaskRetryBackoff is the base backoff before the first retry,
	// doubled per subsequent retry and capped in retryDelay.
	logMergeTaskRetryBackoff = 2 * time.Second
)

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
	c := &coordinator{
		cfg:    cfg,
		logger: logger,
		bucket: bucket,
		runPlan: func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (arrow.RecordBatch, error) {
			return runPlan(ctx, logger, runner, opts, plan)
		},
		metastoreWriter:      metastoreWriter,
		clock:                time.Now,
		metrics:              newCoordinatorMetrics(reg),
		limits:               limits,
		tenantFilter:         parseTenants(cfg.Tenants),
		runOnce:              cfg.RunOnce,
		logMergeRetryBackoff: logMergeTaskRetryBackoff,
	}
	// TargetWindow parseability is checked in Config.Validate, so an error here is
	// unexpected; log and fall back to the default window set.
	if w, ok, err := cfg.ParseTargetWindow(); err != nil {
		level.Warn(logger).Log("msg", "ignoring invalid target_window", "err", err)
	} else if ok {
		c.targetWindow = w.Truncate(metastore.MetastoreWindowSize)
		c.hasTargetWindow = true
	}
	return c
}

// parseTenants splits a comma-separated tenant allow-list into a set. Returns
// nil ("all tenants") for an empty or whitespace-only input.
func parseTenants(s string) map[string]struct{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	set := make(map[string]struct{})
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = struct{}{}
		}
	}
	return set
}

// phasesFor reports which compaction phases run for tenant. With a non-empty
// experiment allow-list (cfg.Tenants), listed tenants run both phases and
// unlisted tenants run none, bypassing the Limits gate; otherwise the per-tenant
// Limits decide.
func (c *coordinator) phasesFor(tenant string) (runIndex, runLog bool) {
	if len(c.tenantFilter) > 0 {
		if _, ok := c.tenantFilter[tenant]; !ok {
			return false, false
		}
		return true, true
	}
	return c.limits.CompactionPhases(tenant)
}

// Run reconciles the set of per-tenant workers against the compacted windows'
// ToCs and filtered by the per-tenant runtime config every PollingInterval
// until ctx is cancelled, then drains all workers.
func (c *coordinator) Run(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction coordinator",
		"polling_interval", c.cfg.PollingInterval,
		"plan_version", c.cfg.PlanVersion,
		"run_once", c.runOnce,
		"target_window", c.cfg.TargetWindow,
		"tenants", c.cfg.Tenants,
	)

	workers := make(map[string]context.CancelFunc)
	var wg sync.WaitGroup

	c.reconcile(ctx, workers, &wg)

	// run-once: each selected tenant's worker returns once it converges (see
	// runTenantLoop), so waiting for all of them is the whole job. Discovery reads
	// the seeded ToC directly, so one reconcile starts every worker; transient
	// worker/scheduler unreadiness is absorbed by per-task retries.
	if c.runOnce {
		wg.Wait()
		level.Info(c.logger).Log("msg", "run-once: all selected tenants converged, stopping coordinator")
		return nil
	}

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

// windows returns the metastore-aligned windows the coordinator compacts on
// each pass, newest first: the current window followed by cfg.WindowLookback
// older windows. With the default lookback of 0 this is exactly the current
// window, preserving the original single-window behaviour.
func (c *coordinator) windows() []time.Time {
	if c.hasTargetWindow {
		return []time.Time{c.targetWindow}
	}
	current := c.clock().UTC().Truncate(metastore.MetastoreWindowSize)
	out := make([]time.Time, 0, c.cfg.WindowLookback+1)
	for i := 0; i <= c.cfg.WindowLookback; i++ {
		out = append(out, current.Add(-time.Duration(i)*metastore.MetastoreWindowSize))
	}
	return out
}

// reconcile brings the live worker set in line with the compacted windows' ToCs
// and the per-tenant enable override. workers is owned solely by the single Run
// goroutine, so it needs no synchronization.
func (c *coordinator) reconcile(ctx context.Context, workers map[string]context.CancelFunc, wg *sync.WaitGroup) {
	discovered, allOK := c.discoverAll(ctx)

	// Per-tenant metric series are dropped by the worker goroutine on exit (see
	// startWorker), not here, so a still-draining worker cannot resurrect a
	// series after this cancel.
	for tenant, cancel := range workers {
		// A worker stays alive whenever any phase runs; runLog is irrelevant to
		// liveness because it implies runIndex.
		if runIndex, _ := c.phasesFor(tenant); !runIndex {
			cancel()
			delete(workers, tenant)
		}
	}

	// Starting a worker for a discovered tenant is always safe, even when a
	// window's ToC failed to load (allOK=false): a tenant present in any
	// successfully-read window has real work. This is what lets a populated
	// older window run while the current window's ToC does not yet exist.
	for tenant := range discovered {
		if _, running := workers[tenant]; running {
			continue
		}
		if runIndex, _ := c.phasesFor(tenant); !runIndex {
			continue
		}
		c.startWorker(ctx, workers, wg, tenant)
	}

	// Absence-driven cancellation requires an authoritative picture: only when
	// every window read cleanly is a tenant's absence from the union conclusive.
	// A transient read failure on any window leaves the running set untouched so
	// a tenant present only in the unread window is not spuriously cancelled.
	if !allOK {
		return
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

// discoverAll unions the tenant sets of every compacted window's ToC. allOK is
// true only when every window read cleanly (a missing ToC or transient error on
// any window clears it); reconcile uses allOK to gate absence-driven
// cancellation so an unread window never causes a spurious cancel.
func (c *coordinator) discoverAll(ctx context.Context) (map[string]struct{}, bool) {
	discovered := make(map[string]struct{})
	allOK := true
	for _, window := range c.windows() {
		tenants, ok := c.discover(ctx, window)
		if !ok {
			allOK = false
			continue
		}
		for tenant := range tenants {
			discovered[tenant] = struct{}{}
		}
	}
	return discovered, allOK
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

// discover reads one window's ToC and returns the set of tenants it references.
// ok is false on any read error (missing ToC or transient); discoverAll folds
// that into its allOK result so reconcile can leave the running set untouched
// when the picture is incomplete. Only a successfully read ToC is authoritative
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

	runs := v2.CalculateRuns(sections)
	if v2.IsTerminal(runs, uint64(c.cfg.LogMinCompactionSize)) {
		level.Debug(c.logger).Log("msg", "log-compaction: window not worth compacting, skipping",
			"tenant", tenant, "window", window)
		return compactionStats{}, nil
	}

	tasks := v2.Plan(runs, tenant, c.cfg.LogMaxRunsPerTask, sortSchema)

	total := len(tasks)
	level.Info(c.logger).Log("msg", "log-compaction: dispatching tasks",
		"tenant", tenant, "window", window, "tasks", total,
		"max_running", c.cfg.LogMaxRunningCompactionTasks, "max_retries", logMergeTaskRetries)

	var completed atomic.Int64
	resultEntries := make([]*metastore.TableOfContentsEntry, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	if c.cfg.LogMaxRunningCompactionTasks > 0 {
		g.SetLimit(c.cfg.LogMaxRunningCompactionTasks)
	}
	for i, ts := range tasks {
		g.Go(func() error {
			plan := buildLogMergePlan(tenant, window, ts)
			opts := workflow.Options{Tenant: tenant, Actor: []string{"compaction", "log-merge"}}
			rec, err := c.runLogMergeTask(gctx, opts, plan, i)
			if err != nil {
				return err
			}
			c.logLogMergeProgress(tenant, window, int(completed.Add(1)), total)
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

// runLogMergeTask runs one LogMerge plan, retrying on error up to
// logMergeTaskRetries additional times with capped backoff. Retries cover
// transient worker drops (e.g. a restarting/OOMed worker not yet reconnected);
// re-running is safe because a swap that already happened is a no-op. Returns
// the task's result record on the first success, or the last error once retries
// are exhausted or ctx is cancelled.
func (c *coordinator) runLogMergeTask(ctx context.Context, opts workflow.Options, plan *physical.Plan, task int) (arrow.RecordBatch, error) {
	maxAttempts := logMergeTaskRetries + 1
	var (
		rec arrow.RecordBatch
		err error
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if rec, err = c.runPlan(ctx, opts, plan); err == nil {
			return rec, nil
		}
		if attempt < maxAttempts {
			level.Warn(c.logger).Log("msg", "log-merge task failed, retrying",
				"task", task, "attempt", attempt, "max_attempts", maxAttempts, "err", err)
			if werr := sleepCtx(ctx, retryDelay(c.logMergeRetryBackoff, attempt-1)); werr != nil {
				return nil, werr
			}
		}
	}
	level.Warn(c.logger).Log("msg", "log-merge task failed, retries exhausted",
		"task", task, "attempts", maxAttempts, "err", err)
	return nil, err
}

// logLogMergeProgress emits a progress line at roughly every ten-percent step
// and on the final task.
func (c *coordinator) logLogMergeProgress(tenant string, window time.Time, done, total int) {
	if done != total && done%progressStep(total) != 0 {
		return
	}
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	level.Info(c.logger).Log("msg", "log-compaction progress",
		"tenant", tenant, "window", window, "completed", done, "total", total, "pct", pct)
}

// progressStep returns the completion interval at which to log progress: about
// ten updates over a large cycle, every task for small ones.
func progressStep(total int) int {
	if step := total / 10; step > 1 {
		return step
	}
	return 1
}

// retryDelay returns the backoff before the next retry: base doubled per prior
// retry, capped at 30s. A non-positive base disables backoff (used in tests).
func retryDelay(base time.Duration, priorRetries int) time.Duration {
	if base <= 0 {
		return 0
	}
	const maxDelay = 30 * time.Second
	if d := base << priorRetries; d > 0 && d < maxDelay {
		return d
	}
	return maxDelay
}

// sleepCtx waits for d or until ctx is cancelled, returning ctx.Err() on
// cancellation and nil otherwise. A non-positive d returns immediately.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
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
	runs := v2.CalculateRuns(sections)
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
// disabled, so an index-only tenant runs IndexMerge exclusively. Each phase runs
// against every window returned by c.windows(); the phase flips only when no
// window errored so a single failing window retries the whole phase.
func (c *coordinator) runTenantLoop(ctx context.Context, tenant string) {
	p := phaseIndexMerge
	noProgress := 0
	for {
		if ctx.Err() != nil {
			return
		}
		runIndex, runLog := c.phasesFor(tenant)
		if !runIndex && !runLog {
			return
		}
		if p == phaseLogMerge && !runLog {
			p = p.flip()
			continue
		}

		outcome := c.runPhaseAllWindows(ctx, tenant, p)
		if ctx.Err() != nil {
			return
		}

		if outcome == phaseOutcomeError {
			noProgress = 0 // never converge while a phase is failing
			continue       // re-arm the same phase
		}
		if outcome == phaseOutcomeSwapped {
			noProgress = 0
		} else {
			noProgress++
		}
		p = p.flip()
		// run-once: two consecutive non-error phases with no swap span a full
		// IndexMerge+LogMerge round that changed nothing — the tenant has
		// converged.
		if c.runOnce && noProgress >= 2 {
			level.Info(c.logger).Log("msg", "run-once: tenant converged, stopping worker", "tenant", tenant)
			return
		}
	}
}

// runPhaseAllWindows runs phase p for the tenant against each compacted window,
// recording the worker-loop cycle metric per window, and returns the worst
// outcome across them. Error dominates (the caller re-arms the same phase);
// otherwise swapped (progress) outranks no-work. Windows are independent: a
// window with no ToC no-ops while a populated one does real work.
func (c *coordinator) runPhaseAllWindows(ctx context.Context, tenant string, p phase) phaseOutcome {
	worst := phaseOutcomeNoWork
	for _, window := range c.windows() {
		if ctx.Err() != nil {
			return worst
		}

		start := c.clock()
		var outcome phaseOutcome
		switch p {
		case phaseIndexMerge:
			outcome = c.runIndexMergePhase(ctx, tenant, window)
		case phaseLogMerge:
			outcome = c.runLogMergePhase(ctx, tenant, window)
		}
		c.metrics.observeCycle(cycleOutcome(outcome), c.clock().Sub(start))
		worst = worseOutcome(worst, outcome)
	}
	return worst
}

// worseOutcome ranks phase outcomes so the tenant loop retries on any error and
// otherwise reports progress: error > swapped > no-work.
func worseOutcome(a, b phaseOutcome) phaseOutcome {
	switch {
	case a == phaseOutcomeError || b == phaseOutcomeError:
		return phaseOutcomeError
	case a == phaseOutcomeSwapped || b == phaseOutcomeSwapped:
		return phaseOutcomeSwapped
	default:
		return phaseOutcomeNoWork
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
