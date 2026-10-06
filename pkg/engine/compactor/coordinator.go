package compactor

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

const indexMergeIterations = 3

// coordinator drives the per-tenant compaction workers. Each iteration
// re-reads the ToC and re-plans, so a crash recovers on the next pass.
type coordinator struct {
	cfg    Config
	logger log.Logger
	bucket objstore.Bucket
	// indexDispatcher runs IndexMerge plans. logDispatcher runs LogMerge and
	// SortObject plans. They have separate concurrency limits.
	indexDispatcher *planDispatcher
	logDispatcher   *planDispatcher
	publisher       *tocPublisher
	// clock is injected so tests can pin the current time; production
	// wiring sets it to time.Now.
	clock func() time.Time
	// sleep blocks for the given duration or until ctx is cancelled. Injected
	// so tests can make per-tenant backoff waits instant and deterministic.
	sleep   func(ctx context.Context, d time.Duration)
	metrics *coordinatorMetrics
	limits  Limits
}

// newCoordinator constructs a coordinator wired to a real
// *metastore.TableOfContentsWriter and a workflow.Runner.
func newCoordinator(
	cfg Config,
	logger log.Logger,
	bucket objstore.Bucket,
	runner workflow.Runner,
	metastoreWriter *metastore.TableOfContentsWriter,
	reg prometheus.Registerer,
	limits Limits,
) *coordinator {
	run := func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
		return runPlan(ctx, logger, runner, opts, plan)
	}
	return &coordinator{
		cfg:             cfg,
		logger:          logger,
		bucket:          bucket,
		indexDispatcher: &planDispatcher{runPlan: run, limit: cfg.MaxRunningCompactionTasks},
		logDispatcher:   &planDispatcher{runPlan: run, limit: cfg.LogMaxRunningCompactionTasks},
		publisher: &tocPublisher{
			writer:  metastoreWriter,
			timeout: cfg.ToCConsolidateTimeout,
			dryRun:  cfg.DryRun,
		},
		clock:   time.Now,
		sleep:   sleepUntil,
		metrics: newCoordinatorMetrics(reg),
		limits:  limits,
	}
}

// sleepUntil blocks for d or returns early when ctx is cancelled. A
// non-positive d returns immediately.
func sleepUntil(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// Run keeps one worker running for each tenant that has work in a compacted
// window's ToC and is enabled for compaction. It re-checks every
// PollingInterval until ctx is cancelled, then waits for all workers to exit.
func (c *coordinator) Run(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction coordinator",
		"polling_interval", c.cfg.PollingInterval,
		"plan_version", c.cfg.PlanVersion,
	)
	s := newTenantsSupervisor(c.logger, c.cfg.PollingInterval, c.discoverTenants, c.compactionEnabled, c.runTenant)
	return s.Run(ctx)
}

// windows returns the metastore-aligned windows the coordinator compacts on
// each pass, newest first: the current window followed by cfg.WindowLookback
// older windows.
func (c *coordinator) windows() []time.Time {
	current := c.clock().UTC().Truncate(metastore.MetastoreWindowSize)
	out := make([]time.Time, 0, c.cfg.WindowLookback+1)
	for i := 0; i <= c.cfg.WindowLookback; i++ {
		out = append(out, current.Add(-time.Duration(i)*metastore.MetastoreWindowSize))
	}
	return out
}

// compactionEnabled reports whether tenant may run any compaction phase. Log
// compaction implies index compaction, so the index phase decides.
func (c *coordinator) compactionEnabled(tenant string) bool {
	runIndex, _ := c.limits.CompactionPhases(tenant)
	return runIndex
}

// runTenant runs tenant's compaction loop until ctx is cancelled. It deletes
// the tenant's per-tenant metric series when the loop exits.
func (c *coordinator) runTenant(ctx context.Context, tenant string) {
	defer c.metrics.deleteTenant(tenant)
	c.runTenantLoop(ctx, tenant)
}

// discoverTenants returns the union of the tenants with a ToC in every
// compacted window.
//
// A window with no ToCs contributes no tenants. This happens after each window
// boundary and in windows that received no data. A listing error fails the
// call, because a partial result could make a tenant with work look absent.
func (c *coordinator) discoverTenants(ctx context.Context) (map[string]struct{}, error) {
	tenants := make(map[string]struct{})
	for _, window := range c.windows() {
		listed, err := metastore.ListTableOfContentsTenants(ctx, c.bucket, window)
		if err != nil {
			return nil, fmt.Errorf("list ToCs for window %s: %w", window, err)
		}
		for _, tenant := range listed {
			tenants[tenant] = struct{}{}
		}
	}
	return tenants, nil
}

