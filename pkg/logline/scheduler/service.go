package scheduler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v4/pkg/logline/limits"
	schedulerapi "github.com/grafana/loki/v4/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v4/pkg/logline/store"
)

// Service implements the dskit Service interface for the compaction scheduler.
// It polls object storage for active indexes, groups them into compaction jobs,
// and hands jobs to workers via long-poll HTTP.
type Service struct {
	services.Service

	cfg    Config
	limits limits.Limits
	store  *store.Store
	logger log.Logger

	// longPollTimeout is schedulerapi.LongPollTimeout. Tests shorten it.
	longPollTimeout time.Duration

	// mu guards all mutable state: jobs, planner, completedSources, workCh.
	mu      sync.Mutex
	jobs    *jobStore
	planner CompactionPlanner
	metrics *Metrics

	// completedSources tracks source IDs from recently-completed jobs that
	// haven't yet been confirmed as compacted by the store snapshot. Without
	// this, the planner would re-group these sources between store polls
	// (causing redundant compaction of the same indexes).
	completedSources map[string]struct{}

	// workCh is closed when new work might be available. Workers waiting in
	// long poll select on this channel. It is replaced (not reused) after each
	// notification to avoid spurious wakeups from stale channels.
	workCh chan struct{}
}

// New creates a new scheduler Service. The jobStore is not created here — it
// is created in starting() so that disk errors surface as startup failures
// rather than construction errors.
func New(indexStore *store.Store, lim limits.Limits, cfg Config, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	s := &Service{
		cfg:              cfg,
		limits:           lim,
		store:            indexStore,
		logger:           logger,
		longPollTimeout:  schedulerapi.LongPollTimeout,
		metrics:          NewMetrics(reg),
		planner:          newOverlapPlanner(nil, int64(cfg.Compaction.MaxJobSize)),
		completedSources: make(map[string]struct{}),
		workCh:           make(chan struct{}),
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)

	return s, nil
}

func (s *Service) starting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "scheduler service starting")

	// Create the job store (loads persisted state from disk).
	js, err := newJobStore(s.cfg.Compaction.JobStateDir, s.logger)
	if err != nil {
		return err
	}
	s.jobs = js

	// Log a summary of each reloaded job so operators can see what
	// was in-flight when the scheduler last stopped.
	for _, j := range s.jobs.all() {
		level.Info(s.logger).Log(
			"msg", "reloaded job",
			"job_id", j.ID,
			"worker_id", j.WorkerID,
			"source_ids", strings.Join(j.SourceIDs, ", "),
			"created_at", j.CreatedAt.Format(time.RFC3339),
			"expires_at", j.ExpiresAt.Format(time.RFC3339),
		)
	}

	// Start the object storage polling loop so the snapshot is populated.
	if err := s.store.StartPolling(ctx); err != nil {
		return err
	}

	// Remove any jobs that expired while the scheduler was offline.
	expired, err := s.jobs.removeExpired(time.Now())
	if err != nil {
		return err
	}
	for _, j := range expired {
		level.Warn(s.logger).Log(
			"msg", "removed expired job found on startup",
			"job_id", j.ID,
			"worker_id", j.WorkerID,
			"expired_at", j.ExpiresAt,
		)
		s.metrics.jobsCompletedTotal.WithLabelValues("expiration").Inc()
	}

	// Prime the planner with the current snapshot.
	s.tick(s.store.Snapshot())

	return nil
}

func (s *Service) running(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "scheduler service running")

	// The delete goroutine consumes from a coalescing channel of size 1: if
	// it is still working when a new snapshot arrives, the pending snapshot
	// is replaced rather than queued. Only the freshest snapshot matters for
	// retention/grace decisions, so dropping intermediate ones is correct.
	deleteCh := make(chan *store.Snapshot, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go s.runDeleteLoop(ctx, deleteCh, &wg)

	defer func() {
		close(deleteCh)
		wg.Wait()
	}()

	for {
		select {
		case snap, ok := <-s.store.PollNotify():
			if !ok {
				return nil // store shut down
			}
			s.tick(snap)
			s.enqueueDelete(deleteCh, snap)
		case <-ctx.Done():
			return nil
		}
	}
}

// runDeleteLoop drains snapshots from ch and runs a delete cycle for each.
// Exits when ch is closed by running().
func (s *Service) runDeleteLoop(ctx context.Context, ch <-chan *store.Snapshot, wg *sync.WaitGroup) {
	defer wg.Done()
	for snap := range ch {
		s.deleteEligibleIndexes(ctx, snap)
	}
}

// enqueueDelete hands the snapshot to the delete goroutine, replacing any
// pending (older) snapshot.
func (s *Service) enqueueDelete(ch chan *store.Snapshot, snap *store.Snapshot) {
	select {
	case ch <- snap:
		return
	default:
	}

	// Buffer full: a delete is in flight and an older snapshot is already
	// queued. Drop the stale one and queue the fresher snapshot instead.
	select {
	case <-ch: // Read the old enqueued snapshot
	default:
	}
	ch <- snap
}

