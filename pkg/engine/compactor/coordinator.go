package compactor

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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
type runFunc func(ctx context.Context, opts workflow.Options, plan *physical.Plan) error

// coordinator drives the per-cycle compaction loop. It is stateless across
// cycles: every poll tick re-reads the most-recent ToC and starts from
// scratch. Crash recovery comes from re-planning over the next poll's ToC
// view + idempotent ReplaceIndexPointers.
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

	// targetWindow, when hasTargetWindow is true, pins every cycle to a fixed
	// window instead of clock().Truncate(MetastoreWindowSize). Used to compact
	// a historical/seeded window in experiments.
	targetWindow    time.Time
	hasTargetWindow bool
	// tenantFilter, when non-empty, restricts a cycle to these tenant IDs.
	tenantFilter map[string]struct{}
	// runOnce makes Run exit after the first cycle that processes the selected
	// tenants without failure.
	runOnce bool
	// logMergeRetryBackoff is the base backoff before a LogMerge task retry.
	// newCoordinator sets it to logMergeTaskRetryBackoff; tests leave it zero to
	// retry without sleeping.
	logMergeRetryBackoff time.Duration
}

const (
	// logMergeTaskRetries is the number of additional attempts made for a
	// failing LogMerge task before the tenant cycle fails. Retries cover
	// transient worker drops (e.g. a "connection closed" from a
	// restarting/OOMed worker) so a single flaky task does not fail the whole
	// cycle on the first error.
	logMergeTaskRetries = 10
	// logMergeTaskRetryBackoff is the base backoff before the first LogMerge
	// retry, doubled per subsequent retry and capped in retryDelay.
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
) *coordinator {
	c := &coordinator{
		cfg:    cfg,
		logger: logger,
		bucket: bucket,
		runPlan: func(ctx context.Context, opts workflow.Options, plan *physical.Plan) error {
			return runPlan(ctx, logger, runner, opts, plan)
		},
		metastoreWriter:      metastoreWriter,
		clock:                time.Now,
		metrics:              newCoordinatorMetrics(reg),
		tenantFilter:         parseTenants(cfg.Tenants),
		runOnce:              cfg.RunOnce,
		logMergeRetryBackoff: logMergeTaskRetryBackoff,
	}
	// TargetWindow is validated in Config.Validate before New is reached, so a
	// parse error here is unexpected; log and fall back to the current window.
	if w, ok, err := cfg.ParseTargetWindow(); err != nil {
		level.Warn(logger).Log("msg", "ignoring invalid target_window", "err", err)
	} else if ok {
		c.targetWindow = w.UTC().Truncate(metastore.MetastoreWindowSize)
		c.hasTargetWindow = true
	}
	return c
}

// parseTenants splits a comma-separated tenant list into a set. Returns nil
// (meaning "all tenants") for an empty or whitespace-only input.
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

// Run blocks until ctx is cancelled, ticking every cfg.PollingInterval and
// running one compaction cycle per tick. Per-cycle errors are logged and
// swallowed; the next tick is always attempted. When run_once is set it
// returns nil once the selected tenants converge (a cycle makes no further
// index-compaction progress without failing).
func (c *coordinator) Run(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction coordinator",
		"polling_interval", c.cfg.PollingInterval,
		"max_runs_per_task", c.cfg.MaxRunsPerTask,
		"plan_version", c.cfg.PlanVersion,
		"target_window", c.cfg.TargetWindow,
		"tenants", c.cfg.Tenants,
		"run_once", c.runOnce,
	)

	// Run one cycle immediately on startup; subsequent cycles are
	// ticker-driven. Without this initial tick a fresh coordinator
	// would wait a full polling_interval before doing anything.
	if c.runOnceDone(c.runCycle(ctx)) {
		return nil
	}

	t := time.NewTicker(c.cfg.PollingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if c.runOnceDone(c.runCycle(ctx)) {
				return nil
			}
		}
	}
}

