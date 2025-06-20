package jobqueue

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	compactor_grpc "github.com/grafana/loki/v3/pkg/compactor/client/grpc"
)

// mockBuilder implements the Builder interface for testing
type mockBuilder struct {
	jobsToBuild   []*compactor_grpc.Job
	jobsSentCount atomic.Int32
	jobsSucceeded atomic.Int32
	jobsFailed    atomic.Int32
}

func (m *mockBuilder) OnJobResponse(res *compactor_grpc.JobResult) {
	if res.Error != "" {
		m.jobsFailed.Inc()
	} else {
		m.jobsSucceeded.Inc()
	}
}

func (m *mockBuilder) BuildJobs(ctx context.Context, jobsChan chan<- *compactor_grpc.Job) {
	for _, job := range m.jobsToBuild {
		select {
		case <-ctx.Done():
			return
		case jobsChan <- job:
			m.jobsSentCount.Inc()
		}
	}

	// Keep running until context is cancelled
	<-ctx.Done()
}

func setupGRPC(t *testing.T, q *Queue) (*grpc.ClientConn, func()) {
	buffer := 101024 * 1024
	lis := bufconn.Listen(buffer)

	baseServer := grpc.NewServer()

	compactor_grpc.RegisterJobQueueServer(baseServer, q)
	go func() {
		if err := baseServer.Serve(lis); err != nil {
			t.Logf("Failed to serve: %v", err)
		}
	}()

	// nolint:staticcheck // compactor_grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(context.Background(), "",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	closer := func() {
		require.NoError(t, lis.Close())
		baseServer.GracefulStop()
	}

	return conn, closer
}

func TestQueue_RegisterBuilder(t *testing.T) {
	q := NewQueue()
	builder := &mockBuilder{}

	// Register builder successfully
	err := q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, builder)
	require.NoError(t, err)

	// Try to register same builder type again
	err = q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, builder)
	require.ErrorIs(t, err, ErrJobTypeAlreadyRegistered)
}

func TestQueue_Loop(t *testing.T) {
	q := NewQueue()

	conn, closer := setupGRPC(t, q)
	defer closer()

	// Create a couple of test jobs
	var jobs []*compactor_grpc.Job
	for i := 0; i < 3; i++ {
		jobs = append(jobs, &compactor_grpc.Job{
			Id:   fmt.Sprintf("test-job-%d", i),
			Type: compactor_grpc.JOB_TYPE_DELETION,
		})
	}

	builder := &mockBuilder{
		jobsToBuild: jobs,
	}

	require.NoError(t, q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, builder))
	require.NoError(t, q.Start(context.Background()))

	// Dequeue the job
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client1 := compactor_grpc.NewJobQueueClient(conn)
	client1Stream, err := client1.Loop(ctx)
	require.NoError(t, err)

	resp, err := client1Stream.Recv()
	require.NoError(t, err)
	require.Equal(t, jobs[0], resp)

	// Verify job is tracked as being processed
	q.processingJobsMtx.RLock()
	require.Equal(t, 1, len(q.processingJobs))
	require.Equal(t, jobs[0], q.processingJobs[jobs[0].Id].job)
	require.Equal(t, 2, q.processingJobs[jobs[0].Id].attemptsLeft)
	q.processingJobsMtx.RUnlock()

	// another Recv call on client1Stream without calling the Send call should get blocked
	client1ReceivedNextJob := make(chan struct{})
	go func() {
		resp, err := client1Stream.Recv()
		require.NoError(t, err)
		require.Equal(t, jobs[2], resp)
		client1ReceivedNextJob <- struct{}{}
	}()

	// make a new client and try getting a job
	client2 := compactor_grpc.NewJobQueueClient(conn)
	client2Stream, err := client2.Loop(ctx)
	require.NoError(t, err)

	resp, err = client2Stream.Recv()
	require.NoError(t, err)
	require.Equal(t, jobs[1], resp)

	// Verify both the jobs are tracked as being processed
	q.processingJobsMtx.RLock()
	require.Equal(t, 2, len(q.processingJobs))
	require.Equal(t, jobs[0], q.processingJobs[jobs[0].Id].job)
	require.Equal(t, 2, q.processingJobs[jobs[0].Id].attemptsLeft)
	require.Equal(t, jobs[1], q.processingJobs[jobs[1].Id].job)
	require.Equal(t, 2, q.processingJobs[jobs[1].Id].attemptsLeft)
	q.processingJobsMtx.RUnlock()

	// sending a response on client1Stream should get it unblocked to Recv the next job
	err = client1Stream.Send(&compactor_grpc.JobResult{
		JobId:   jobs[0].Id,
		JobType: jobs[0].Type,
		Result:  []byte("test-result"),
	})
	require.NoError(t, err)

	<-client1ReceivedNextJob

	// Verify that now job1 & job2 are tracked as being processed
	q.processingJobsMtx.RLock()
	require.Equal(t, 2, len(q.processingJobs))
	require.Equal(t, jobs[1], q.processingJobs[jobs[1].Id].job)
	require.Equal(t, 2, q.processingJobs[jobs[1].Id].attemptsLeft)
	require.Equal(t, jobs[2], q.processingJobs[jobs[2].Id].job)
	require.Equal(t, 2, q.processingJobs[jobs[2].Id].attemptsLeft)
	q.processingJobsMtx.RUnlock()
}

