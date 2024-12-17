package scheduler

import (
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

// JobQueue manages the queue of pending jobs and tracks their state.
type JobQueue struct {
	logger     log.Logger
	pending    *PriorityQueue[string, *JobWithMetadata] // Jobs waiting to be processed, ordered by priority
	inProgress map[string]*JobWithMetadata              // Jobs currently being processed
	completed  *CircularBuffer[*JobWithMetadata]        // Last N completed jobs
	statusMap  map[string]types.JobStatus               // Maps job ID to its current status
	metrics    *jobQueueMetrics
	mu         sync.RWMutex
}

func NewJobQueueWithLogger(logger log.Logger, reg prometheus.Registerer) *JobQueue {
	return &JobQueue{
		logger: logger,
		pending: NewPriorityQueue(
			func(a, b *JobWithMetadata) bool {
				return a.Priority > b.Priority // Higher priority first
			},
			func(j *JobWithMetadata) string { return j.ID() },
		),
		inProgress: make(map[string]*JobWithMetadata),
		completed:  NewCircularBuffer[*JobWithMetadata](defaultCompletedJobsCapacity),
		statusMap:  make(map[string]types.JobStatus),
		metrics:    newJobQueueMetrics(reg),
	}
}

// NewJobQueue creates a new job queue instance
func NewJobQueue() *JobQueue {
	return NewJobQueueWithLogger(log.NewNopLogger(), prometheus.DefaultRegisterer)
}

// Exists checks if a job exists in any state and returns its status
func (q *JobQueue) Exists(job *types.Job) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	x, ok := q.existsLockLess(job.ID())
	if !ok {
		return types.JobStatusUnknown, false
	}
	return x.Status, ok
}

func (q *JobQueue) existsLockLess(id string) (*JobWithMetadata, bool) {
	status, ok := q.statusMap[id]
	if !ok {
		return nil, false
	}

	switch status {
	case types.JobStatusPending:
		return q.pending.Lookup(id)
	case types.JobStatusInProgress:
		res, ok := q.inProgress[id]
		return res, ok
	case types.JobStatusComplete:
		return q.completed.Lookup(func(jwm *JobWithMetadata) bool {
			return jwm.ID() == id
		})
	default:
		return nil, false
	}
}

// Enqueue adds a job to the pending queue with the given priority
func (q *JobQueue) Enqueue(job *types.Job, priority int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if job already exists
	if status, exists := q.statusMap[job.ID()]; exists {
		return fmt.Errorf("job %s already exists with status %v", job.ID(), status)
	}

	jobMeta := NewJobWithMetadata(job, priority)
	q.pending.Push(jobMeta)
	q.statusMap[job.ID()] = types.JobStatusPending
	q.metrics.pending.Inc()
	return nil
}

// Dequeue removes and returns the highest priority job from the pending queue
func (q *JobQueue) Dequeue() (*types.Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	jobMeta, ok := q.pending.Pop()
	if !ok {
		return nil, false
	}
	q.metrics.pending.Dec()

	// Update metadata for in-progress state
	jobMeta.Status = types.JobStatusInProgress
	jobMeta.StartTime = time.Now()
	jobMeta.UpdateTime = jobMeta.StartTime

	q.inProgress[jobMeta.ID()] = jobMeta
	q.statusMap[jobMeta.ID()] = types.JobStatusInProgress
	q.metrics.inProgress.Inc()

	return jobMeta.Job, true
}

// GetInProgressJob retrieves a job that is currently being processed
func (q *JobQueue) GetInProgressJob(id string) (*types.Job, time.Time, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if jobMeta, ok := q.inProgress[id]; ok {
		return jobMeta.Job, jobMeta.StartTime, true
	}
	return nil, time.Time{}, false
}

// RemoveInProgress removes a job from the in-progress map
func (q *JobQueue) RemoveInProgress(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.inProgress, id)
	q.metrics.inProgress.Dec()
}

// MarkComplete moves a job from in-progress to completed with the given status
func (q *JobQueue) MarkComplete(id string, status types.JobStatus) {
	q.mu.Lock()
	defer q.mu.Unlock()

	jobMeta, ok := q.existsLockLess(id)
	if !ok {
		level.Error(q.logger).Log("msg", "failed to mark job as complete", "job", id, "status", status)
		return
	}

	switch jobMeta.Status {
	case types.JobStatusInProgress:
		// update & remove from in progress
		delete(q.inProgress, id)
		q.metrics.inProgress.Dec()
	case types.JobStatusPending:
		_, ok := q.pending.Remove(id)
		if !ok {
			level.Error(q.logger).Log("msg", "failed to remove job from pending queue", "job", id)
		}
		q.metrics.pending.Dec()
	default:
		level.Error(q.logger).Log("msg", "unknown job status, cannot mark as complete", "job", id, "status", status)
	}

	jobMeta.Status = status
	jobMeta.UpdateTime = time.Now()

	// add it to the completed buffer, removing any evicted job from the statusMap
	removal, evicted := q.completed.Push(jobMeta)
	if evicted {
		delete(q.statusMap, removal.ID())
	}
	q.statusMap[id] = status
	q.metrics.completed.WithLabelValues(status.String()).Inc()
}

// SyncJob registers a job as in-progress or updates its UpdateTime if already in progress
func (q *JobQueue) SyncJob(jobID string, job *types.Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Helper function to create a new job
	registerInProgress := func() {
		// Job does not exist; add it as in-progress
		now := time.Now()
		jobMeta := NewJobWithMetadata(job, DefaultPriority)
		jobMeta.StartTime = now
		jobMeta.UpdateTime = now
		jobMeta.Status = types.JobStatusInProgress
		q.inProgress[jobID] = jobMeta
		q.statusMap[jobID] = types.JobStatusInProgress
		q.metrics.inProgress.Inc()
	}

	jobMeta, ok := q.existsLockLess(jobID)

	if !ok {
		registerInProgress()
		return
	}

	switch jobMeta.Status {
	case types.JobStatusPending:
		// Job already pending, move to in-progress
		_, ok := q.pending.Remove(jobID)
		if !ok {
			level.Error(q.logger).Log("msg", "failed to remove job from pending queue", "job", jobID)
		}
		jobMeta.Status = types.JobStatusInProgress
		q.metrics.pending.Dec()
		q.metrics.inProgress.Inc()
	case types.JobStatusInProgress:
	case types.JobStatusComplete, types.JobStatusFailed, types.JobStatusExpired:
		// Job already completed, re-enqueue a new one
		registerInProgress()
		return
	default:
		registerInProgress()
		return
	}

	jobMeta.UpdateTime = time.Now()
	q.inProgress[jobID] = jobMeta
	q.statusMap[jobID] = types.JobStatusInProgress
}