// cycleResult summarizes one poll cycle's per-tenant outcomes so Run can decide
// when a run-once coordinator has done its job.
type cycleResult struct {
	compacted    int
	converged    int
	logCompacted int
	failed       int
}

// processed reports whether the cycle handled at least one tenant — whether it
// compacted, log-compacted, or found the tenant already converged.
func (r cycleResult) processed() bool {
	return r.compacted+r.converged+r.logCompacted > 0
}

// runOnceDone reports whether a run-once coordinator should stop after the
// given cycle. It stops once a cycle makes no further index-compaction progress
// (compacted == 0) and hits no failures — i.e. the selected tenants have
// converged. A cycle that still compacted has more merge rounds to run; a
// failed cycle keeps polling so a transient error (e.g. the worker not yet
// connected on startup) is retried. Always false when run_once is disabled.
func (c *coordinator) runOnceDone(res cycleResult) bool {
	if !c.runOnce {
		return false
	}
	if res.failed == 0 && res.compacted == 0 && res.processed() {
		level.Info(c.logger).Log("msg", "run-once: selected tenants converged, stopping coordinator")
		return true
	}
	return false
}

// runCycle performs one full poll iteration: reads the ToC for the target
// window (or the current window when unset) and runs compaction for every
// selected tenant whose window has > 1 index. All errors are logged and
// swallowed; the loop is designed to recover on the next tick by re-planning
// against the post-swap ToC.
func (c *coordinator) runCycle(ctx context.Context) cycleResult {
	start := c.clock()
	window := start.UTC().Truncate(metastore.MetastoreWindowSize)
	if c.hasTargetWindow {
		window = c.targetWindow
	}

	indexes, err := loadTenantIndexes(ctx, c.bucket, window)
	if err != nil {
		if c.bucket.IsObjNotFoundErr(err) {
			level.Debug(c.logger).Log("msg", "no ToC for window", "window", window)
			c.metrics.observeCycle("toc_not_found", c.clock().Sub(start))
			return cycleResult{}
		}
		level.Warn(c.logger).Log("msg", "cycle aborted: load tenant indexes",
			"window", window, "err", err)
		c.metrics.observeCycle("index_load_err", c.clock().Sub(start))
		return cycleResult{}
	}
	if len(c.tenantFilter) > 0 {
		for tenant := range indexes {
			if _, ok := c.tenantFilter[tenant]; !ok {
				delete(indexes, tenant)
			}
		}
	}
	if len(indexes) == 0 {
		level.Debug(c.logger).Log("msg", "cycle: no tenants to compact in window", "window", window)
		c.metrics.observeCycle("no_indexes", c.clock().Sub(start))
		return cycleResult{}
	}

	var (
		converged    = 0 // tenants with no index, or a single index with no log-merge work
		compacted    = 0 // tenants whose cycle ran to completion
		logCompacted = 0 // tenants whose window dispatched log-merge tasks
		failed       = 0 // tenants whose cycle returned an error
	)
	for tenant, entries := range indexes {
		// Stop the cycle early on context cancellation. Remaining tenants
		// would fail immediately on the cancelled ctx, doing no useful work
		// but inflating the failed metric and risking false-positive alerts.
		// The next polling tick (with a fresh ctx) re-plans the entire window.
		if ctx.Err() != nil {
			break
		}
		switch c.runTenantCycle(ctx, tenant, window, entries) {
		case tenantCycleConverged:
			converged++
		case tenantCycleCompacted:
			compacted++
		case tenantCycleLogCompacted:
			logCompacted++
		case tenantCycleFailed:
			failed++
		}
	}

	duration := c.clock().Sub(start)
	level.Info(c.logger).Log(
		"msg", "cycle complete",
		"window", window,
		"duration", duration,
		"tenants_total", len(indexes),
		"tenants_compacted", compacted,
		"tenants_converged", converged,
		"tenants_log_compacted", logCompacted,
		"tenants_failed", failed,
	)

	cycleOutcome := "converged"
	if failed > 0 && compacted <= 0 {
		cycleOutcome = "compaction_failed"
	}

	if compacted > 0 {
		if failed > 0 {
			cycleOutcome = "compacted_with_failures"
		} else {
			cycleOutcome = "compacted"
		}
	}

	if compacted == 0 && failed == 0 && logCompacted > 0 {
		cycleOutcome = "log_compacted"
	}
	c.metrics.observeCycle(cycleOutcome, duration)

	//TODO(twhitney): will want a metric for this
	if duration > c.cfg.PollingInterval {
		level.Warn(c.logger).Log(
			"msg", "cycle duration exceeded polling interval; next tick will be dropped",
			"duration", duration,
			"polling_interval", c.cfg.PollingInterval,
		)
	}

	return cycleResult{
		compacted:    compacted,
		converged:    converged,
		logCompacted: logCompacted,
		failed:       failed,
	}
}

