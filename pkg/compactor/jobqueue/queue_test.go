package jobqueue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockBuilder implements the Builder interface for testing
type mockBuilder struct {
	jobsToBuild []*Job
	buildErr    error
}

func (m *mockBuilder) OnJobResponse(_ *JobResponse) {}

func (m *mockBuilder) BuildJobs(ctx context.Context, jobsChan chan<- *Job) error {
	if m.buildErr != nil {
		return m.buildErr
	}

	for _, job := range m.jobsToBuild {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobsChan <- job:
		}
	}

	// Keep running until context is cancelled
	<-ctx.Done()
	return ctx.Err()
}

func TestQueue_RegisterBuilder(t *testing.T) {
	q := New()
	builder := &mockBuilder{}

	// Register builder successfully
	err := q.RegisterBuilder(JOB_TYPE_DELETION, builder)
	require.NoError(t, err)

	// Try to register same builder type again
	err = q.RegisterBuilder(JOB_TYPE_DELETION, builder)
	require.ErrorIs(t, err, ErrBuilderAlreadyRegistered)
}

func TestQueue_Dequeue(t *testing.T) {
	q := New()
	ctx := context.Background()

	// Create a test job
	job := &Job{
		Id:   "test-job",
		Type: JOB_TYPE_DELETION,
	}

	// Enqueue the job
	select {
	case q.queue <- job:
		// Successfully enqueued
	case <-time.After(time.Second):
		t.Fatal("failed to enqueue job")
	}

	// Dequeue the job
	resp, err := q.Dequeue(ctx, &DequeueRequest{})
	require.NoError(t, err)
	require.Equal(t, job, resp.Job)

	// Verify job is tracked as being processed
	q.processingJobsMtx.RLock()
	pj, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.True(t, exists)
	require.Equal(t, job, pj.job)
	require.Equal(t, 0, pj.retryCount)
}

func TestQueue_ReportJobResponse(t *testing.T) {
	q := New()
	ctx := context.Background()

	// Create a test job
	job := &Job{
		Id:   "test-job",
		Type: JOB_TYPE_DELETION,
	}

	// Add job to processing jobs
	q.processingJobsMtx.Lock()
	q.processingJobs[job.Id] = &processingJob{
		job:        job,
		dequeued:   time.Now(),
		retryCount: 0,
	}
	q.processingJobsMtx.Unlock()

	// Test successful response
	resp, err := q.ReportJobResponse(ctx, &ReportJobResponseRequest{
		Response: &JobResponse{
			JobId:   job.Id,
			JobType: job.Type,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify job is removed from processing jobs
	q.processingJobsMtx.RLock()
	_, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.False(t, exists)

	// Test error response with retry
	job.Id = "retry-job"
	q.processingJobsMtx.Lock()
	q.processingJobs[job.Id] = &processingJob{
		job:        job,
		dequeued:   time.Now(),
		retryCount: 0,
	}
	q.processingJobsMtx.Unlock()

	resp, err = q.ReportJobResponse(ctx, &ReportJobResponseRequest{
		Response: &JobResponse{
			JobId:   job.Id,
			JobType: job.Type,
			Error:   "test error",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify job is requeued with timeout
	select {
	case requeuedJob := <-q.queue:
		require.Equal(t, job, requeuedJob)
	case <-time.After(time.Second):
		t.Fatal("job was not requeued")
	}

	// Verify retry count is incremented
	q.processingJobsMtx.RLock()
	pj, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.True(t, exists)
	require.Equal(t, 1, pj.retryCount)
}

func TestQueue_JobTimeout(t *testing.T) {
	q := newQueue(50 * time.Millisecond)
	q.jobTimeout = 100 * time.Millisecond // Short timeout for testing

	// Create a test job
	job := &Job{
		Id:   "test-job",
		Type: JOB_TYPE_DELETION,
	}

	// Add job to processing jobs with old dequeued time
	q.processingJobsMtx.Lock()
	q.processingJobs[job.Id] = &processingJob{
		job:        job,
		dequeued:   time.Now().Add(-200 * time.Millisecond),
		retryCount: 0,
	}
	q.processingJobsMtx.Unlock()

	// Wait for timeout checker to run
	time.Sleep(100 * time.Millisecond)

	// Verify job is requeued
	select {
	case requeuedJob := <-q.queue:
		require.Equal(t, job, requeuedJob)
	case <-time.After(time.Second):
		t.Fatal("job was not requeued after timeout")
	}

	// Verify job is removed from processing jobs
	q.processingJobsMtx.RLock()
	_, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.False(t, exists)
}

func TestQueue_StartStop(t *testing.T) {
	q := New()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Create a builder that returns an error
	builder := &mockBuilder{
		buildErr: errors.New("test error"),
	}

	// Register and start the builder
	err := q.RegisterBuilder(JOB_TYPE_DELETION, builder)
	require.NoError(t, err)

	err = q.Start(ctx)
	require.NoError(t, err)

	// Wait for context cancellation
	<-ctx.Done()

	// Stop the queue
	err = q.Stop()
	require.NoError(t, err)
}

func TestQueue_Close(t *testing.T) {
	q := New()

	// Close the queue
	q.Close()

	// Verify queue is closed
	require.True(t, q.closed)

	// Verify channel is closed
	select {
	case _, ok := <-q.queue:
		require.False(t, ok)
	default:
		t.Fatal("queue channel should be closed")
	}
}
