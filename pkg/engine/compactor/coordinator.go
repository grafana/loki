package compactor

import (
	"context"
	"fmt"
	"strings"
	"time"

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
) *coordinator {
	return &coordinator{
		cfg:    cfg,
		logger: logger,
		bucket: bucket,
		runPlan: func(ctx context.Context, opts workflow.Options, plan *physical.Plan) error {
			return runPlan(ctx, logger, runner, opts, plan)
		},
		metastoreWriter: metastoreWriter,
		clock:           time.Now,
		metrics:         newCoordinatorMetrics(reg),
	}
}

// Run blocks until ctx is cancelled, ticking every cfg.PollingInterval and
// running one compaction cycle per tick. Per-cycle errors are logged and
// swallowed; the next tick is always attempted.
func (c *coordinator) Run(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction coordinator",
		"polling_interval", c.cfg.PollingInterval,
		"max_runs_per_task", c.cfg.MaxRunsPerTask,
		"plan_version", c.cfg.PlanVersion,
	)

	// Run one cycle immediately on startup; subsequent cycles are
	// ticker-driven. Without this initial tick a fresh coordinator
	// would wait a full polling_interval before doing anything.
	c.runCycle(ctx)

	t := time.NewTicker(c.cfg.PollingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			c.runCycle(ctx)
		}
	}
}

// runCycle performs one full poll iteration: reads the most-recent ToC and runs
// compaction for every tenant whose window has > 1 index. All errors are logged
// and swallowed; the loop is designed to recover on the next tick by
// re-planning against the post-swap ToC.
func (c *coordinator) runCycle(ctx context.Context) {
	start := c.clock()
	window := start.UTC().Truncate(metastore.MetastoreWindowSize)

	indexes, err := loadTenantIndexes(ctx, c.bucket, window)
	if err != nil {
		if c.bucket.IsObjNotFoundErr(err) {
			level.Debug(c.logger).Log("msg", "no ToC for current window", "window", window)
			c.metrics.observeCycle("toc_not_found", c.clock().Sub(start))
			return
		}
		level.Warn(c.logger).Log("msg", "cycle aborted: load tenant indexes",
			"window", window, "err", err)
		c.metrics.observeCycle("index_load_err", c.clock().Sub(start))
		return
	}
	if len(indexes) == 0 {
		level.Debug(c.logger).Log("msg", "cycle: no tenants in ToC", "window", window)
		c.metrics.observeCycle("no_indexes", c.clock().Sub(start))
		return
	}

	var (
		converged = 0 // tenants already at <=1 index (no work)
		compacted = 0 // tenants whose cycle ran to completion
		failed    = 0 // tenants whose cycle returned an error
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
	c.metrics.observeCycle(cycleOutcome, duration)

	//TODO(twhitney): will want a metric for this
	if duration > c.cfg.PollingInterval {
		level.Warn(c.logger).Log(
			"msg", "cycle duration exceeded polling interval; next tick will be dropped",
			"duration", duration,
			"polling_interval", c.cfg.PollingInterval,
		)
	}
}

// tenantCycleResult is the single outcome of one per-(tenant, window) cycle.
type tenantCycleResult int

const (
	// tenantCycleConverged: tenant was already at <=1 index, no work done.
	tenantCycleConverged tenantCycleResult = iota
	// tenantCycleCompacted: tenant cycle ran to completion.
	tenantCycleCompacted
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

	if len(entries) <= 1 {
		level.Debug(c.logger).Log("msg", "cycle: tenant converged, skipping",
			"tenant", tenant, "indexes", len(entries))
		c.metrics.observeTenantCycle(tenant, "converged", c.clock().Sub(tenantStart), compactionStats{})
		c.metrics.observeEntries(tenant, entries, c.clock())
		return tenantCycleConverged
	}

	stats, err := c.compactTenant(ctx, tenant, window, entries)
	tenantDuration := c.clock().Sub(tenantStart)
	if err != nil {
		level.Warn(c.logger).Log("msg", "tenant cycle failed",
			"tenant", tenant, "window", window, "err", err)
		c.metrics.observeTenantCycle(tenant, "failed", tenantDuration, compactionStats{})
		c.metrics.observeEntries(tenant, entries, c.clock())
		return tenantCycleFailed
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
	tasks := v2.Plan(ctx, sections, tenant, c.cfg.MaxRunsPerTask)
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
