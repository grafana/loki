package blockbuilder

import (
	"context"
)

// Scheduler interface defines the methods for scheduling jobs and managing worker pools.
type Scheduler interface {
	// HandleGetJob processes a job request from a block builder
	HandleGetJob(ctx context.Context, builderID string) (*Job, bool, error)
	// HandleCompleteJob processes a job completion notification
	HandleCompleteJob(ctx context.Context, builderID string, job *Job) error
	// HandleSyncJob processes a job sync request
	HandleSyncJob(ctx context.Context, builderID string, job *Job) error
}

// unimplementedScheduler provides default implementations that panic.
type unimplementedScheduler struct{}

func (s *unimplementedScheduler) HandleGetJob(ctx context.Context, builderID string) (*Job, bool, error) {
	panic("unimplemented")
}

func (s *unimplementedScheduler) HandleCompleteJob(ctx context.Context, builderID string, job *Job) error {
	panic("unimplemented")
}

func (s *unimplementedScheduler) HandleSyncJob(ctx context.Context, builderID string, job *Job) error {
	panic("unimplemented")
}

// SchedulerImpl implements the Scheduler interface
type SchedulerImpl struct {
	unimplementedScheduler
	queue *JobQueue
}

// NewScheduler creates a new scheduler instance
func NewScheduler(queue *JobQueue) *SchedulerImpl {
	return &SchedulerImpl{
		queue: queue,
	}
}

func (s *SchedulerImpl) HandleGetJob(ctx context.Context, builderID string) (*Job, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		return s.queue.Dequeue(builderID)
	}
}

func (s *SchedulerImpl) HandleCompleteJob(ctx context.Context, builderID string, job *Job) error {
	return s.queue.MarkComplete(job.ID, builderID)
}

func (s *SchedulerImpl) HandleSyncJob(ctx context.Context, builderID string, job *Job) error {
	return s.queue.SyncJob(job.ID, builderID, job)
}
