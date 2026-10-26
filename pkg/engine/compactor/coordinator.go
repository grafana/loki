package compactor

import (
	"context"
	"errors"
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

// errStalePlan marks a log compaction plan whose source index the ToC no
// longer lists.
var errStalePlan = errors.New("source index no longer exists in ToC")

// coordinator drives the per-tenant compaction workers. Each iteration
// re-reads the ToC and re-plans, so a crash recovers on the next pass.
type coordinator struct {
	cfg    Config
	logger log.Logger
	bucket objstore.Bucket
	// indexDispatcher runs IndexMerge plans. logDispatcher runs LogMerge and
	// IndexFilter plans. They have separate concurrency limits.
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
	// logMergePlanningStrategy decides when a log index needs compaction and
	// groups its runs into merge tasks by size level.
	logMergePlanningStrategy *v2.SizeLeveledStrategy
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
) (*coordinator, error) {
	logMergePlanningStrategy, err := v2.NewSizeLeveledStrategy(v2.DefaultSizeLevelBase, v2.DefaultSizeLevelRatio, cfg.LogMaxRunsPerTask)
	if err != nil {
		return nil, fmt.Errorf("log merge planning strategy: %w", err)
	}
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
		clock:                    time.Now,
		sleep:                    sleepUntil,
		metrics:                  newCoordinatorMetrics(reg),
		limits:                   limits,
		logMergePlanningStrategy: logMergePlanningStrategy,
	}, nil
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

// layoutKey identifies a stream layout as a map key. Two layouts have equal
// keys if and only if they are equal.
type layoutKey struct {
	schemaLabels string
	shardBuckets uint32
	streamOrder  streams.StreamOrder
}

func newLayoutKey(layout streams.Layout) layoutKey {
	return layoutKey{
		schemaLabels: strings.Join(layout.SchemaLabels(), ","),
		shardBuckets: layout.ShardLayout().Buckets(),
		streamOrder:  layout.StreamOrder(),
	}
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

// compactTenantLogs plans and then executes the log compaction of one index.
// Stats are zero-valued on any no-op.
func (c *coordinator) compactTenantLogs(
	ctx context.Context,
	tenant string,
	window time.Time,
	sourceIndex indexEntry,
) (compactionStats, error) {
	plan, err := c.planLogCompaction(ctx, tenant, window, sourceIndex)
	if err != nil {
		return compactionStats{}, err
	}
	return c.executeLogCompaction(ctx, tenant, plan)
}

// planLogCompaction decides how to compact the logs of one layout-homogeneous
// index. It reads the index but dispatches nothing and changes no state.
//
// It plans LogMerge tasks that keep the sort schema of the source index. An
// IndexFilter of the source index keeps the runs that have no merge partner.
// Compaction never changes the layout of an object.
//
// planLogCompaction returns a plan with no work if the index has no sections
// for tenant, is below the minimum compaction size, or needs no compaction.
func (c *coordinator) planLogCompaction(
	ctx context.Context,
	tenant string,
	window time.Time,
	sourceIndex indexEntry,
) (logCompactionPlan, error) {
	sections, err := logSectionRefsFor(ctx, c.bucket, tenant, sourceIndex.Path)
	if err != nil {
		return logCompactionPlan{}, fmt.Errorf("reading log section refs: %w", err)
	}
	if len(sections) == 0 {
		return logCompactionPlan{}, nil
	}
	return c.planLogMerge(tenant, window, sourceIndex, sections)
}

// planLogMerge plans the k-way merge of sections, which must share a sort
// schema.
func (c *coordinator) planLogMerge(
	tenant string,
	window time.Time,
	sourceIndex indexEntry,
	sections []v2.Section[logSortPrefix],
) (logCompactionPlan, error) {
	entryLogger := log.With(c.logger, "tenant", tenant, "entry", sourceIndex.Path)
	runs := v2.CalculateRuns(sections, compareLogSortPrefix)
	if v2.BelowMinCompactionSize(runs, uint64(c.cfg.LogMinCompactionSize)) ||
		!c.logMergePlanningStrategy.NeedsCompaction(runs) {
		level.Debug(entryLogger).Log("msg", "log-compaction: window not worth compacting, skipping", "window", window)
		return logCompactionPlan{}, nil
	}

	merges, unmerged := c.logMergePlanningStrategy.Plan(runs, tenant)
	if len(merges) == 0 {
		return logCompactionPlan{}, fmt.Errorf("no log merge tasks to execute for source index %q: %d runs, %d unmerged", sourceIndex.Path, len(runs), len(unmerged))
	}

	splitter := newLogTaskSplitter(entryLogger, uint64(c.cfg.LogMaxTaskSize), sections)
	var (
		specs  []*compactionv2pb.TaskSpec
		groups []logMergeGroup
	)
	for _, merge := range merges {
		tasks := splitter.split(merge)
		groups = append(groups, newLogMergeGroup(merge, len(specs), len(tasks)))
		specs = append(specs, tasks...)
	}

	level.Info(entryLogger).Log("msg", "planned log compaction tasks", "input_runs", len(runs), "runs_per_level", fmt.Sprint(c.logMergePlanningStrategy.RunsPerLevel(runs)), "merges", len(merges), "tasks", len(specs), "unmerged_runs", len(unmerged))
	logMergeTaskDetails(entryLogger, specs)

	plan := logCompactionPlan{
		window:      window,
		sourceIndex: sourceIndex,
		tasks:       make([]logCompactionTask, 0, len(specs)+1),
		merges:      groups,
	}
	for _, spec := range specs {
		plan.tasks = append(plan.tasks, newLogMergeTask(spec))
	}
	if len(unmerged) > 0 {
		plan.tasks = append(plan.tasks, indexFilterForUnmerged(sourceIndex, unmerged))
	}
	return plan, nil
}

// executeLogCompaction dispatches the tasks of plan and swaps its source index
// for the results. Stats are zero-valued if plan has no work or the swap is a
// no-op.
//
// It reads the latest ToC of the window first. It returns an error that wraps
// errStalePlan if the ToC no longer lists the source index.
func (c *coordinator) executeLogCompaction(
	ctx context.Context,
	tenant string,
	plan logCompactionPlan,
) (compactionStats, error) {
	if !plan.hasWork() {
		return compactionStats{}, nil
	}

	// A swap leaves the old index object in storage, so only the ToC shows
	// that the plan is stale. A swap by another writer while the tasks run is
	// still safe, because our swap then fails and writes nothing.
	exists, err := c.existsInToC(ctx, tenant, plan.window, plan.sourceIndex.Path)
	if err != nil {
		return compactionStats{}, fmt.Errorf("check source index %s exists in ToC: %w", plan.sourceIndex.Path, err)
	}
	if !exists {
		return compactionStats{}, fmt.Errorf("%w: %s", errStalePlan, plan.sourceIndex.Path)
	}

	artifacts, err := c.logDispatcher.Run(ctx, tenant, plan.physicalPlans(tenant))
	if err != nil {
		return compactionStats{}, fmt.Errorf("failed to execute log compaction tasks: %w", err)
	}
	if err := plan.checkRecordCounts(artifacts); err != nil {
		return compactionStats{}, fmt.Errorf("check log compaction record counts for source index %s: %w", plan.sourceIndex.Path, err)
	}
	entries := plan.tocEntries()
	if err := assignArtifactPaths(entries, artifacts); err != nil {
		return compactionStats{}, fmt.Errorf("build log ToC entries: %w", err)
	}

	stats, err := c.replaceLogIndex(ctx, tenant, plan.window, plan.sourceIndex, entries)
	if err != nil {
		return compactionStats{}, err
	}
	if stats.removed == 0 {
		level.Debug(c.logger).Log("msg", "log-compaction step for index was a no-op", "tenant", tenant, "entry", plan.sourceIndex.Path)
	} else {
		level.Debug(c.logger).Log("msg", "log-compaction step completed for index", "tenant", tenant, "entry", plan.sourceIndex.Path, "index_files_added", stats.added, "index_files_removed", stats.removed, "tasks_dispatched", stats.dispatched)
	}
	return stats, nil
}

// existsInToC reports whether the latest ToC of the tenant's window lists
// the index at path. A missing ToC is an error, because planning read it.
func (c *coordinator) existsInToC(ctx context.Context, tenant string, window time.Time, path string) (bool, error) {
	entries, err := loadTenantIndexes(ctx, c.bucket, window, tenant)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Path == path {
			return true, nil
		}
	}
	return false, nil
}

// indexFilterForUnmerged returns an IndexFilter task that keeps only the
// unmerged runs from sourceIndex. Its ToC entry spans the unmerged runs.
func indexFilterForUnmerged(sourceIndex indexEntry, unmerged []v2.Run) indexFilterTask {
	runRefs := make([]*compactionv2pb.RunRef, len(unmerged))
	var objectPaths []string
	for i, run := range unmerged {
		runRefs[i] = &compactionv2pb.RunRef{Sections: run.Sections()}
		for _, section := range run.Sections() {
			objectPaths = append(objectPaths, section.ObjectPath)
		}
	}
	slices.Sort(objectPaths)
	objectPaths = slices.Compact(objectPaths)
	return indexFilterTask{
		sourceIndexPath: sourceIndex.Path,
		objectPaths:     objectPaths,
		entry:           runsToCEntry(runRefs),
	}
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
// Indexes are grouped by stream layout, so indexes in different groups never
// merge. It skips corrupt indexes, so they do not block the other indexes of
// the tenant.
func (c *coordinator) compactTenantIndexes(ctx context.Context, tenant string, window time.Time, entries []indexEntry) (compactionStats, error) {
	groups := make(map[layoutKey][]indexEntry)
	for _, entry := range entries {
		layout, ok, err := indexLayoutAt(ctx, c.bucket, tenant, entry.Path)
		if errors.Is(err, errCorruptIndex) {
			c.skipCorruptIndex(tenant, window, entry.Path, phaseLabelIndexMerge, err)
			continue
		}
		if err != nil {
			return compactionStats{}, fmt.Errorf("group indexes by layout: %w", err)
		}
		// An index without stats sections for tenant has no layout. Such
		// indexes share the zero key, so they merge only with each other.
		var key layoutKey
		if ok {
			key = newLayoutKey(layout)
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

// skipCorruptIndex records that phase skipped the corrupt index at path.
func (c *coordinator) skipCorruptIndex(tenant string, window time.Time, path, phase string, err error) {
	level.Error(c.logger).Log("msg", "skipping corrupt index",
		"tenant", tenant, "window", window, "index", path, "phase", phase, "err", err)
	c.metrics.corruptIndexesTotal.WithLabelValues(tenant, phase).Inc()
}

func (c *coordinator) compactTenantIndexesGroup(ctx context.Context, tenant string, window time.Time, entries []indexEntry) (compactionStats, error) {
	windowLogger := log.With(c.logger, "tenant", tenant, "window", window)
	sections, err := indexSectionRefsFor(ctx, c.bucket, tenant, entries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("discover index section bounds: %w", err)
	}

	runs := v2.CalculateRuns(sections, compareIndexSortKey)
	inputRuns := len(runs)
	c.metrics.indexInputRuns.Observe(float64(inputRuns))
	converged := v2.IsConverged(sections, compareIndexSortKey)
	c.metrics.observeIndexConvergence(tenant, converged, inputRuns, entries, c.clock())
	if converged {
		level.Debug(windowLogger).Log("msg", "index-compaction: window converged, skipping", "input_runs", inputRuns)
		return compactionStats{}, nil
	}

	tasks := v2.Plan(runs, tenant, c.cfg.MaxRunsPerTask)
	if len(tasks) == 0 {
		return compactionStats{}, fmt.Errorf("no index merge tasks to execute")
	}

	level.Info(windowLogger).Log("msg", "planned index compaction tasks", "tenant", tenant, "tasks", len(tasks), "input_runs", len(runs))
	logIndexTaskDetails(windowLogger, tasks)

	entriesByPath := make(map[string]indexEntry, len(entries))
	for _, entry := range entries {
		entriesByPath[entry.Path] = entry
	}
	plans := make([]*physical.Plan, len(tasks))
	newEntries := make([]metastore.TableOfContentsEntry, len(tasks))
	for i, task := range tasks {
		start, end, err := indexTaskBounds(task, entriesByPath)
		if err != nil {
			return compactionStats{}, fmt.Errorf("build index ToC entries: task %d: %w", i, err)
		}
		plans[i] = buildIndexMergePlan(tenant, window, task)
		newEntries[i] = metastore.TableOfContentsEntry{
			StartTime: start.UTC(),
			EndTime:   end.UTC(),
		}
	}
	artifacts, err := c.indexDispatcher.Run(ctx, tenant, plans)
	if err != nil {
		return compactionStats{}, fmt.Errorf("execute index-compaction tasks: %w", err)
	}
	if err := assignArtifactPaths(newEntries, artifacts); err != nil {
		return compactionStats{}, fmt.Errorf("build index ToC entries: %w", err)
	}

	oldPaths := taskObjectPaths(tasks)

	swapped, err := c.publisher.Replace(ctx, tenant, window, oldPaths, newEntries)
	if err != nil {
		return compactionStats{}, fmt.Errorf("replace index pointers after compaction: %w", err)
	}
	if !swapped {
		level.Debug(windowLogger).Log("msg", "index-compaction ToC replace wrote nothing")
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

// runsToCEntry returns a ToC entry without a path. Its time range spans from
// the earliest section start to the latest section end across runs.
func runsToCEntry(runs []*compactionv2pb.RunRef) metastore.TableOfContentsEntry {
	var minTS, maxTS int64
	first := true
	for _, run := range runs {
		for _, sec := range run.Sections {
			if first {
				minTS, maxTS, first = sec.MinTimestamp, sec.MaxTimestamp, false
				continue
			}
			minTS = min(minTS, sec.MinTimestamp)
			maxTS = max(maxTS, sec.MaxTimestamp)
		}
	}
	return metastore.TableOfContentsEntry{
		StartTime: time.Unix(0, minTS).UTC(),
		EndTime:   time.Unix(0, maxTS).UTC(),
	}
}

// assignArtifactPaths sets the path of each entry to the path of the artifact
// at the same index. entries and artifacts must both be in plan order.
//
// A length mismatch returns an error and leaves entries unchanged. It stops a
// ToC swap that would drop a result or reference the wrong object.
func assignArtifactPaths(entries []metastore.TableOfContentsEntry, artifacts []v2.ResultArtifact) error {
	if len(entries) != len(artifacts) {
		return fmt.Errorf("got %d artifacts for %d ToC entries", len(artifacts), len(entries))
	}
	for i := range entries {
		entries[i].Path = artifacts[i].Path
	}
	return nil
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
	phaseOutcomeError   phaseOutcome = iota // a window or index failed
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

	c.metrics.indexesPerTenantWindow.WithLabelValues(tenant).Set(float64(len(entries)))

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

// runLogMergePhase plans the log compaction of every index in every window for
// the tenant, then executes the plans. An error does not lose work, because
// the next LogMerge phase plans again. Running a plan again is safe, because
// swapping an index that already swapped is a no-op.
//
// All planning finishes before any task runs, so the pending log bytes gauge
// shows the whole phase. The last plan timestamp moves only when every index
// planned without an error, so a partial total is visible as stale.
//
// The phase holds every plan in memory to avoid a second read of each index.
// Its memory grows with the number of windows and indexes. A held plan stays
// safe to execute because index objects are written once. A plan fails before
// its tasks run if the ToC no longer lists its source index. If another writer
// replaces the source index while the tasks run, the swap fails and the work
// is not used.
func (c *coordinator) runLogMergePhase(ctx context.Context, tenant string) phaseOutcome {
	start := c.clock()
	plans, anyError := c.planLogCompactions(ctx, tenant)
	if ctx.Err() != nil {
		return phaseOutcomeError
	}

	planTime := c.clock()
	pending, pendingBytes := startPendingLogBytes(c.metrics.pendingLogBytes, tenant, newLogAgeBuckets(c.cfg.WindowLookback), planTime, plans)
	if !anyError {
		c.metrics.lastPlanTimestampSeconds.WithLabelValues(tenant).Set(float64(planTime.UnixNano()) / 1e9)
	}

	level.Debug(c.logger).Log("msg", "log merge cycle begin", "tenant", tenant, "plans", len(plans), "pending_bytes", pendingBytes)
	var agg compactionStats
	anySwapped := false
	for i, plan := range plans {
		if ctx.Err() != nil {
			return phaseOutcomeError
		}
		level.Debug(c.logger).Log("msg", "log merge cycle iteration", "tenant", tenant, "window", plan.window, "index", plan.sourceIndex.Path, "progress", fmt.Sprintf("%d/%d", i+1, len(plans)))
		stats, err := c.executeLogCompaction(ctx, tenant, plan)
		if err != nil {
			// Return only when the coordinator ctx is done. Treat any other
			// error as a failure of this index, including a swap timeout, so
			// the remaining plans still run.
			if ctx.Err() != nil {
				return phaseOutcomeError
			}
			level.Warn(c.logger).Log("msg", "log-merge phase: index failed",
				"tenant", tenant, "window", plan.window, "index", plan.sourceIndex.Path, "err", err)
			reason := failReasonExecute
			if errors.Is(err, errStalePlan) {
				reason = failReasonStalePlan
			}
			c.metrics.logMergeFailedIndexesTotal.WithLabelValues(tenant, reason).Inc()
			anyError = true
			continue
		}
		// A no-op swap also removes the plan's bytes. It happens in dry-run
		// mode, or when an earlier attempt wrote the swap and lost the
		// response. In both cases this phase does not rewrite those bytes.
		pending.complete(plan)
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

// planLogCompactions plans every index in every window for the tenant. It
// returns only the plans with work, newest window first.
//
// anyError reports whether a ToC or an index failed to load or plan, or ctx
// was done. The plans for the other indexes are still returned, so one bad
// index does not block the rest. A corrupt index does not set anyError,
// because no retry can fix it. corruptIndexesTotal records it instead.
func (c *coordinator) planLogCompactions(ctx context.Context, tenant string) (plans []logCompactionPlan, anyError bool) {
	for _, window := range c.windows() {
		entries, ok := c.tenantEntries(ctx, tenant, window)
		if !ok {
			anyError = true
			continue
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return plans, true
			}
			plan, err := c.planLogCompaction(ctx, tenant, window, entry)
			if errors.Is(err, errCorruptIndex) {
				c.skipCorruptIndex(tenant, window, entry.Path, phaseLabelLogMerge, err)
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return plans, true
				}
				level.Warn(c.logger).Log("msg", "log-merge phase: planning index failed",
					"tenant", tenant, "window", window, "index", entry.Path, "err", err)
				c.metrics.logMergeFailedIndexesTotal.WithLabelValues(tenant, failReasonPlan).Inc()
				anyError = true
				continue
			}
			if plan.hasWork() {
				plans = append(plans, plan)
			}
		}
	}
	return plans, anyError
}

// runTenantLoop alternates IndexMerge and LogMerge for tenant until ctx is
// cancelled or both phases are disabled.
//
// Each phase runs against every window in c.windows(). An IndexMerge phase
// with an error runs again. A LogMerge phase always flips to IndexMerge.
//
// The loop reads the phase limits of the tenant on each iteration. It skips
// LogMerge when log compaction is disabled, so an index-only tenant runs only
// IndexMerge.
//
// Between phases the loop waits at least MinBackoff. Each phase in a row with
// no work or an error doubles the wait, up to MaxBackoff.
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

		outcome := c.runMultiplePhases(ctx, tenant, p, iterations)
		if ctx.Err() != nil {
			return
		}

		// A LogMerge error can repeat for a long time. Retrying it at once
		// would block IndexMerge, so unmerged indexes would pile up.
		if p == phaseLogMerge || outcome != phaseOutcomeError {
			p = p.flip()
		}

		var wait time.Duration
		wait, backoff = nextBackoff(outcome, backoff, c.cfg.MinBackoff, c.cfg.MaxBackoff)
		c.metrics.cycleBackoffSeconds.Observe(wait.Seconds())
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

// runMultiplePhases runs phase p for the tenant iterations times and returns
// the worst outcome. Error outranks swapped, and swapped outranks no-work.
func (c *coordinator) runMultiplePhases(ctx context.Context, tenant string, p phase, iterations int) phaseOutcome {
	worst := phaseOutcomeNoWork
	for range iterations {
		if ctx.Err() != nil {
			return worst
		}
		worst = worstOutcome(worst, c.runPhase(ctx, tenant, p))
	}
	return worst
}

// runPhase runs phase p once for the tenant across every compacted window and
// returns the worst outcome.
//
// IndexMerge runs each window separately and records one cycle per window.
// LogMerge covers all windows in one pass and records one cycle.
func (c *coordinator) runPhase(ctx context.Context, tenant string, p phase) phaseOutcome {
	switch p {
	case phaseLogMerge:
		start := c.clock()
		outcome := c.runLogMergePhase(ctx, tenant)
		c.metrics.observeCycle(cycleOutcome(outcome), c.clock().Sub(start))
		return outcome
	case phaseIndexMerge:
		worst := phaseOutcomeNoWork
		for _, window := range c.windows() {
			if ctx.Err() != nil {
				return worst
			}
			start := c.clock()
			outcome := c.runIndexMergePhase(ctx, tenant, window)
			c.metrics.observeCycle(cycleOutcome(outcome), c.clock().Sub(start))
			worst = worstOutcome(worst, outcome)
		}
		return worst
	default:
		return phaseOutcomeError
	}
}

// worstOutcome returns the more severe of a and b, in the order
// error > swapped > no-work.
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

// cycleOutcome maps a phaseOutcome to a cyclesTotal outcome label:
// compacted, converged, or failed.
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
