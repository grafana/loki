package types

import (
	"context"
)

var (
	_ BuilderTransport = unimplementedTransport{}
	_ BuilderTransport = &MemoryTransport{}
)

// unimplementedTransport provides default implementations that panic
type unimplementedTransport struct{}

func (t unimplementedTransport) SendGetJobRequest(_ context.Context, _ *GetJobRequest) (*GetJobResponse, error) {
	panic("unimplemented")
}

func (t unimplementedTransport) SendCompleteJob(_ context.Context, _ *CompleteJobRequest) error {
	panic("unimplemented")
}

func (t unimplementedTransport) SendSyncJob(_ context.Context, _ *SyncJobRequest) error {
	panic("unimplemented")
}

// MemoryTransport implements Transport interface for in-memory communication
type MemoryTransport struct {
	scheduler SchedulerHandler
}

// NewMemoryTransport creates a new in-memory transport instance
func NewMemoryTransport(scheduler SchedulerHandler) *MemoryTransport {
	return &MemoryTransport{
		scheduler: scheduler,
	}
}

func (t *MemoryTransport) SendGetJobRequest(ctx context.Context, _ *GetJobRequest) (*GetJobResponse, error) {
	job, ok, err := t.scheduler.HandleGetJob(ctx)
	if err != nil {
		return nil, err
	}
	return &GetJobResponse{
		Job: job,
		OK:  ok,
	}, nil
}

func (t *MemoryTransport) SendCompleteJob(ctx context.Context, req *CompleteJobRequest) error {
	return t.scheduler.HandleCompleteJob(ctx, req.Job, req.Success)
}

func (t *MemoryTransport) SendSyncJob(ctx context.Context, req *SyncJobRequest) error {
	return t.scheduler.HandleSyncJob(ctx, req.Job)
}
