package compactor

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
)

// Values of the outcome label of pairJobsTotal.
const (
	pairJobOutcomeCommitted = "committed"
	pairJobOutcomeNoop      = "noop"
	pairJobOutcomeStale     = "stale"
	pairJobOutcomeFailed    = "failed"
)

// pairCompactor runs the coupled compaction of one tenant.
//
// Every rescanInterval, it scans the windows of the tenant and plans jobs
// for the index runs that no planned job leases. Each job runs in its own
// goroutine. The task slots of the tenant admit the tasks of every job by
// priority, so the slots stay busy while any job has a task to run.
//
// A planned job keeps its place until it runs. Later scans plan every index
// run that no planned job leases. These are fresh ingest, the outputs of
// committed jobs, and index runs that earlier scans left unmerged. Only this
// tenant's compactor removes index runs from the ToC, so a planned job
// normally stays valid. The job still checks the ToC before its first task,
// and the swap fails if a source is gone.
type pairCompactor struct {
	tenant     string
	logger     log.Logger
	planner    *pairPlanner
	bucket     objstore.Bucket
	publisher  *tocPublisher
	dispatcher *planDispatcher
	limits     Limits
	metrics    *coordinatorMetrics

	// windows returns the ToC windows to scan, newest first.
	windows func() []time.Time
	clock   func() time.Time
	// sleep blocks for the given duration or until ctx is done.
	sleep          func(ctx context.Context, d time.Duration)
	rescanInterval time.Duration

	// maxIndexTaskSize is the maximum uncompressed postings size of one
	// IndexMerge task. 0 disables the split.
	maxIndexTaskSize uint64

	slots  *taskSlots
	leases *runLeases
	jobs   sync.WaitGroup

	// plannedJobs counts the jobs that start received. It sets the plan
	// order of each job. Only the scan goroutine uses it.
	plannedJobs uint64
}

// newPairPlanner returns the planner of the coupled planner, with the
// settings of c.
func (c *coordinator) newPairPlanner() *pairPlanner {
	return &pairPlanner{
		logger:            c.logger,
		bucket:            c.bucket,
		metrics:           c.metrics,
		strategy:          c.logMergePlanningStrategy,
		minCompactionSize: uint64(c.cfg.LogMinCompactionSize),
		maxLogTaskSize:    uint64(c.cfg.LogMaxTaskSize),
		maxKeySize:        uint64(c.cfg.LogMaxKeySize),
	}
}

// newPairCompactor returns the coupled compactor of tenant, with the settings
// and dependencies of c.
func (c *coordinator) newPairCompactor(tenant string) *pairCompactor {
	return &pairCompactor{
		tenant:           tenant,
		logger:           log.With(c.logger, "tenant", tenant, "planner", "pairs"),
		planner:          c.newPairPlanner(),
		bucket:           c.bucket,
		publisher:        c.publisher,
		dispatcher:       c.pairDispatcher,
		limits:           c.limits,
		metrics:          c.metrics,
		windows:          c.windows,
		clock:            c.clock,
		sleep:            c.sleep,
		rescanInterval:   c.cfg.RescanInterval,
		maxIndexTaskSize: uint64(c.cfg.MaxTaskSize),
		slots:            newTaskSlots(c.cfg.MaxRunningCompactionTasks),
		leases:           newRunLeases(),
	}
}

// run scans the tenant every rescanInterval until ctx is cancelled, or
// until every compaction phase of the tenant is disabled. It scans only while
// log compaction is enabled, because every merge rewrites log data. A tenant
// with only index compaction enabled gets no compaction.
//
// Jobs that did not start stop when ctx is cancelled, when log compaction is
// disabled, and when run returns. A job that started runs its tasks to
// completion and swaps the ToC. run returns after every job ends.
func (p *pairCompactor) run(ctx context.Context) {
	jobs := newJobsContext(ctx)
	defer func() {
		jobs.stop()
		p.jobs.Wait()
	}()

	var warnedIndexOnly bool
	for ctx.Err() == nil {
		runIndex, runLog := p.limits.CompactionPhases(p.tenant)
		if !runIndex && !runLog {
			return
		}
		if runLog {
			warnedIndexOnly = false
			p.scan(jobs.ctx)
		} else {
			jobs.restart()
			if !warnedIndexOnly {
				level.Warn(p.logger).Log("msg", "pair-compaction: tenant has index compaction enabled but log compaction disabled, so plan-version 2 does not compact it")
				warnedIndexOnly = true
			}
		}
		p.sleep(ctx, p.rescanInterval)
	}
}

