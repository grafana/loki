package scheduler

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

const (
	DefaultPriority              = 0 // TODO(owen-d): better determine priority when unknown
	defaultCompletedJobsCapacity = 100
)

type jobQueueMetrics struct {
	pending    prometheus.Gauge
	inProgress prometheus.Gauge
	completed  *prometheus.CounterVec
}

func newJobQueueMetrics(r prometheus.Registerer) *jobQueueMetrics {
	return &jobQueueMetrics{
		pending: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_block_scheduler_pending_jobs",
			Help: "Number of jobs in the block scheduler queue",
		}),
		inProgress: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_block_scheduler_in_progress_jobs",
			Help: "Number of jobs currently being processed",
		}),
		completed: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_block_scheduler_completed_jobs_total",
			Help: "Total number of jobs completed by the block scheduler",
		}, []string{"status"}),
	}
}

// JobWithMetadata wraps a job with additional metadata for tracking its lifecycle
type JobWithMetadata struct {
	*types.Job

	Priority   int
	Status     types.JobStatus
	StartTime  time.Time
	UpdateTime time.Time
}

// NewJobWithMetadata creates a new JobWithMetadata instance
func NewJobWithMetadata(job *types.Job, priority int) *JobWithMetadata {
	return &JobWithMetadata{
		Job:        job,
		Priority:   priority,
		Status:     types.JobStatusPending,
		UpdateTime: time.Now(),
	}
}

type JobQueueConfig struct {
	LeaseExpiryCheckInterval time.Duration `yaml:"lease_expiry_check_interval"`
	LeaseDuration            time.Duration `yaml:"lease_duration"`
}

func (cfg *JobQueueConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.LeaseExpiryCheckInterval, "jobqueue.lease-expiry-check-interval", 1*time.Minute, "Interval to check for expired job leases")
	f.DurationVar(&cfg.LeaseDuration, "jobqueue.lease-duration", 10*time.Minute, "Duration after which a job lease is considered expired if the scheduler receives no updates from builders about the job. Expired jobs are re-enqueued")
}

// JobQueue is a thread-safe implementation of a job queue with state tracking
type JobQueue struct {
	cfg JobQueueConfig

	mu         sync.RWMutex
	pending    *PriorityQueue[string, *JobWithMetadata] // Jobs waiting to be processed, ordered by priority
	inProgress map[string]*JobWithMetadata              // Jobs currently being processed
	completed  *CircularBuffer[*JobWithMetadata]        // Last N completed jobs
	statusMap  map[string]types.JobStatus               // Maps job ID to its current status

	logger  log.Logger
	metrics *jobQueueMetrics
}

// NewJobQueue creates a new JobQueue instance
func NewJobQueue(cfg JobQueueConfig, logger log.Logger, reg prometheus.Registerer) *JobQueue {

	return &JobQueue{
		cfg:        cfg,
		pending:    NewPriorityQueue(priorityComparator, jobIDExtractor),
		inProgress: make(map[string]*JobWithMetadata),
		completed:  NewCircularBuffer[*JobWithMetadata](defaultCompletedJobsCapacity),
		statusMap:  make(map[string]types.JobStatus),
		logger:     logger,
		metrics:    newJobQueueMetrics(reg),
	}
}

