package builder

import (
	"context"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

var (
	_ types.Worker = unimplementedWorker{}
	_ types.Worker = &Worker{}
)

// unimplementedWorker provides default implementations for the Worker interface.
type unimplementedWorker struct{}

func (u unimplementedWorker) GetJob(_ context.Context) (*types.Job, bool, error) {
	panic("unimplemented")
}

func (u unimplementedWorker) CompleteJob(_ context.Context, _ *types.Job) error {
	panic("unimplemented")
}

func (u unimplementedWorker) SyncJob(_ context.Context, _ *types.Job) error {
	panic("unimplemented")
}

// Worker is the implementation of the Worker interface.
type Worker struct {
	unimplementedWorker
	transport types.Transport
	builderID string
}

// NewWorker creates a new Worker instance.
func NewWorker(builderID string, transport types.Transport) *Worker {
	return &Worker{
		transport: transport,
		builderID: builderID,
	}
}

func (w *Worker) GetJob(ctx context.Context) (*types.Job, bool, error) {
	resp, err := w.transport.SendGetJobRequest(ctx, &types.GetJobRequest{
		BuilderID: w.builderID,
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Job, resp.OK, nil
}

func (w *Worker) CompleteJob(ctx context.Context, job *types.Job) error {
	return w.transport.SendCompleteJob(ctx, &types.CompleteJobRequest{
		BuilderID: w.builderID,
		Job:       job,
	})
}

func (w *Worker) SyncJob(ctx context.Context, job *types.Job) error {
	return w.transport.SendSyncJob(ctx, &types.SyncJobRequest{
		BuilderID: w.builderID,
		Job:       job,
	})
}