// jobsContext is the context of the jobs that the compactor planned. Its
// cancellation stops the jobs that did not start.
type jobsContext struct {
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
}

func newJobsContext(parent context.Context) *jobsContext {
	ctx, cancel := context.WithCancel(parent)
	return &jobsContext{parent: parent, ctx: ctx, cancel: cancel}
}

// stop cancels the context.
func (j *jobsContext) stop() { j.cancel() }

// restart cancels the context and replaces it with a new one for the jobs
// that later scans plan.
func (j *jobsContext) restart() {
	j.cancel()
	j.ctx, j.cancel = context.WithCancel(j.parent)
}

// scan plans every window of the tenant, newest first, and starts the jobs it
// plans. It returns the number of jobs it started. A window that fails to plan
// does not stop the other windows.
//
// scan sets the last plan timestamp of the tenant only if it planned every
// index run of every window.
func (p *pairCompactor) scan(ctx context.Context) int {
	var (
		started  int
		complete = true
	)
	for lookback, window := range p.windows() {
		leased := p.leases.snapshot()
		plan, err := p.planner.planWindow(ctx, p.tenant, window, leased)
		if err != nil {
			complete = false
			if ctx.Err() == nil {
				level.Warn(p.logger).Log("msg", "pair-compaction: planning window failed", "window", window, "err", err)
			}
			continue
		}
		if plan.readFailures > 0 {
			complete = false
		}
		p.metrics.indexesPerTenantWindow.WithLabelValues(p.tenant, strconv.Itoa(lookback)).Set(float64(plan.indexRuns))
		for _, job := range plan.jobs {
			if p.start(ctx, job) {
				started++
			}
		}
	}
	if complete {
		p.metrics.lastPlanTimestampSeconds.WithLabelValues(p.tenant).Set(float64(p.clock().UnixNano()) / 1e9)
	}
	return started
}

// start leases the sources and log objects of job and runs job in a new
// goroutine. It returns false if a planned job already leases one of them.
func (p *pairCompactor) start(ctx context.Context, job pairJob) bool {
	lease := jobLease{indexes: job.replaces().sourcePaths(), logObjects: job.logObjects()}
	if !p.leases.tryAcquire(lease) {
		level.Warn(p.logger).Log("msg", "pair-compaction: skipping job, because another planned job leases one of its indexes or log objects", "job", job.kind())
		return false
	}
	planOrder := p.plannedJobs
	p.plannedJobs++
	p.jobs.Go(func() {
		// Release only after the swap. A scan takes its lease snapshot
		// before it reads the ToC. If the lease ended before the swap, a
		// scan could see the lease free and then read a ToC that still
		// lists the sources, so it would plan them a second time.
		defer p.leases.release(lease)
		p.runJob(ctx, job, planOrder)
	})
	return true
}

