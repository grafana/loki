package blockbuilder

import (
	"context"
	"fmt"
	"sync"
)

// TestBuilder implements Worker for testing
type TestBuilder struct {
	ID        string
	transport Transport
	mu        sync.Mutex
	jobs      map[string]*Job // Track jobs this builder has processed
}

// NewTestBuilder creates a new test builder instance
func NewTestBuilder(id string, transport Transport) *TestBuilder {
	return &TestBuilder{
		ID:        id,
		transport: transport,
		jobs:      make(map[string]*Job),
	}
}

func (b *TestBuilder) GetJob(ctx context.Context) (*Job, bool, error) {
	resp, err := b.transport.SendGetJobRequest(ctx, &GetJobRequest{
		BuilderID: b.ID,
	})
	if err != nil {
		return nil, false, err
	}
	if !resp.OK {
		return nil, false, nil
	}

	b.mu.Lock()
	b.jobs[resp.Job.ID] = resp.Job
	b.mu.Unlock()

	return resp.Job, true, nil
}

func (b *TestBuilder) CompleteJob(ctx context.Context, job *Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.jobs[job.ID]; !exists {
		return fmt.Errorf("job %s not found in builder %s", job.ID, b.ID)
	}

	err := b.transport.SendCompleteJob(ctx, &CompleteJobRequest{
		BuilderID: b.ID,
		Job:       job,
	})
	if err != nil {
		return err
	}

	delete(b.jobs, job.ID)
	return nil
}

func (b *TestBuilder) SyncJob(ctx context.Context, job *Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.jobs[job.ID]; !exists {
		return fmt.Errorf("job %s not found in builder %s", job.ID, b.ID)
	}

	return b.transport.SendSyncJob(ctx, &SyncJobRequest{
		BuilderID: b.ID,
		Job:       job,
	})
}