func (s *Service) stopping(err error) error {
	level.Info(s.logger).Log("msg", "scheduler service stopping", "err", err)
	return nil
}

// tick runs one scheduler cycle: expire stale jobs, detect completions,
// rebuild the compaction plan, and wake waiting workers.
func (s *Service) tick(snap *store.Snapshot) {
	start := time.Now()
	defer func() {
		s.metrics.tickDuration.Observe(time.Since(start).Seconds())
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Expire jobs whose workers failed to complete in time.
	expired, err := s.jobs.removeExpired(time.Now())
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to remove expired jobs", "err", err)
	}
	for _, j := range expired {
		level.Warn(s.logger).Log(
			"msg", "job completion through expiration",
			"job_id", j.ID,
			"worker_id", j.WorkerID,
		)
		s.metrics.jobsCompletedTotal.WithLabelValues("expiration").Inc()
	}

	s.resolveCompletedWorkLocked(snap)
	s.rebuildPlannerLocked(snap)

	// Update gauges.
	s.metrics.jobsInFlight.Set(float64(len(s.jobs.jobs)))

	// Wake waiting workers.
	s.notifyWorkersLocked()
}

// resolveCompletedWork takes the lock and delegates to resolveCompletedWorkLocked.
func (s *Service) resolveCompletedWork(snap *store.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveCompletedWorkLocked(snap)
}

// resolveCompletedWorkLocked reconciles in-memory tracking state against the
// snapshot. It removes jobs whose sources have all been resolved (compacted
// or absent) and prunes the completedSources cache for entries the snapshot
// has confirmed. Generally jobs are marked completed in the handler when a
// worker asks for a new job. This is a second check in case the update from
// the worker failed.
//
// Caller must hold s.mu.
func (s *Service) resolveCompletedWorkLocked(snap *store.Snapshot) {
	// Detect jobs completed through snapshot state.
	for _, j := range s.jobs.all() {
		allResolved := true
		for _, srcID := range j.SourceIDs {
			if !isResolved(snap, srcID) {
				allResolved = false
				break
			}
		}
		if !allResolved {
			continue
		}

		if err := s.jobs.remove(j.ID); err != nil {
			level.Error(s.logger).Log("msg", "failed to remove completed job", "job_id", j.ID, "err", err)
			continue
		}

		level.Info(s.logger).Log("msg", "job completion through state change", "job_id", j.ID, "worker_id", j.WorkerID)
		s.metrics.jobsCompletedTotal.WithLabelValues("state_change").Inc()
	}

	// Prune completed sources the snapshot has confirmed as resolved.
	for id := range s.completedSources {
		if isResolved(snap, id) {
			delete(s.completedSources, id)
		}
	}
}

// rebuildPlanner takes the lock and delegates to rebuildPlannerLocked.
func (s *Service) rebuildPlanner(snap *store.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuildPlannerLocked(snap)
}

// rebuildPlannerLocked uses the snapshot's active indexes, excludes sources
// that are in-flight or recently completed, and installs a fresh planner.
// Excluding recently-completed sources prevents redundant re-compaction
// between store polls (the snapshot may not yet reflect the merged index).
//
// Caller must hold s.mu.
func (s *Service) rebuildPlannerLocked(snap *store.Snapshot) {
	active := snap.Active()
	inFlight := s.jobs.inFlightSourceIDs()

	available := active[:0]
	for _, m := range active {
		id := m.ID()
		if _, ok := inFlight[id]; ok {
			continue
		}
		if _, ok := s.completedSources[id]; ok {
			continue
		}
		available = append(available, m)
	}

	s.metrics.availableIndexes.Set(float64(len(available)))

	planner := newOverlapPlanner(available, int64(s.cfg.Compaction.MaxJobSize))

	if op, ok := planner.(*overlapPlanner); ok && op.skipped > 0 {
		level.Warn(s.logger).Log(
			"msg", "skipped unmergeable indexes",
			"skipped", op.skipped,
			"max_job_size", s.cfg.Compaction.MaxJobSize.String(),
		)
		s.metrics.jobsSkippedOversized.Set(float64(op.skipped))
	} else {
		s.metrics.jobsSkippedOversized.Set(0)
	}

	s.planner = planner

	// Seed the pending-jobs counter from the freshly-built planner. Each
	// successful assignJob decrements it; rebuilds reset it.
	s.metrics.pendingJobs.Set(float64(len(planner.Groups())))
}

// notifyWorkers wakes all workers currently blocked in long poll by closing
// the current workCh and replacing it.
func (s *Service) notifyWorkers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.workCh)
	s.workCh = make(chan struct{})
}

func (s *Service) notifyWorkersLocked() {
	close(s.workCh)
	s.workCh = make(chan struct{})
}

