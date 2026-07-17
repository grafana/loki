package compactor

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
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

// Run spawns one worker per configured tenant and blocks until ctx is
// cancelled, then waits for all workers to drain.
func (c *coordinator) Run(ctx context.Context) error {
	tenants := []string(c.cfg.CompactionTenants)
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction workers",
		"tenants", len(tenants),
		"plan_version", c.cfg.PlanVersion,
	)

	var wg sync.WaitGroup
	seen := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		if _, dup := seen[tenant]; dup {
			continue
		}
		seen[tenant] = struct{}{}
		wg.Add(1)
		go func(tenant string) {
			defer wg.Done()
			c.runTenantLoop(ctx, tenant)
		}(tenant)
	}

	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
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

	var pathBuilder indexMergePath
	var idBuf bytes.Buffer
	outputs := make([]string, len(tasks))
	for i, ts := range tasks {
		outputs[i] = pathBuilder.Build(tenant, window, c.cfg.PlanVersion, i, logTaskSectionIDs(ts.Runs, &idBuf))
	}

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
			return c.runPlan(gctx, opts, plan)
		})
	}
	if err := g.Wait(); err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute log-merge tasks: %w", err)
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
		return compactionStats{dispatched: len(tasks)}, nil
	}

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
// the same phase, otherwise it flips.
func (c *coordinator) runTenantLoop(ctx context.Context, tenant string) {
	p := phaseIndexMerge
	for {
		if ctx.Err() != nil {
			return
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
