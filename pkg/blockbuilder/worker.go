package blockbuilder

import "context"

// Worker interface defines the methods for processing jobs and reporting status.
type Worker interface {
	// GetJob requests a new job from the scheduler
	GetJob(ctx context.Context) (*Job, bool, error)
	// CompleteJob marks a job as finished
	CompleteJob(ctx context.Context, job *Job) error
	// SyncJob informs the scheduler about an in-progress job
	SyncJob(ctx context.Context, job *Job) error
}

// unimplementedWorker provides default implementations for the Worker interface.
type unimplementedWorker struct{}

func (u *unimplementedWorker) GetJob(ctx context.Context) (*Job, bool, error) {
	panic("unimplemented")
}

func (u *unimplementedWorker) CompleteJob(ctx context.Context, job *Job) error {
	panic("unimplemented")
}

func (u *unimplementedWorker) SyncJob(ctx context.Context, job *Job) error {
	panic("unimplemented")
}

// WorkerImpl is the implementation of the Worker interface.
type WorkerImpl struct {
	unimplementedWorker
	// Add necessary fields here
}

// NewWorker creates a new Worker instance.
func NewWorker() Worker {
	return &WorkerImpl{}
}