// tenantCycleResult is the single outcome of one per-(tenant, window) cycle.
type tenantCycleResult int

const (
	// tenantCycleConverged: no index at all, or a single converged index with no
	// log-merge work; no work dispatched.
	tenantCycleConverged tenantCycleResult = iota
	// tenantCycleCompacted: index-compaction cycle ran to completion.
	tenantCycleCompacted
	// tenantCycleLogCompacted: converged window dispatched log-merge tasks.
	tenantCycleLogCompacted
	// tenantCycleFailed: the tenant cycle returned an error.
	tenantCycleFailed
)

// runTenantCycle drives one per-(tenant, window) cycle end to end and returns a
// tenantCycleResult.
func (c *coordinator) runTenantCycle(
	ctx context.Context,
	tenant string,
	window time.Time,
	entries []indexEntry,
) tenantCycleResult {
	tenantStart := c.clock()

	if len(entries) == 0 {
		level.Debug(c.logger).Log("msg", "cycle: tenant has no index, skipping", "tenant", tenant)
		c.metrics.observeTenantCycle(tenant, "converged", c.clock().Sub(tenantStart), compactionStats{})
		c.metrics.observeEntries(tenant, entries, c.clock())
		return tenantCycleConverged
	}

	if len(entries) == 1 {
		result, stats, err := c.compactTenantLogs(ctx, tenant, window, entries[0])
		tenantDuration := c.clock().Sub(tenantStart)
		c.metrics.observeEntries(tenant, entries, c.clock())
		switch {
		case err != nil:
			level.Warn(c.logger).Log("msg", "tenant log-compaction failed",
				"tenant", tenant, "window", window, "err", err)
			c.metrics.observeTenantLogCycle(tenant, "failed", tenantDuration, compactionStats{})
			return tenantCycleFailed
		case result == tenantCycleConverged:
			c.metrics.observeTenantLogCycle(tenant, "converged", tenantDuration, compactionStats{})
			return tenantCycleConverged
		default:
			c.metrics.observeTenantLogCycle(tenant, "compacted", tenantDuration, stats)
			return tenantCycleLogCompacted
		}
	}

	stats, err := c.compactTenant(ctx, tenant, window, entries)
	tenantDuration := c.clock().Sub(tenantStart)
	c.metrics.observeEntries(tenant, entries, c.clock())
	if err != nil {
		level.Warn(c.logger).Log("msg", "tenant cycle failed",
			"tenant", tenant, "window", window, "err", err)
		c.metrics.observeTenantCycle(tenant, "failed", tenantDuration, compactionStats{})
		return tenantCycleFailed
	}
	if stats.added == 0 {
		// No index-merge work this cycle: the planner produced no tasks, or the
		// ToC swap was a race-loss / already-converged no-op. Report converged
		// (not compacted) so a run-once coordinator can tell that index
		// compaction has nothing left to do for this tenant.
		c.metrics.observeTenantCycle(tenant, "converged", tenantDuration, stats)
		return tenantCycleConverged
	}
	c.metrics.observeTenantCycle(tenant, "compacted", tenantDuration, stats)
	return tenantCycleCompacted
}

