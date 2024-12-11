package types

import "context"

// BuilderTransport is for calls originating from the builder
type BuilderTransport interface {
	// SendGetJobRequest sends a request to get a new job
	SendGetJobRequest(ctx context.Context, req *GetJobRequest) (*GetJobResponse, error)
	// SendCompleteJob sends a job completion notification
	SendCompleteJob(ctx context.Context, req *CompleteJobRequest) error
	// SendSyncJob sends a job sync request
	SendSyncJob(ctx context.Context, req *SyncJobRequest) error
}

// SchedulerHandler defines the business logic for handling builder requests
type SchedulerHandler interface {
	// HandleGetJob processes a request for a new job
	HandleGetJob(ctx context.Context) (*Job, bool, error)
	// HandleCompleteJob processes a job completion notification
	HandleCompleteJob(ctx context.Context, job *Job, success bool) error
	// HandleSyncJob processes a job sync request
	HandleSyncJob(ctx context.Context, job *Job) error
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
	Success   bool
}

type SyncJobRequest struct {
	BuilderID string
	Job       *Job
}
