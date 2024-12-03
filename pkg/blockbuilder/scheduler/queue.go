package scheduler

import (
	"fmt"
	"sync"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

// jobAssignment tracks a job and its assigned builder
type jobAssignment struct {
	job       *types.Job
	builderID string
}

// JobQueue manages the queue of pending jobs and tracks their state.
type JobQueue struct {
	pending    map[string]*types.Job     // Jobs waiting to be processed, key is job ID
	inProgress map[string]*jobAssignment // job ID -> assignment info
	completed  map[string]*types.Job     // Completed jobs, key is job ID
	mu         sync.RWMutex
}

// NewJobQueue creates a new job queue instance
func NewJobQueue() *JobQueue {
	return &JobQueue{
		pending:    make(map[string]*types.Job),
		inProgress: make(map[string]*jobAssignment),
		completed:  make(map[string]*types.Job),
	}
}

func (q *JobQueue) Exists(job *types.Job) (types.JobStatus, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if _, ok := q.inProgress[job.ID]; ok {
		return types.JobStatusInProgress, true
	}

	if _, ok := q.pending[job.ID]; ok {
		return types.JobStatusPending, true
	}

	if _, ok := q.completed[job.ID]; ok {
		return types.JobStatusComplete, true
	}

	return -1, false
}

// Enqueue adds a new job to the pending queue
// This is a naive implementation, intended to be refactored
func (q *JobQueue) Enqueue(job *types.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.pending[job.ID]; exists {
		return fmt.Errorf("job %s already exists in pending queue", job.ID)
	}
	if _, exists := q.inProgress[job.ID]; exists {
		return fmt.Errorf("job %s already exists in progress", job.ID)
	}
	if _, exists := q.completed[job.ID]; exists {
		return fmt.Errorf("job %s already completed", job.ID)
	}

	q.pending[job.ID] = job
	return nil
}

// Dequeue gets the next available job and assigns it to a builder
func (q *JobQueue) Dequeue(builderID string) (*types.Job, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Simple FIFO for now
	for id, job := range q.pending {
		delete(q.pending, id)
		q.inProgress[id] = &jobAssignment{
			job:       job,
			builderID: builderID,
		}
		return job, true, nil
	}

	return nil, false, nil
}

// MarkComplete moves a job from in-progress to completed
func (q *JobQueue) MarkComplete(jobID string, builderID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	assignment, exists := q.inProgress[jobID]
	if !exists {
		return fmt.Errorf("job %s not found in progress", jobID)
	}

	if assignment.builderID != builderID {
		return fmt.Errorf("job %s not assigned to builder %s", jobID, builderID)
	}

	delete(q.inProgress, jobID)
	q.completed[jobID] = assignment.job
	return nil
}

// SyncJob updates the state of an in-progress job
func (q *JobQueue) SyncJob(jobID string, builderID string, job *types.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	assignment, exists := q.inProgress[jobID]
	if !exists {
		return fmt.Errorf("job %s not found in progress", jobID)
	}

	if assignment.builderID != builderID {
		return fmt.Errorf("job %s not assigned to builder %s", jobID, builderID)
	}

	assignment.job = job
	return nil
}