// RunLeaseExpiryChecker periodically checks for expired job leases and requeues them
func (q *JobQueue) RunLeaseExpiryChecker(ctx context.Context) {
	ticker := time.NewTicker(q.cfg.LeaseExpiryCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			level.Debug(q.logger).Log("msg", "checking for expired job leases")
			if err := q.requeueExpiredJobs(); err != nil {
				level.Error(q.logger).Log("msg", "failed to requeue expired jobs", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// requeueExpiredJobs checks for jobs that have exceeded their lease duration and requeues them
func (q *JobQueue) requeueExpiredJobs() error {
	// First collect expired jobs while holding the lock
	q.mu.Lock()
	var expiredJobs []*JobWithMetadata
	for id, job := range q.inProgress {
		if time.Since(job.UpdateTime) > q.cfg.LeaseDuration {
			level.Warn(q.logger).Log("msg", "job lease expired, will requeue", "job", id, "update_time", job.UpdateTime, "now", time.Now())
			expiredJobs = append(expiredJobs, job)
		}
	}
	q.mu.Unlock()

	// Then requeue them without holding the lock
	var multiErr error
	for _, job := range expiredJobs {
		// First try to transition from in-progress to expired
		ok, err := q.TransitionState(job.ID(), types.JobStatusInProgress, types.JobStatusExpired)
		if err != nil {
			level.Error(q.logger).Log("msg", "failed to mark job as expired", "job", job.ID(), "err", err)
			multiErr = errors.Join(multiErr, fmt.Errorf("failed to mark job %s as expired: %w", job.ID(), err))
			continue
		}
		if !ok {
			// Job is no longer in progress, someone else must have handled it
			level.Debug(q.logger).Log("msg", "job no longer in progress, skipping expiry", "job", job.ID())
			continue
		}

		// Then re-enqueue it
		_, _, err = q.TransitionAny(job.ID(), types.JobStatusPending, func() (*JobWithMetadata, error) {
			return NewJobWithMetadata(job.Job, job.Priority), nil
		})
		if err != nil {
			level.Error(q.logger).Log("msg", "failed to requeue expired job", "job", job.ID(), "err", err)
			multiErr = errors.Join(multiErr, fmt.Errorf("failed to requeue expired job %s: %w", job.ID(), err))
		}
	}

	return multiErr
}

// priorityComparator compares two jobs by priority (higher priority first)
func priorityComparator(a, b *JobWithMetadata) bool {
	return a.Priority > b.Priority
}

// jobIDExtractor extracts the job ID from a JobWithMetadata
func jobIDExtractor(j *JobWithMetadata) string {
	return j.ID()
}

// TransitionState attempts to transition a job from one specific state to another
func (q *JobQueue) TransitionState(jobID string, from, to types.JobStatus) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	currentStatus, exists := q.statusMap[jobID]
	if !exists {
		return false, fmt.Errorf("job %s not found", jobID)
	}

	if currentStatus != from {
		return false, fmt.Errorf("job %s is in state %s, not %s", jobID, currentStatus, from)
	}

	return q.transitionLockLess(jobID, to)
}

// TransitionAny transitions a job from any state to the specified state
func (q *JobQueue) TransitionAny(jobID string, to types.JobStatus, createFn func() (*JobWithMetadata, error)) (prevStatus types.JobStatus, found bool, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	currentStatus, exists := q.statusMap[jobID]

	// If the job isn't found or has already finished, create a new job
	if finished := currentStatus.IsFinished(); !exists || finished {

		// exception:
		// we're just moving one finished type to another; no need to re-enqueue
		if finished && to.IsFinished() {
			q.statusMap[jobID] = to
			if j, found := q.completed.Lookup(
				func(jwm *JobWithMetadata) bool {
					return jwm.ID() == jobID
				},
			); found {
				j.Status = to
				j.UpdateTime = time.Now()
			}
			return currentStatus, true, nil
		}

		if createFn == nil {
			return types.JobStatusUnknown, false, fmt.Errorf("job %s not found and no creation function provided", jobID)
		}

		if finished {
			level.Debug(q.logger).Log("msg", "creating a copy of already-completed job", "id", jobID, "from", currentStatus, "to", to)
		}

		job, err := createFn()
		if err != nil {
			return types.JobStatusUnknown, false, fmt.Errorf("failed to create job %s: %w", jobID, err)
		}

		// temporarily mark as pending so we can transition it to the target state
		q.statusMap[jobID] = types.JobStatusPending
		q.pending.Push(job)
		q.metrics.pending.Inc()
		level.Debug(q.logger).Log("msg", "created new job", "id", jobID, "status", types.JobStatusPending)

		if _, err := q.transitionLockLess(jobID, to); err != nil {
			return types.JobStatusUnknown, false, err
		}
		return types.JobStatusUnknown, false, nil
	}

	_, err = q.transitionLockLess(jobID, to)
	return currentStatus, true, err
}

// transitionLockLess performs the actual state transition (must be called with lock held)
func (q *JobQueue) transitionLockLess(jobID string, to types.JobStatus) (bool, error) {
	from := q.statusMap[jobID]
	if from == to {
		return false, nil
	}

	var job *JobWithMetadata

	// Remove from current state
	switch from {
	case types.JobStatusPending:
		if j, exists := q.pending.Remove(jobID); exists {
			job = j
			q.metrics.pending.Dec()
		}
	case types.JobStatusInProgress:
		if j, exists := q.inProgress[jobID]; exists {
			job = j
			delete(q.inProgress, jobID)
			q.metrics.inProgress.Dec()
		}
	}

	if job == nil {
		return false, fmt.Errorf("job %s not found in its supposed state %s", jobID, from)
	}

	// Add to new state
	job.Status = to
	job.UpdateTime = time.Now()
	q.statusMap[jobID] = to

	switch to {
	case types.JobStatusPending:
		q.pending.Push(job)
		q.metrics.pending.Inc()
	case types.JobStatusInProgress:
		q.inProgress[jobID] = job
		q.metrics.inProgress.Inc()
		job.StartTime = job.UpdateTime
	case types.JobStatusComplete, types.JobStatusFailed, types.JobStatusExpired:
		q.completed.Push(job)
		q.metrics.completed.WithLabelValues(to.String()).Inc()
		delete(q.statusMap, jobID) // remove from status map so we don't grow indefinitely
	default:
		return false, fmt.Errorf("invalid target state: %s", to)
	}

	level.Debug(q.logger).Log("msg", "transitioned job state", "id", jobID, "from", from, "to", to)
	return true, nil
}

// Exists checks if a job exists and returns its current status
func (q *JobQueue) Exists(jobID string) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	status, exists := q.statusMap[jobID]
	return status, exists
}

// Dequeue removes and returns the highest priority pending job
func (q *JobQueue) Dequeue() (*types.Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.pending.Peek()
	if !ok {
		return nil, false
	}

	_, err := q.transitionLockLess(job.ID(), types.JobStatusInProgress)
	if err != nil {
		level.Error(q.logger).Log("msg", "failed to transition dequeued job to in progress", "id", job.ID(), "err", err)
		return nil, false
	}

	return job.Job, true
}

// ListPendingJobs returns a list of all pending jobs
func (q *JobQueue) ListPendingJobs() []JobWithMetadata {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// return copies of the jobs since they can change after the lock is released
	jobs := make([]JobWithMetadata, 0, q.pending.Len())
	for _, j := range q.pending.List() {
		cpy := *j.Job
		jobs = append(jobs, JobWithMetadata{
			Job:        &cpy, // force copy
			Priority:   j.Priority,
			Status:     j.Status,
			StartTime:  j.StartTime,
			UpdateTime: j.UpdateTime,
		})
	}

	return jobs
}

// ListInProgressJobs returns a list of all in-progress jobs
func (q *JobQueue) ListInProgressJobs() []JobWithMetadata {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// return copies of the jobs since they can change after the lock is released
	jobs := make([]JobWithMetadata, 0, len(q.inProgress))
	for _, j := range q.inProgress {
		cpy := *j.Job
		jobs = append(jobs, JobWithMetadata{
			Job:        &cpy, // force copy
			Priority:   j.Priority,
			Status:     j.Status,
			StartTime:  j.StartTime,
			UpdateTime: j.UpdateTime,
		})
	}
	return jobs
}

// ListCompletedJobs returns a list of completed jobs
func (q *JobQueue) ListCompletedJobs() []JobWithMetadata {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs := make([]JobWithMetadata, 0, q.completed.Len())
	q.completed.Range(func(job *JobWithMetadata) bool {
		cpy := *job.Job
		jobs = append(jobs, JobWithMetadata{
			Job:        &cpy, // force copy
			Priority:   job.Priority,
			Status:     job.Status,
			StartTime:  job.StartTime,
			UpdateTime: job.UpdateTime,
		})
		return true
	})
	return jobs
}

// UpdatePriority updates the priority of a pending job. If the job is not pending,
// returns false to indicate the update was not performed.
func (q *JobQueue) UpdatePriority(id string, priority int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if job is still pending
	if job, ok := q.pending.Lookup(id); ok {
		// nit: we're technically already updating the prio via reference,
		// but that's fine -- we may refactor this eventually to have 3 generic types: (key, value, priority) where value implements a `Priority() T` method.
		job.Priority = priority
		return q.pending.UpdatePriority(id, job)
	}

	// Job is no longer pending (might be in progress, completed, etc)
	return false
}

// Ping updates the last-updated timestamp of a job and returns whether it was found.
// This is useful for keeping jobs alive and preventing lease expiry.
func (q *JobQueue) Ping(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if job, ok := q.inProgress[id]; ok {
		job.UpdateTime = time.Now()
		return true
	}

	return false
}