func TestQueue_ReportJobResult(t *testing.T) {
	q := newQueue(time.Second)
	require.NoError(t, q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, &mockBuilder{}))

	// Create a test job
	job := &compactor_grpc.Job{
		Id:   "test-job",
		Type: compactor_grpc.JOB_TYPE_DELETION,
	}

	// Add job to processing jobs
	q.processingJobsMtx.Lock()
	q.processingJobs[job.Id] = &processingJob{
		job:          job,
		dequeued:     time.Now(),
		attemptsLeft: 2,
	}
	q.processingJobsMtx.Unlock()

	// Test successful response
	err := q.reportJobResult(&compactor_grpc.JobResult{
		JobId:   job.Id,
		JobType: job.Type,
	})
	require.NoError(t, err)

	// Verify job is removed from processing jobs
	q.processingJobsMtx.RLock()
	_, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.False(t, exists)

	// Test error response with retry
	job.Id = "retry-job"
	q.processingJobsMtx.Lock()
	q.processingJobs[job.Id] = &processingJob{
		job:          job,
		dequeued:     time.Now(),
		attemptsLeft: 2,
	}
	q.processingJobsMtx.Unlock()

	err = q.reportJobResult(&compactor_grpc.JobResult{
		JobId:   job.Id,
		JobType: job.Type,
		Error:   "test error",
	})
	require.NoError(t, err)

	// Verify job is requeued with timeout
	select {
	case requeuedJob := <-q.queue:
		require.Equal(t, job, requeuedJob)
	case <-time.After(time.Minute):
		t.Fatal("job was not requeued")
	}

	// Verify retry count is incremented
	q.processingJobsMtx.RLock()
	pj, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.True(t, exists)
	require.Equal(t, 1, pj.attemptsLeft)
}

func TestQueue_JobTimeout(t *testing.T) {
	q := newQueue(50 * time.Millisecond)
	q.jobTimeout = 100 * time.Millisecond // Short timeout for testing

	// Create a test job
	job := &compactor_grpc.Job{
		Id:   "test-job",
		Type: compactor_grpc.JOB_TYPE_DELETION,
	}

	// Add job to processing jobs with old dequeued time
	q.processingJobsMtx.Lock()
	q.processingJobs[job.Id] = &processingJob{
		job:          job,
		dequeued:     time.Now().Add(-200 * time.Millisecond),
		attemptsLeft: 2,
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
	pj, exists := q.processingJobs[job.Id]
	q.processingJobsMtx.RUnlock()
	require.True(t, exists)
	require.Equal(t, 1, pj.attemptsLeft)
}

func TestQueue_Close(t *testing.T) {
	q := NewQueue()

	// Close the queue
	q.Close()

	// Verify queue is closed
	require.True(t, q.closed.Load())

	// Verify channel is closed
	select {
	case _, ok := <-q.queue:
		require.False(t, ok)
	default:
		t.Fatal("queue channel should be closed")
	}
}
