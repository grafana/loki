package scheduler

import (
	"context"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

var (
	_ types.Scheduler = unimplementedScheduler{}
	_ types.Scheduler = &QueueScheduler{}
)

// unimplementedScheduler provides default implementations that panic.
type unimplementedScheduler struct{}

func (s unimplementedScheduler) HandleGetJob(_ context.Context, _ string) (*types.Job, bool, error) {
	panic("unimplemented")
}

func (s unimplementedScheduler) HandleCompleteJob(_ context.Context, _ string, _ *types.Job) error {
	panic("unimplemented")
}

func (s unimplementedScheduler) HandleSyncJob(_ context.Context, _ string, _ *types.Job) error {
	panic("unimplemented")
}

// QueueScheduler implements the Scheduler interface
type QueueScheduler struct {
	queue *JobQueue
}

// NewScheduler creates a new scheduler instance
func NewScheduler(queue *JobQueue) *QueueScheduler {
	return &QueueScheduler{
		queue: queue,
	}
}

func (s *QueueScheduler) HandleGetJob(ctx context.Context, builderID string) (*types.Job, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		return s.queue.Dequeue(builderID)
	}
}

func (s *QueueScheduler) HandleCompleteJob(_ context.Context, builderID string, job *types.Job) error {
	return s.queue.MarkComplete(job.ID, builderID)
}

func (s *QueueScheduler) HandleSyncJob(_ context.Context, builderID string, job *types.Job) error {
	return s.queue.SyncJob(job.ID, builderID, job)
}