// compactionStats reports the results of a single tenant compaction. The zero
// value (all fields 0) represents a no-op success.
type compactionStats struct {
	removed    int
	added      int
	dispatched int
}

// Merge returns the field-wise sum of s and other.
func (s compactionStats) Merge(other compactionStats) compactionStats {
	return compactionStats{
		removed:    s.removed + other.removed,
		added:      s.added + other.added,
		dispatched: s.dispatched + other.dispatched,
	}
}

type indexedLogLayout struct {
	sortSchema string
	shardCount int64
}

// replaceLogIndex swaps sourceIndex for newEntries, which hold one entry per
// dispatched task.
func (c *coordinator) replaceLogIndex(
	ctx context.Context,
	tenant string,
	window time.Time,
	sourceIndex indexEntry,
	newEntries []metastore.TableOfContentsEntry,
) (compactionStats, error) {
	if len(newEntries) == 0 {
		return compactionStats{}, fmt.Errorf("replace source log index %q: no replacement entries", sourceIndex.Path)
	}

	swapped, err := c.publisher.Replace(ctx, tenant, window, []string{sourceIndex.Path}, newEntries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("replace source log index %q: %w", sourceIndex.Path, err)
	}
	if !swapped {
		return compactionStats{}, nil
	}
	return compactionStats{
		removed:    1,
		added:      len(newEntries),
		dispatched: len(newEntries),
	}, nil
}

// compactTenantLogs processes one layout-homogeneous index. Objects matching
// the target layout are compacted with LogMerge; incompatible objects are
// individually rewritten with SortObject. Stats are zero-valued on any no-op.
func (c *coordinator) compactTenantLogs(
	ctx context.Context,
	tenant string,
	window time.Time,
	sourceIndex indexEntry,
) (compactionStats, error) {
	entryLogger := log.With(c.logger, "tenant", tenant, "entry", sourceIndex.Path)
	sections, sortSchema, shardCount, err := logSectionRefsFor(ctx, c.bucket, tenant, sourceIndex.Path)
	if err != nil {
		return compactionStats{}, fmt.Errorf("reading log section refs: %w", err)
	}
	if len(sections) == 0 {
		return compactionStats{}, nil
	}

	targetSortSchema := c.limits.SortSchemaLabels(tenant)
	// Decide whether the logs referenced by this index file are ready to merge, or if they need re-sorting first.
	if !slices.Equal(sortSchema, targetSortSchema) || shardCount != int64(streams.ShardFactor) {
		return c.sortTenantLogObjects(ctx, tenant, window, sourceIndex, sections, targetSortSchema)
	}

	// Begin k-way merge planning
	runs := v2.CalculateRuns(sections, compareLogSortPrefix)
	if v2.IsConvergedWithInclusiveOverlap(sections, compareLogSortPrefix) ||
		v2.BelowMinCompactionSize(runs, uint64(c.cfg.LogMinCompactionSize)) {
		level.Debug(entryLogger).Log("msg", "log-compaction: window not worth compacting, skipping", "window", window)
		return compactionStats{}, nil
	}

	tasks := v2.Plan(runs, tenant, c.cfg.LogMaxRunsPerTask, sortSchema)
	if len(tasks) == 0 {
		return compactionStats{}, fmt.Errorf("no log merge tasks to execute")
	}

	level.Info(entryLogger).Log("msg", "planned log compaction tasks", "input_runs", len(runs), "tasks", len(tasks))
	logMergeTaskDetails(entryLogger, tasks)

	plans := make([]*physical.Plan, len(tasks))
	for i, task := range tasks {
		plans[i] = buildLogMergePlan(tenant, window, task)
	}
	artifacts, err := c.logDispatcher.Run(ctx, tenant, "log-merge", plans)
	if err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute log-merge tasks: %w", err)
	}
	resultEntries := make([]metastore.TableOfContentsEntry, len(tasks))
	for i, task := range tasks {
		minTS, maxTS := taskBounds(task)
		resultEntries[i] = metastore.TableOfContentsEntry{
			Path:      artifacts[i].Path,
			StartTime: time.Unix(0, minTS).UTC(),
			EndTime:   time.Unix(0, maxTS).UTC(),
		}
	}

	stats, err := c.replaceLogIndex(ctx, tenant, window, sourceIndex, resultEntries)
	if err != nil {
		return compactionStats{}, err
	}
	if stats.removed > 0 {
		level.Debug(entryLogger).Log("msg", "log-compaction step completed for index", "index_files_added", stats.added, "index_files_removed", stats.removed, "tasks_dispatched", stats.dispatched)
	}
	return stats, nil
}

