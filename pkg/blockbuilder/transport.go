package blockbuilder

import "context"

// Message types for transport layer
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

// Transport interface defines methods for communication between components
type Transport interface {
	// SendGetJobRequest requests a new job from the scheduler
	SendGetJobRequest(ctx context.Context, req *GetJobRequest) (*GetJobResponse, error)
	// SendCompleteJob notifies the scheduler that a job is complete
	SendCompleteJob(ctx context.Context, req *CompleteJobRequest) error
	// SendSyncJob syncs job state with the scheduler
	SendSyncJob(ctx context.Context, req *SyncJobRequest) error
}

// unimplementedTransport provides default implementations that panic
type unimplementedTransport struct{}

func (t *unimplementedTransport) SendGetJobRequest(_ context.Context, _ *GetJobRequest) (*GetJobResponse, error) {
	panic("unimplemented")
}

func (t *unimplementedTransport) SendCompleteJob(_ context.Context, _ *CompleteJobRequest) error {
	panic("unimplemented")
}

func (t *unimplementedTransport) SendSyncJob(_ context.Context, _ *SyncJobRequest) error {
	panic("unimplemented")
}

// GRPCTransport implements Transport using gRPC
type GRPCTransport struct {
	unimplementedTransport
	// Add gRPC client fields here
}

// NewGRPCTransport creates a new gRPC transport instance
func NewGRPCTransport() *GRPCTransport {
	return &GRPCTransport{}
}

// MemoryTransport implements Transport for in-memory testing
type MemoryTransport struct {
	scheduler Scheduler
}

// NewMemoryTransport creates a new in-memory transport connected to a scheduler
func NewMemoryTransport(scheduler Scheduler) *MemoryTransport {
	return &MemoryTransport{
		scheduler: scheduler,
	}
}

func (t *MemoryTransport) SendGetJobRequest(ctx context.Context, req *GetJobRequest) (*GetJobResponse, error) {
	job, ok, err := t.scheduler.HandleGetJob(ctx, req.BuilderID)
	if err != nil {
		return nil, err
	}
	return &GetJobResponse{
		Job: job,
		OK:  ok,
	}, nil
}

func (t *MemoryTransport) SendCompleteJob(ctx context.Context, req *CompleteJobRequest) error {
	return t.scheduler.HandleCompleteJob(ctx, req.BuilderID, req.Job)
}

func (t *MemoryTransport) SendSyncJob(ctx context.Context, req *SyncJobRequest) error {
	return t.scheduler.HandleSyncJob(ctx, req.BuilderID, req.Job)
}