// deleteEligibleIndexes removes indexes that have exceeded their retention
// or whose compacted sources have aged past the grace period.
func (s *Service) deleteEligibleIndexes(ctx context.Context, snap *store.Snapshot) {
	now := time.Now()

	retentionDuration := s.limits.RetentionPeriod()

	// Guard against passing a zero cutoff to EligibleForDeletion, which would
	// mark all active indexes as eligible (catastrophic data loss).
	var retentionCutoff time.Time
	if retentionDuration > 0 {
		retentionCutoff = now.Add(-retentionDuration)
	}
	graceCutoff := now.Add(-s.cfg.Retention.CompactedSourceDeleteDelay)

	eligible := snap.EligibleForDeletion(retentionCutoff, graceCutoff)

	g := errgroup.Group{}

	concurrency := max(s.cfg.Retention.DeleteConcurrency, 1)
	g.SetLimit(concurrency)

	start := time.Now()
	for _, m := range eligible {
		// Stop dispatching new deletes when the service is shutting down. Any
		// goroutines already running will see the cancelled ctx propagate
		// through DeleteIndex and return promptly.
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			if err := s.store.DeleteIndex(ctx, m); err != nil {
				level.Error(s.logger).Log("msg", "failed to delete index", "id", m.ID(), "err", err)
				s.metrics.indexesDeleteFailuresTotal.Inc()
				return nil
			}
			level.Info(s.logger).Log("msg", "deleted index", "id", m.ID())
			s.metrics.indexesDeletedTotal.Inc()
			return nil
		})
	}

	_ = g.Wait()

	level.Info(s.logger).Log(
		"msg", "eligible indexes deleted",
		"count", len(eligible),
		"concurrency", concurrency,
		"duration", time.Since(start),
	)
}

// prepareWorker handles fast-path completion and checks for an existing job.
// Returns the existing job to re-deliver, or nil if the worker is free.
func (s *Service) prepareWorker(workerID string, r *http.Request) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Fast-path completion: the worker reports the job it just finished so
	// the scheduler can remove it immediately instead of waiting for the
	// next tick to detect it via the store snapshot.
	if lastJobID := schedulerapi.LastCompletedJobID(r); lastJobID != "" {
		if err := s.completeReportedJobLocked(workerID, lastJobID); err != nil && !errors.Is(err, errJobOwnedByOtherWorker) {
			level.Error(s.logger).Log("msg", "failed to remove worker-reported completed job", "job_id", lastJobID, "err", err)
		}
	}

	// If this worker already has a job, re-deliver it. This handles retries
	// from workers that crashed before receiving the response.
	return s.jobs.getByWorker(workerID)
}

// errJobOwnedByOtherWorker is returned when a worker reports a job that is
// assigned to a different worker.
var errJobOwnedByOtherWorker = errors.New("job belongs to a different worker")

// completeReportedJobLocked removes a job its worker reports as finished and
// marks its sources completed, so the planner skips them until a store poll
// sees the merged index. A job that no longer exists is not an error: workers
// resend a report until one succeeds.
//
// Caller must hold s.mu.
func (s *Service) completeReportedJobLocked(workerID, jobID string) error {
	job := s.jobs.get(jobID)
	if job == nil {
		return nil
	}
	if job.WorkerID != workerID {
		return errJobOwnedByOtherWorker
	}
	if err := s.jobs.remove(jobID); err != nil {
		return err
	}
	for _, id := range job.SourceIDs {
		s.completedSources[id] = struct{}{}
	}
	level.Info(s.logger).Log("msg", "job completion through worker report", "job_id", jobID, "worker_id", workerID)
	s.metrics.jobsCompletedTotal.WithLabelValues("worker_report").Inc()
	s.metrics.jobsInFlight.Set(float64(len(s.jobs.jobs)))
	return nil
}

// waitForWork returns the current notification channel. Callers select on it;
// when closed, new work may be available.
func (s *Service) waitForWork() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.workCh
}

// assignJob tries to plan and assign a compaction job to the given worker.
// Returns nil if no work is currently available. Persists the job to disk
// before returning — if persistence fails the job is not returned.
func (s *Service) assignJob(workerID string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	sourceIDs := s.planner.Plan()
	if sourceIDs == nil {
		return nil
	}

	job := &Job{
		ID:        jobID(sourceIDs),
		SourceIDs: sourceIDs,
		WorkerID:  workerID,
		ExpiresAt: time.Now().Add(s.cfg.Compaction.JobTimeout),
		CreatedAt: time.Now(),
	}

	if err := s.jobs.add(job); err != nil {
		level.Error(s.logger).Log(
			"msg", "failed to persist job assignment, not returning job to worker",
			"job_id", job.ID,
			"worker_id", workerID,
			"err", err,
		)
		return nil
	}

	s.metrics.jobsCreatedTotal.Inc()
	s.metrics.jobsInFlight.Set(float64(len(s.jobs.jobs)))
	s.metrics.pendingJobs.Dec()
	return job
}

// isResolved reports whether a source index is no longer active — either
// compacted (covered by a merged index) or absent from the snapshot entirely.
func isResolved(snap *store.Snapshot, id string) bool {
	return snap.IsCompacted(id) || !snap.Contains(id)
}