func (c *coordinator) sortTenantLogObjects(
	ctx context.Context,
	tenant string,
	window time.Time,
	sourceIndex indexEntry,
	sections []v2.Section[logSortPrefix],
	targetSortSchema []string,
) (compactionStats, error) {
	type object struct {
		path         string
		minTimestamp int64
		maxTimestamp int64
	}

	var objects []*object
	objectsByPath := make(map[string]*object)
	for _, section := range sections {
		path := section.Ref.ObjectPath
		obj, ok := objectsByPath[path]
		if !ok {
			obj = &object{
				path:         path,
				minTimestamp: section.Ref.MinTimestamp,
				maxTimestamp: section.Ref.MaxTimestamp,
			}
			objectsByPath[path] = obj
			objects = append(objects, obj)
		}
		obj.minTimestamp = min(obj.minTimestamp, section.Ref.MinTimestamp)
		obj.maxTimestamp = max(obj.maxTimestamp, section.Ref.MaxTimestamp)
	}

	plans := make([]*physical.Plan, len(objects))
	for i, obj := range objects {
		plans[i] = buildSortObjectPlan(obj.path, targetSortSchema)
	}
	artifacts, err := c.logDispatcher.Run(ctx, tenant, "sort-object", plans)
	if err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute sort-object tasks: %w", err)
	}
	resultEntries := make([]metastore.TableOfContentsEntry, len(objects))
	for i, obj := range objects {
		resultEntries[i] = metastore.TableOfContentsEntry{
			Path:      artifacts[i].Path,
			StartTime: time.Unix(0, obj.minTimestamp).UTC(),
			EndTime:   time.Unix(0, obj.maxTimestamp).UTC(),
		}
	}

	return c.replaceLogIndex(ctx, tenant, window, sourceIndex, resultEntries)
}

func logMergeTaskDetails(logger log.Logger, tasks []*compactionv2pb.TaskSpec) {
	// Only log the first 20 tasks
	tasksCnt := min(len(tasks), 20)
	for _, task := range tasks[:tasksCnt] {
		totalTaskSize := int64(0)
		sb := strings.Builder{}
		sb.WriteString("[")
		for i, run := range task.Runs {
			fmt.Fprintf(&sb, "%d", len(run.Sections))
			if i != len(task.Runs)-1 {
				sb.WriteString(", ")
			}
			for j := 0; j < len(run.Sections); j++ {
				totalTaskSize += run.Sections[j].UncompressedSize
			}
		}
		sb.WriteString("]")
		level.Debug(logger).Log("msg", "log compaction task snippet", "runs", len(task.Runs), "sections_per_run", sb.String(), "total_uncompressed_logs_size", totalTaskSize)
	}
}

// compactTenantIndexes performs one index-compaction pass for a tenant and window.
// Indexes are grouped by indexed log layout so an IndexMerge can never create
// an index containing incompatible sort metadata.
func (c *coordinator) compactTenantIndexes(ctx context.Context, tenant string, window time.Time, entries []indexEntry) (compactionStats, error) {
	groups := make(map[indexedLogLayout][]indexEntry)
	for _, entry := range entries {
		_, schemaLabels, shardCount, err := logSectionRefsFor(ctx, c.bucket, tenant, entry.Path)
		if err != nil {
			return compactionStats{}, fmt.Errorf("discover index section bounds: read index sort schema %s: %w", entry.Path, err)
		}
		key := indexedLogLayout{
			sortSchema: strings.Join(schemaLabels, ","),
			shardCount: shardCount,
		}
		groups[key] = append(groups[key], entry)
	}

	var total compactionStats
	for _, groupIndexEntries := range groups {
		stats, err := c.compactTenantIndexesGroup(ctx, tenant, window, groupIndexEntries)
		if err != nil {
			return total, err
		}
		total = total.Merge(stats)
	}
	return total, nil
}