// compactionStats reports the results of a single tenant compaction. The zero
// value (all fields 0) represents a no-op success.
type compactionStats struct {
	removed    int
	added      int
	dispatched int
}

// compactTenantLogs plans and dispatches log-merge tasks for a window with a
// converged index. It dispatches LogMerge tasks, then swaps the ToC. Returns
// tenantCycleConverged when the window is terminal (no work) or the ToC swap is
// a race-loss / already-converged no-op, tenantCycleLogCompacted when the swap
// succeeds, and tenantCycleFailed on error.
func (c *coordinator) compactTenantLogs(
	ctx context.Context,
	tenant string,
	window time.Time,
	converged indexEntry,
) (tenantCycleResult, compactionStats, error) {
	sections, sortSchema, err := logSectionRefsFor(ctx, c.bucket, tenant, converged.Path)
	if err != nil {
		return tenantCycleFailed, compactionStats{}, fmt.Errorf("reading log section refs: %w", err)
	}

	runs := v2.CalculateRuns(sections)
	if v2.IsTerminal(runs, uint64(c.cfg.LogMinCompactionSize)) {
		level.Debug(c.logger).Log("msg", "log-compaction: window not worth compacting, skipping",
			"tenant", tenant, "window", window)
		return tenantCycleConverged, compactionStats{}, nil
	}

	tasks := v2.Plan(runs, tenant, c.cfg.LogMaxRunsPerTask, sortSchema)

	var pathBuilder indexMergePath
	var idBuf bytes.Buffer
	outputs := make([]string, len(tasks))
	for i, ts := range tasks {
		outputs[i] = pathBuilder.Build(tenant, window, c.cfg.PlanVersion, i, logTaskSectionIDs(ts.Runs, &idBuf))
	}

	total := len(tasks)
	level.Info(c.logger).Log("msg", "log-compaction cycle: dispatching tasks",
		"tenant", tenant, "window", window, "tasks", total,
		"max_running", c.cfg.LogMaxRunningCompactionTasks, "max_retries", logMergeTaskRetries)

	var completed atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	if c.cfg.LogMaxRunningCompactionTasks > 0 {
		g.SetLimit(c.cfg.LogMaxRunningCompactionTasks)
	}
	for i, ts := range tasks {
		g.Go(func() error {
			plan := buildLogMergePlan(tenant, window, ts, outputs[i])
			opts := workflow.Options{
				Tenant: tenant,
				Actor:  []string{"compaction", "log-merge"},
			}
			if err := c.runLogMergeTask(gctx, opts, plan, outputs[i]); err != nil {
				return err
			}
			c.logLogMergeProgress(tenant, window, int(completed.Add(1)), total)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return tenantCycleFailed, compactionStats{}, fmt.Errorf("failed to execute log-merge tasks: %w", err)
	}

	oldPaths := []string{converged.Path}
	newEntries := makeTocEntries(tasks, outputs)

	// removed = the single converged index this window replaces; added = the
	// merged indexes written; dispatched = log-merge tasks run this cycle.
	stats := compactionStats{
		removed:    len(oldPaths),
		added:      len(outputs),
		dispatched: len(tasks),
	}

	level.Debug(c.logger).Log("msg", "log-compacted",
		"tenant", tenant, "window", window,
		"dry_run", c.cfg.DryRun,
		"tasks", len(tasks),
		"removed_index", converged.Path,
		"added_indexes", strings.Join(outputs, ","),
	)

	if c.cfg.DryRun {
		// Tasks ran but the ToC was left untouched, so no indexes were
		// added/removed; only report the tasks dispatched.
		return tenantCycleLogCompacted, compactionStats{dispatched: len(tasks)}, nil
	}

	phase2Ctx, cancel := context.WithTimeout(ctx, c.cfg.ToCConsolidateTimeout)
	defer cancel()
	swapped, err := c.metastoreWriter.ReplaceIndexPointers(phase2Ctx, window, tenant, oldPaths, newEntries)
	if err != nil {
		return tenantCycleFailed, compactionStats{}, fmt.Errorf("failed to replace index pointers after log-compaction: %w", err)
	}
	if !swapped {
		level.Debug(c.logger).Log("msg", "log-compaction ToC replace race-loss / already-converged",
			"tenant", tenant, "window", window)
		return tenantCycleConverged, compactionStats{}, nil
	}
	return tenantCycleLogCompacted, stats, nil
}

// runLogMergeTask runs one LogMerge plan, retrying on error up to
// logMergeTaskRetries additional times before giving up. Retries cover
// transient worker drops (e.g. a "connection closed" from a restarting/OOMed
// worker) so a single flaky task no longer fails the whole tenant cycle on the
// first error. Re-running a task is safe: output paths are deterministic and
// the worker short-circuits when the output already exists. It returns nil on
// the first success, or the last error once retries are exhausted or ctx is
// cancelled.
func (c *coordinator) runLogMergeTask(ctx context.Context, opts workflow.Options, plan *physical.Plan, output string) error {
	maxAttempts := logMergeTaskRetries + 1
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = c.runPlan(ctx, opts, plan); err == nil {
			return nil
		}
		if attempt < maxAttempts {
			level.Warn(c.logger).Log("msg", "log-merge task failed, retrying",
				"output", output, "attempt", attempt, "max_attempts", maxAttempts, "err", err)
			if werr := sleepCtx(ctx, retryDelay(c.logMergeRetryBackoff, attempt-1)); werr != nil {
				return werr
			}
		}
	}
	level.Warn(c.logger).Log("msg", "log-merge task failed, retries exhausted",
		"output", output, "attempts", maxAttempts, "err", err)
	return err
}

// logLogMergeProgress emits a periodic progress line for a log-compaction
// cycle: at roughly every ten-percent step and on the final task.
func (c *coordinator) logLogMergeProgress(tenant string, window time.Time, done, total int) {
	if done != total && done%progressStep(total) != 0 {
		return
	}
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	level.Info(c.logger).Log("msg", "log-compaction cycle progress",
		"tenant", tenant, "window", window, "completed", done, "total", total, "pct", pct)
}

// progressStep returns the completion interval at which to log cycle progress:
// about ten updates over a large cycle, and every task for small cycles.
func progressStep(total int) int {
	if step := total / 10; step > 1 {
		return step
	}
	return 1
}

// retryDelay returns the backoff before the next retry: base doubled per prior
// retry, capped. A non-positive base disables the backoff (used in tests).
func retryDelay(base time.Duration, priorRetries int) time.Duration {
	if base <= 0 {
		return 0
	}
	const maxDelay = 30 * time.Second
	// A large priorRetries could overflow the shift to a non-positive value;
	// clamp to the cap in that case.
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

// compactTenant performs the per-(tenant, window) compaction. Returns the
// index/task deltas and an error if present. A zero-value compactionStats with
// a nil error is returned when there was no work (planner produced no tasks) or
// when the ToC swap was a race-loss / already-converged no-op — both are
// successes that produced no index/task deltas.
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

	// Compute deterministic output paths per task. A single indexMergePath
	// is reused across all tasks in this cycle to reduce allocations.
	var pathBuilder indexMergePath
	outputs := make([]string, len(tasks))
	for i, ts := range tasks {
		outputs[i] = pathBuilder.Build(tenant, window, c.cfg.PlanVersion, i, taskSectionIDs(ts.Runs))
	}

	g, gctx := errgroup.WithContext(ctx)
	if c.cfg.MaxRunningCompactionTasks > 0 {
		g.SetLimit(c.cfg.MaxRunningCompactionTasks)
	}
	for i, ts := range tasks {
		g.Go(func() error {
			plan := buildIndexMergePlan(tenant, window, ts, outputs[i])
			opts := workflow.Options{
				Tenant: tenant,
				Actor:  []string{"compaction", "index-merge"},
			}
			return c.runPlan(gctx, opts, plan)
		})
	}
	if err := g.Wait(); err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute compaction tasks: %w", err)
	}

	// Replace index pointers with new indexes
	oldPaths := make([]string, len(entries))
	for i, e := range entries {
		oldPaths[i] = e.Path
	}
	newEntries := makeTocEntries(tasks, outputs)

	level.Debug(c.logger).Log("msg", "compacted indexes",
		"tenant", tenant, "window", window,
		"dry_run", c.cfg.DryRun,
		"tasks", len(tasks),
		"removed_indexes", strings.Join(oldPaths, ","),
		"added_indexes", strings.Join(outputs, ","),
	)

	if c.cfg.DryRun {
		return compactionStats{}, nil
	}

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
		"tasks", len(tasks),
		"removed_indexes", len(oldPaths),
		"added_indexes", len(outputs),
	)
	return compactionStats{
		removed:    len(oldPaths),
		added:      len(outputs),
		dispatched: len(tasks),
	}, nil
}

