package blockbuilder

import (
	"fmt"
	"sync"
)

// JobQueue manages the queue of pending jobs and tracks their state.
type JobQueue struct {
	pending    map[string]*Job           // Jobs waiting to be processed, key is job ID
	inProgress map[string]*jobAssignment // job ID -> assignment info
	completed  map[string]*Job           // Completed jobs, key is job ID
	mu         sync.RWMutex
}

type jobAssignment struct {
	builderID string
	job       *Job
}

// NewJobQueue creates a new job queue instance
func NewJobQueue() *JobQueue {
	return &JobQueue{
		pending:    make(map[string]*Job),
		inProgress: make(map[string]*jobAssignment),
		completed:  make(map[string]*Job),
	}
}

// Enqueue adds a new job to the pending queue
func (q *JobQueue) Enqueue(job *Job) error {
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

	job.Status = JobStatusPending
	q.pending[job.ID] = job
	return nil
}

// Dequeue gets the next available job and assigns it to a builder
func (q *JobQueue) Dequeue(builderID string) (*Job, bool, error) {
	if builderID == "" {
		return nil, false, fmt.Errorf("builder ID cannot be empty")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Get first available pending job
	var jobID string
	var job *Job
	for id, j := range q.pending {
		jobID = id
		job = j
		break
	}

	if job == nil {
		return nil, false, nil
	}

	// Move job to in progress
	delete(q.pending, jobID)
	job.Status = JobStatusInProgress
	q.inProgress[jobID] = &jobAssignment{
		builderID: builderID,
		job:       job,
	}

	return job, true, nil
}

// MarkComplete moves a job from in progress to completed
func (q *JobQueue) MarkComplete(jobID string, builderID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	assignment, exists := q.inProgress[jobID]
	if !exists {
		return fmt.Errorf("job %s not found in progress", jobID)
	}
	if assignment.builderID != builderID {
		return fmt.Errorf("job %s is assigned to builder %s, not %s", jobID, assignment.builderID, builderID)
	}

	job := assignment.job
	delete(q.inProgress, jobID)
	job.Status = JobStatusComplete
	q.completed[jobID] = job

	return nil
}

// SyncJob updates the state of an in-progress job
func (q *JobQueue) SyncJob(jobID string, builderID string, job *Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	assignment, exists := q.inProgress[jobID]
	if !exists {
		return fmt.Errorf("job %s not found in progress", jobID)
	}
	if assignment.builderID != builderID {
		return fmt.Errorf("job %s is assigned to builder %s, not %s", jobID, assignment.builderID, builderID)
	}

	// Update job state
	assignment.job = job
	return nil
}