func (c *coordinator) compactTenantIndexesGroup(ctx context.Context, tenant string, window time.Time, entries []indexEntry) (compactionStats, error) {
	windowLogger := log.With(c.logger, "tenant", tenant, "window", window)
	sections, err := indexSectionRefsFor(ctx, c.bucket, tenant, entries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("discover index section bounds: %w", err)
	}

	runs := v2.CalculateRuns(sections, compareIndexSortKey)
	inputRuns := len(runs)
	c.metrics.observeIndexInputRuns(inputRuns)
	converged := v2.IsConverged(sections, compareIndexSortKey)
	c.metrics.observeIndexConvergence(tenant, converged, inputRuns, entries, c.clock())
	if converged {
		level.Debug(windowLogger).Log("msg", "index-compaction: window converged, skipping", "input_runs", inputRuns)
		return compactionStats{}, nil
	}

	tasks := v2.Plan(runs, tenant, c.cfg.MaxRunsPerTask, nil)
	if len(tasks) == 0 {
		return compactionStats{}, fmt.Errorf("no index merge tasks to execute")
	}

	level.Info(windowLogger).Log("msg", "planned index compaction tasks", "tenant", tenant, "tasks", len(tasks), "input_runs", len(runs))
	logIndexTaskDetails(windowLogger, tasks)

	plans := make([]*physical.Plan, len(tasks))
	for i, task := range tasks {
		plans[i] = buildIndexMergePlan(tenant, window, task)
	}
	artifacts, err := c.indexDispatcher.Run(ctx, tenant, "index-merge", plans)
	if err != nil {
		return compactionStats{}, fmt.Errorf("execute index-compaction tasks: %w", err)
	}

	entriesByPath := make(map[string]indexEntry, len(entries))
	for _, entry := range entries {
		entriesByPath[entry.Path] = entry
	}
	newEntries := make([]metastore.TableOfContentsEntry, len(tasks))
	for i, task := range tasks {
		start, end, err := indexTaskBounds(task, entriesByPath)
		if err != nil {
			return compactionStats{}, fmt.Errorf("build index ToC entries: task %d: %w", i, err)
		}
		newEntries[i] = metastore.TableOfContentsEntry{
			Path:      artifacts[i].Path,
			StartTime: start.UTC(),
			EndTime:   end.UTC(),
		}
	}

	oldPaths := taskObjectPaths(tasks)

	swapped, err := c.publisher.Replace(ctx, tenant, window, oldPaths, newEntries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("replace index pointers after compaction: %w", err)
	}
	if !swapped {
		level.Debug(windowLogger).Log("msg", "index-compaction ToC replace race-loss")
		return compactionStats{}, nil
	}

	level.Info(windowLogger).Log("msg", "tenant cycle complete",
		"removed_indexes", len(oldPaths),
		"added_indexes", len(newEntries),
	)
	return compactionStats{
		removed:    len(oldPaths),
		added:      len(newEntries),
		dispatched: len(tasks),
	}, nil
}

