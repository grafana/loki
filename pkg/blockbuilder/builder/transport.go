package builder

import (
	"context"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

// unimplementedTransport provides default implementations that panic
type unimplementedTransport struct{}

func (t *unimplementedTransport) SendGetJobRequest(_ context.Context, _ *types.GetJobRequest) (*types.GetJobResponse, error) {
	panic("unimplemented")
}

func (t *unimplementedTransport) SendCompleteJob(_ context.Context, _ *types.CompleteJobRequest) error {
	panic("unimplemented")
}

func (t *unimplementedTransport) SendSyncJob(_ context.Context, _ *types.SyncJobRequest) error {
	panic("unimplemented")
}

// MemoryTransport implements Transport interface for in-memory communication
type MemoryTransport struct {
	unimplementedTransport
	scheduler types.Scheduler
}

// NewMemoryTransport creates a new in-memory transport instance
func NewMemoryTransport(scheduler types.Scheduler) *MemoryTransport {
	return &MemoryTransport{
		scheduler: scheduler,
	}
}

func (t *MemoryTransport) SendGetJobRequest(ctx context.Context, req *types.GetJobRequest) (*types.GetJobResponse, error) {
	job, ok, err := t.scheduler.HandleGetJob(ctx, req.BuilderID)
	if err != nil {
		return nil, err
	}
	return &types.GetJobResponse{
		Job: job,
		OK:  ok,
	}, nil
}

func (t *MemoryTransport) SendCompleteJob(ctx context.Context, req *types.CompleteJobRequest) error {
	return t.scheduler.HandleCompleteJob(ctx, req.BuilderID, req.Job)
}

func (t *MemoryTransport) SendSyncJob(ctx context.Context, req *types.SyncJobRequest) error {
	return t.scheduler.HandleSyncJob(ctx, req.BuilderID, req.Job)
}