// runJob waits for the first task slot of job, checks that the ToC still lists
// its sources, runs it, and swaps the ToC. planOrder is the plan order of job.
//
// runJob records an outcome for each job that runs a task, or that finds its
// sources gone. A job that ctx stops before its first task records no
// outcome, because it did no work.
func (p *pairCompactor) runJob(ctx context.Context, job pairJob, planOrder uint64) {
	scope := job.replaces()
	logger := log.With(p.logger, "window", scope.window, "job", job.kind(), "sources", len(scope.sources))

	priority := job.priority()
	priority.planOrder = planOrder
	slots, err := p.slots.startJob(ctx, priority)
	if err != nil {
		return
	}
	defer slots.releaseHeld()

	paths := scope.sourcePaths()
	exists, err := existsInToC(ctx, p.bucket, p.tenant, scope.window, paths)
	if err != nil {
		if ctx.Err() == nil {
			p.jobFailed(logger, job, err)
		}
		return
	}
	if !exists {
		level.Info(logger).Log("msg", "pair-compaction: skipping job, because the ToC no longer lists all its indexes")
		p.metrics.pairJobsTotal.WithLabelValues(p.tenant, job.kind(), pairJobOutcomeStale).Inc()
		return
	}

	env := jobEnv{
		tenant:           p.tenant,
		logger:           logger,
		bucket:           p.bucket,
		dispatcher:       p.dispatcher,
		slots:            slots,
		maxIndexTaskSize: p.maxIndexTaskSize,
	}
	newRuns, tasks, err := job.execute(ctx, env)
	if err != nil {
		p.jobFailed(logger, job, err)
		return
	}

	// The swap must publish the finished tasks, even at shutdown. Without
	// the swap, the next scan runs the tasks again.
	swapped, err := p.publisher.Replace(context.WithoutCancel(ctx), p.tenant, scope.window, paths, newRuns)
	if err != nil {
		p.jobFailed(logger, job, err)
		return
	}
	if !swapped {
		level.Debug(logger).Log("msg", "pair-compaction: ToC swap wrote nothing, because of dry run or because the ToC already holds it")
		p.metrics.pairJobsTotal.WithLabelValues(p.tenant, job.kind(), pairJobOutcomeNoop).Inc()
		return
	}
	level.Info(logger).Log("msg", "pair-compaction: job committed", "removed_indexes", len(scope.sources), "added_indexes", len(newRuns), "added_index_objects", entryCount(newRuns), "tasks", tasks)
	p.metrics.pairJobsTotal.WithLabelValues(p.tenant, job.kind(), pairJobOutcomeCommitted).Inc()
	p.metrics.indexesRemovedTotal.WithLabelValues(p.tenant).Add(float64(len(scope.sources)))
	p.metrics.indexesAddedTotal.WithLabelValues(p.tenant).Add(float64(len(newRuns)))
	p.metrics.tasksTotal.WithLabelValues(p.tenant).Add(float64(tasks))
}

func (p *pairCompactor) jobFailed(logger log.Logger, job pairJob, err error) {
	sources := make([]string, len(job.replaces().sources))
	for i, run := range job.replaces().sources {
		sources[i] = run.String()
	}
	level.Warn(logger).Log("msg", "pair-compaction: job failed, so the next scan plans its indexes again", "indexes", strings.Join(sources, ","), "err", err)
	p.metrics.pairJobsTotal.WithLabelValues(p.tenant, job.kind(), pairJobOutcomeFailed).Inc()
}

// jobLease is what one planned job leases.
type jobLease struct {
	// indexes are the paths of the index objects that the job replaces.
	indexes []string

	// logObjects are the paths of the log objects that the job reads.
	logObjects []string
}

// leaseSnapshot is a copy of the leases of every planned job.
type leaseSnapshot struct {
	indexes    map[string]struct{}
	logObjects map[string]struct{}
}

func (s leaseSnapshot) holdsIndex(path string) bool {
	_, ok := s.indexes[path]
	return ok
}

func (s leaseSnapshot) holdsLogObject(path string) bool {
	_, ok := s.logObjects[path]
	return ok
}

// runLeases holds the index objects and log objects of the planned jobs. A
// job leases them until its swap ends, so no scan plans them again.
//
// runLeases is safe for concurrent use.
type runLeases struct {
	leasesMu   sync.Mutex
	indexes    map[string]struct{}
	logObjects map[string]struct{}
}

func newRunLeases() *runLeases {
	return &runLeases{indexes: make(map[string]struct{}), logObjects: make(map[string]struct{})}
}

// snapshot returns a copy of the leases.
func (l *runLeases) snapshot() leaseSnapshot {
	l.leasesMu.Lock()
	defer l.leasesMu.Unlock()
	return leaseSnapshot{indexes: maps.Clone(l.indexes), logObjects: maps.Clone(l.logObjects)}
}

// tryAcquire takes every path of lease and returns true, or takes none and
// returns false if any path is leased already.
func (l *runLeases) tryAcquire(lease jobLease) bool {
	l.leasesMu.Lock()
	defer l.leasesMu.Unlock()
	if slices.ContainsFunc(lease.indexes, func(path string) bool { _, ok := l.indexes[path]; return ok }) ||
		slices.ContainsFunc(lease.logObjects, func(path string) bool { _, ok := l.logObjects[path]; return ok }) {
		return false
	}
	for _, path := range lease.indexes {
		l.indexes[path] = struct{}{}
	}
	for _, path := range lease.logObjects {
		l.logObjects[path] = struct{}{}
	}
	return true
}

// release ends lease.
func (l *runLeases) release(lease jobLease) {
	l.leasesMu.Lock()
	defer l.leasesMu.Unlock()
	for _, path := range lease.indexes {
		delete(l.indexes, path)
	}
	for _, path := range lease.logObjects {
		delete(l.logObjects, path)
	}
}
