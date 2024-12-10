package scheduler

import (
	"fmt"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

const (
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

// JobQueue manages the queue of pending jobs and tracks their state.
type JobQueue struct {
	pending    *PriorityQueue[string, *JobWithMetadata] // Jobs waiting to be processed, ordered by priority
	inProgress map[string]*JobWithMetadata              // Jobs currently being processed
	completed  *CircularBuffer[*JobWithMetadata]        // Last N completed jobs
	statusMap  map[string]types.JobStatus               // Maps job ID to its current status
	mu         sync.RWMutex
}

// NewJobQueue creates a new job queue instance
func NewJobQueue() *JobQueue {
	return &JobQueue{
		pending: NewPriorityQueue(
			func(a, b *JobWithMetadata) bool {
				return a.Priority > b.Priority // Higher priority first
			},
			func(j *JobWithMetadata) string { return j.ID() },
		),
		inProgress: make(map[string]*JobWithMetadata),
		completed:  NewCircularBuffer[*JobWithMetadata](defaultCompletedJobsCapacity),
		statusMap:  make(map[string]types.JobStatus),
	}
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

	// Update metadata for in-progress state
	jobMeta.Status = types.JobStatusInProgress
	jobMeta.StartTime = time.Now()
	jobMeta.UpdateTime = jobMeta.StartTime

	q.inProgress[jobMeta.ID()] = jobMeta
	q.statusMap[jobMeta.ID()] = types.JobStatusInProgress

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
}

// MarkComplete moves a job from in-progress to completed with the given status
func (q *JobQueue) MarkComplete(id string, status types.JobStatus) {
	q.mu.Lock()
	defer q.mu.Unlock()

	jobMeta, ok := q.inProgress[id]
	if !ok {
		return
	}

	// Update metadata for completion
	jobMeta.Status = status
	jobMeta.UpdateTime = time.Now()

	// Add to completed buffer
	if old, evicted := q.completed.Push(jobMeta); evicted {
		// If the buffer is full, evict the oldest job and remove it from the status map to avoid leaks
		delete(q.statusMap, old.ID())
	}

	// Update status map and clean up
	q.statusMap[id] = status
	delete(q.inProgress, id)

	// If the job failed, re-enqueue it with its original priority
	if status == types.JobStatusFailed {
		// Create new metadata for the re-enqueued job
		newJobMeta := NewJobWithMetadata(jobMeta.Job, jobMeta.Priority)
		q.pending.Push(newJobMeta)
		q.statusMap[id] = types.JobStatusPending
	}
}

// GetStatus returns the current status of a job
func (q *JobQueue) GetStatus(id string) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	status, ok := q.statusMap[id]
	return status, ok
}

// SyncJob registers a job as in-progress or updates its UpdateTime if already in progress
func (q *JobQueue) SyncJob(jobID string, job *types.Job) {
	// Check if job exists and is in progress
	if status, exists := q.Exists(job); exists && status == types.JobStatusInProgress {
		q.mu.Lock()
		if existingJob, ok := q.inProgress[jobID]; ok {
			existingJob.UpdateTime = time.Now()
		}
		q.mu.Unlock()
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Add new job to in-progress
	jobMeta := &JobWithMetadata{
		Job:        job,
		Priority:   0, // Priority is not known in this case
		Status:     types.JobStatusInProgress,
		StartTime:  time.Now(),
		UpdateTime: time.Now(),
	}
	q.inProgress[jobID] = jobMeta
	q.statusMap[jobID] = types.JobStatusInProgress
}
