package types

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

// Scheduler interface defines the methods for scheduling jobs and managing worker pools.
type Scheduler interface {
	// HandleGetJob processes a job request from a block builder
	HandleGetJob(ctx context.Context, builderID string) (*Job, bool, error)
	// HandleCompleteJob processes a job completion notification
	HandleCompleteJob(ctx context.Context, builderID string, job *Job) error
	// HandleSyncJob processes a job sync request
	HandleSyncJob(ctx context.Context, builderID string, job *Job) error
}

// Transport defines the interface for communication between block builders and scheduler
type Transport interface {
	BuilderTransport
	SchedulerTransport
}

// SchedulerTransport is for calls originating from the scheduler
type SchedulerTransport interface{}

// BuilderTransport is for calls originating from the builder
type BuilderTransport interface {
	// SendGetJobRequest sends a request to get a new job
	SendGetJobRequest(ctx context.Context, req *GetJobRequest) (*GetJobResponse, error)
	// SendCompleteJob sends a job completion notification
	SendCompleteJob(ctx context.Context, req *CompleteJobRequest) error
	// SendSyncJob sends a job sync request
	SendSyncJob(ctx context.Context, req *SyncJobRequest) error
}

// Request/Response message types
type GetJobRequest struct {
	BuilderID string
}

type GetJobResponse struct {
	Job *Job
	OK  bool
}

type CompleteJobRequest struct {
	BuilderID string
	Job       *Job
}

type SyncJobRequest struct {
	BuilderID string
	Job       *Job
}
