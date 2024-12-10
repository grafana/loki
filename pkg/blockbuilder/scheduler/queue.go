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

// JobWithPriority wraps a job with a priority value
type JobWithPriority[T comparable] struct {
	Job      *types.Job
	Priority T
}

// NewJobWithPriority creates a new JobWithPriority instance
func NewJobWithPriority[T comparable](job *types.Job, priority T) *JobWithPriority[T] {
	return &JobWithPriority[T]{
		Job:      job,
		Priority: priority,
	}
}

// inProgressJob contains a job and its start time
type inProgressJob struct {
	job       *types.Job
	startTime time.Time
}

// Duration returns how long the job has been running
func (j *inProgressJob) Duration() time.Duration {
	return time.Since(j.startTime)
}

// JobQueue manages the queue of pending jobs and tracks their state.
type JobQueue struct {
	pending    *PriorityQueue[*JobWithPriority[int], string] // Jobs waiting to be processed, ordered by priority
	inProgress map[string]*inProgressJob                     // Jobs currently being processed, key is job ID
	completed  *CircularBuffer[*types.Job]                   // Last N completed jobs
	statusMap  map[string]types.JobStatus                    // Maps job ID to its current status
	mu         sync.RWMutex
}

// NewJobQueue creates a new job queue instance
func NewJobQueue() *JobQueue {
	return &JobQueue{
		pending: NewPriorityQueue[*JobWithPriority[int]](
			func(a, b *JobWithPriority[int]) bool {
				return a.Priority > b.Priority // Higher priority first
			},
			func(a *JobWithPriority[int]) string {
				return a.Job.ID
			},
		),
		inProgress: make(map[string]*inProgressJob),
		completed:  NewCircularBuffer[*types.Job](defaultCompletedJobsCapacity),
		statusMap:  make(map[string]types.JobStatus),
	}
}

func (q *JobQueue) Exists(job *types.Job) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	status, exists := q.statusMap[job.ID]
	return status, exists
}

// Enqueue adds a new job to the pending queue with a priority
func (q *JobQueue) Enqueue(job *types.Job, priority int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if job already exists
	if status, exists := q.statusMap[job.ID]; exists {
		return fmt.Errorf("job %s already exists with status %v", job.ID, status)
	}

	jobWithPriority := NewJobWithPriority(job, priority)
	q.pending.Push(jobWithPriority)
	q.statusMap[job.ID] = types.JobStatusPending
	return nil
}

// Dequeue gets the next available job and assigns it to a builder
func (q *JobQueue) Dequeue(_ string) (*types.Job, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.pending.Len() == 0 {
		return nil, false, nil
	}

	jobWithPriority, ok := q.pending.Pop()
	if !ok {
		return nil, false, nil
	}

	// Add to in-progress with current time
	q.inProgress[jobWithPriority.Job.ID] = &inProgressJob{
		job:       jobWithPriority.Job,
		startTime: time.Now(),
	}
	q.statusMap[jobWithPriority.Job.ID] = types.JobStatusInProgress

	return jobWithPriority.Job, true, nil
}

// MarkComplete moves a job from in-progress to completed
func (q *JobQueue) MarkComplete(jobID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Find job in in-progress map
	inProgressJob, exists := q.inProgress[jobID]
	// if it doesn't exist, it could be previously removed (duplicate job execution)
	// or the scheduler may have restarted and not have the job state anymore.
	if exists {
		// Remove from in-progress
		delete(q.inProgress, jobID)
	}

	// Add to completed buffer and handle evicted job
	if evictedJob, hasEvicted := q.completed.Push(inProgressJob.job); hasEvicted {
		// Remove evicted job from status map
		delete(q.statusMap, evictedJob.ID)
	}
	q.statusMap[jobID] = types.JobStatusComplete
}

// SyncJob registers a job as in-progress, used for restoring state after scheduler restarts
func (q *JobQueue) SyncJob(jobID string, _ string, job *types.Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Add directly to in-progress
	q.inProgress[jobID] = &inProgressJob{
		job:       job,
		startTime: time.Now(),
	}
	q.statusMap[jobID] = types.JobStatusInProgress
}