func logIndexTaskDetails(logger log.Logger, tasks []*compactionv2pb.TaskSpec) {
	// Only log the first 20 tasks
	tasksCnt := min(len(tasks), 20)
	for _, task := range tasks[:tasksCnt] {
		sb := strings.Builder{}
		sb.WriteString("[")
		for i, run := range task.Runs {
			fmt.Fprintf(&sb, "%d", len(run.Sections))
			if i != len(task.Runs)-1 {
				sb.WriteString(", ")
			}
		}
		sb.WriteString("]")
		level.Debug(logger).Log("msg", "index compaction task snippet", "runs", len(task.Runs), "sections_per_run", sb.String())
	}
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

func taskObjectPaths(tasks []*compactionv2pb.TaskSpec) []string {
	seen := make(map[string]struct{})
	for _, task := range tasks {
		for _, run := range task.Runs {
			for _, section := range run.Sections {
				seen[section.ObjectPath] = struct{}{}
			}
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// indexTaskBounds returns the time range covered by the source indexes that
// task merges. inputsByPath maps each source index path to its ToC entry.
func indexTaskBounds(task *compactionv2pb.TaskSpec, inputsByPath map[string]indexEntry) (start, end time.Time, err error) {
	first := true
	for _, run := range task.Runs {
		for _, section := range run.Sections {
			input, ok := inputsByPath[section.ObjectPath]
			if !ok {
				return time.Time{}, time.Time{}, fmt.Errorf("task references unknown index %q", section.ObjectPath)
			}
			if first || input.Start.Before(start) {
				start = input.Start
			}
			if first || input.End.After(end) {
				end = input.End
			}
			first = false
		}
	}
	if first {
		return time.Time{}, time.Time{}, fmt.Errorf("task has no source indexes")
	}
	return start, end, nil
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

// phaseOutcome is the result of running one phase
type phaseOutcome int

const (
	phaseOutcomeError   phaseOutcome = iota // re-arm same phase
	phaseOutcomeNoWork                      // success, nothing to do
	phaseOutcomeSwapped                     // ToC swap applied/observed
)

// runIndexMergePhase runs IndexMerge for the tenant's current window and swaps
// the ToC.
func (c *coordinator) runIndexMergePhase(ctx context.Context, tenant string, window time.Time) phaseOutcome {
	start := c.clock()
	entries, ok := c.tenantEntries(ctx, tenant, window)
	if !ok {
		return phaseOutcomeError
	}

	c.metrics.observeEntries(tenant, entries)

	stats, err := c.compactTenantIndexes(ctx, tenant, window, entries)
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
	// compactTenantIndexes returns zero stats for every no-op success. A real
	// swap sets added > 0.
	if stats.added == 0 {
		c.metrics.observeTenantCycle(tenant, "converged", dur, compactionStats{})
		return phaseOutcomeNoWork
	}
	c.metrics.observeTenantCycle(tenant, "compacted", dur, stats)
	return phaseOutcomeSwapped
}

// tenantEntries reads the tenant's ToC for the window and returns its entries.
// A missing ToC yields (nil, true) — no work, not an error. Any other read
// error yields (nil, false).
func (c *coordinator) tenantEntries(ctx context.Context, tenant string, window time.Time) ([]indexEntry, bool) {
	entries, err := loadTenantIndexes(ctx, c.bucket, window, tenant)
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
	return entries, true
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

	level.Debug(c.logger).Log("msg", "log merge cycle begin", "tenant", tenant, "window", window, "index_entries_to_process", len(entries))
	var agg compactionStats
	anySwapped := false
	anyError := false
	for i, entry := range entries {
		if ctx.Err() != nil {
			return phaseOutcomeError
		}
		level.Debug(c.logger).Log("msg", "log merge cycle iteration", "tenant", tenant, "window", window, "index", entry.Path, "progress", fmt.Sprintf("%d/%d", i+1, len(entries)))
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
			agg = agg.Merge(stats)
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
// is cancelled. It never returns an error: on error it retries
// the same phase, otherwise it flips. It re-reads the per-tenant phase
// enablement each iteration and skips the LogMerge phase when log compaction is
// disabled, so an index-only tenant runs IndexMerge exclusively. Each phase runs
// against every window returned by c.windows(); the phase flips only when no
// window errored so a single failing window retries the whole phase. Between
// phases it waits at least MinBackoff; consecutive no-work or failing phases
// grow the wait exponentially up to MaxBackoff.
func (c *coordinator) runTenantLoop(ctx context.Context, tenant string) {
	p := phaseIndexMerge
	backoff := c.cfg.MinBackoff
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

		iterations := 1
		if p == phaseIndexMerge {
			iterations = indexMergeIterations
		}

		outcome := c.runMultiplePhasesForAllWindows(ctx, tenant, p, iterations)
		if ctx.Err() != nil {
			return
		}

		if outcome != phaseOutcomeError {
			p = p.flip()
		}

		var wait time.Duration
		wait, backoff = nextBackoff(outcome, backoff, c.cfg.MinBackoff, c.cfg.MaxBackoff)
		c.metrics.observeBackoff(wait)
		c.sleep(ctx, wait)
	}
}

// nextBackoff returns the wait after a phase and the backoff carried into the
// next iteration. Productive phases reset to the floor; no-work and error
// phases apply the current backoff and double it toward the ceiling.
func nextBackoff(outcome phaseOutcome, current, minWait, maxWait time.Duration) (wait, next time.Duration) {
	if outcome == phaseOutcomeSwapped {
		return minWait, minWait
	}
	next = current * 2
	if next <= 0 || next > maxWait {
		next = maxWait
	}
	return current, next
}

// runMultiplePhasesForAllWindows runs phase p for the tenant against each compacted window, iterations times,
// recording the worker-loop cycle metric per window, and returns the worst
// outcome across them. Error dominates (the caller re-arms the same phase);
// otherwise swapped (progress) outranks no-work. Windows are independent: a
// window with no ToC no-ops while a populated one does real work.
func (c *coordinator) runMultiplePhasesForAllWindows(ctx context.Context, tenant string, p phase, iterations int) phaseOutcome {
	worst := phaseOutcomeNoWork
	for range iterations {
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
			worst = worstOutcome(worst, outcome)
		}
	}
	return worst
}

// worstOutcome ranks phase outcomes so the tenant loop retries on any error and
// otherwise reports progress: error > swapped > no-work.
func worstOutcome(a, b phaseOutcome) phaseOutcome {
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
