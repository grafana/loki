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

// JobWithPriority wraps a job with its priority
type JobWithPriority[P any] struct {
	*types.Job
	Priority P
}

func NewJobWithPriority[P any](job *types.Job, priority P) *JobWithPriority[P] {
	return &JobWithPriority[P]{
		Job:      job,
		Priority: priority,
	}
}

// JobWithStatus wraps a job with its completion status and time
type JobWithStatus struct {
	*types.Job
	Status      types.JobStatus
	CompletedAt time.Time
}

// inProgressJob tracks a job that is currently being processed
type inProgressJob struct {
	*types.Job
	StartTime time.Time
}

// JobQueue manages the queue of pending jobs and tracks their state.
type JobQueue struct {
	pending    *PriorityQueue[string, *JobWithPriority[int]] // Jobs waiting to be processed, ordered by priority
	inProgress map[string]*inProgressJob                     // Jobs currently being processed, key is job ID
	completed  *CircularBuffer[*JobWithStatus]               // Last N completed jobs with their status
	statusMap  map[string]types.JobStatus                    // Maps job ID to its current status
	mu         sync.RWMutex
}

// NewJobQueue creates a new job queue instance
func NewJobQueue() *JobQueue {
	return &JobQueue{
		pending: NewPriorityQueue(
			func(a, b *JobWithPriority[int]) bool {
				return a.Priority > b.Priority // Higher priority first
			},
			func(j *JobWithPriority[int]) string { return j.ID },
		),
		inProgress: make(map[string]*inProgressJob),
		completed:  NewCircularBuffer[*JobWithStatus](defaultCompletedJobsCapacity), // Keep last 100 completed jobs
		statusMap:  make(map[string]types.JobStatus),
	}
}

// Exists checks if a job exists in any state and returns its status
func (q *JobQueue) Exists(job *types.Job) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	status, ok := q.statusMap[job.ID]
	return status, ok
}

// Enqueue adds a job to the pending queue with the given priority
func (q *JobQueue) Enqueue(job *types.Job, priority int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if job already exists
	if status, exists := q.statusMap[job.ID]; exists {
		return fmt.Errorf("job %s already exists with status %v", job.ID, status)
	}

	q.pending.Push(NewJobWithPriority(job, priority))
	q.statusMap[job.ID] = types.JobStatusPending
	return nil
}

// Dequeue removes and returns the highest priority job from the pending queue
func (q *JobQueue) Dequeue() (*types.Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.pending.Pop()
	if !ok {
		return nil, false
	}

	q.inProgress[job.ID] = &inProgressJob{
		Job:       job.Job,
		StartTime: time.Now(),
	}
	q.statusMap[job.ID] = types.JobStatusInProgress

	return job.Job, true
}

// GetInProgressJob retrieves a job that is currently being processed
func (q *JobQueue) GetInProgressJob(id string) (*types.Job, time.Time, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if job, ok := q.inProgress[id]; ok {
		return job.Job, job.StartTime, true
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

	job, ok := q.inProgress[id]
	if !ok {
		return
	}

	// Add to completed buffer with status
	completedJob := &JobWithStatus{
		Job:         job.Job,
		Status:      status,
		CompletedAt: time.Now(),
	}
	_, _ = q.completed.Push(completedJob)

	// Update status map and clean up
	q.statusMap[id] = status
	delete(q.inProgress, id)

	// If the job failed, re-enqueue it with its original priority
	if status == types.JobStatusFailed {
		// Look up the original priority from the pending queue
		if origJob, ok := q.pending.Lookup(id); ok {
			q.pending.Push(origJob) // Re-add with original priority
			q.statusMap[id] = types.JobStatusPending
		}
	}
}

// GetStatus returns the current status of a job
func (q *JobQueue) GetStatus(id string) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	status, ok := q.statusMap[id]
	return status, ok
}

// SyncJob registers a job as in-progress, used for restoring state after scheduler restarts
func (q *JobQueue) SyncJob(jobID string, _ string, job *types.Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Add directly to in-progress
	q.inProgress[jobID] = &inProgressJob{
		Job:       job,
		StartTime: time.Now(),
	}
	q.statusMap[jobID] = types.JobStatusInProgress
}