// taskSectionIDs returns canonical "<ObjectPath>#<SectionIndex>" IDs for
// every section across all Runs in a task. Used as input to
// indexMergePath.Build. The output is unsorted; Build sorts internally so
// order here doesn't affect the resulting path.
func taskSectionIDs(runs []*compactionv2pb.RunRef) []string {
	var ids []string
	for _, r := range runs {
		for _, s := range r.Sections {
			ids = append(ids, fmt.Sprintf("%s#%d", s.ObjectPath, s.SectionIndex))
		}
	}
	return ids
}

// logTaskSectionIDs returns unique IDs for every section across [runs]. A log
// SectionRef's identity is {ObjectPath, SectionIndex, labelTuple}. Components
// are separated by \x00 (which cannot occur in object paths or label values) to
// keep the encoding unambiguous. Callers may reuse the same [buf] across calls.
func logTaskSectionIDs(runs []*compactionv2pb.RunRef, buf *bytes.Buffer) []string {
	var ids []string
	for _, r := range runs {
		for _, s := range r.Sections {
			buf.Reset()
			buf.WriteString(s.ObjectPath)
			buf.WriteByte(0)
			buf.WriteString(strconv.FormatInt(s.SectionIndex, 10))
			for _, v := range s.MinKey {
				buf.WriteByte(0)
				buf.WriteString(v)
			}
			ids = append(ids, buf.String())
		}
	}
	return ids
}

// makeTocEntries pairs each output path with the time bounds derived
// from its task's source SectionRefs. tasks[i] is the TaskSpec that produced
// outputs[i].
//
// Time bounds: min(MinTimestamp) / max(MaxTimestamp) across all SectionRefs
// in the task.
func makeTocEntries(
	tasks []*compactionv2pb.TaskSpec,
	outputs []string,
) []metastore.TableOfContentsEntry {
	entries := make([]metastore.TableOfContentsEntry, len(outputs))
	for i, ts := range tasks {
		minTS, maxTS := int64(0), int64(0)
		first := true
		for _, run := range ts.Runs {
			for _, sec := range run.Sections {
				if first {
					minTS = sec.MinTimestamp
					maxTS = sec.MaxTimestamp
					first = false
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
		entries[i] = metastore.TableOfContentsEntry{
			Path:      outputs[i],
			StartTime: time.Unix(0, minTS).UTC(),
			EndTime:   time.Unix(0, maxTS).UTC(),
		}
	}
	return entries
}
