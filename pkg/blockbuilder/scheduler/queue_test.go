package scheduler

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

func newTestQueue() *JobQueue {
	return NewJobQueue(
		JobQueueConfig{
			LeaseDuration:            10 * time.Minute,
			LeaseExpiryCheckInterval: time.Minute,
		},
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)
}

func TestJobQueue_TransitionState(t *testing.T) {
	q := newTestQueue()
	offsets := types.Offsets{Min: 0, Max: 100}
	job := types.NewJob(1, offsets)
	jobID := job.ID()

	// Create initial job
	prevState, existed, err := q.TransitionAny(jobID, types.JobStatusPending, func() (*JobWithMetadata, error) {
		return NewJobWithMetadata(job, 1), nil
	})
	require.NoError(t, err)
	require.False(t, existed)
	require.Equal(t, types.JobStatusUnknown, prevState)

	// Test valid transition
	ok, err := q.TransitionState(jobID, types.JobStatusPending, types.JobStatusInProgress)
	require.NoError(t, err)
	require.True(t, ok)

	// Test invalid transition (wrong 'from' state)
	ok, err = q.TransitionState(jobID, types.JobStatusPending, types.JobStatusComplete)
	require.Error(t, err)
	require.False(t, ok)

	// Test non-existent job
	ok, err = q.TransitionState("non-existent", types.JobStatusPending, types.JobStatusInProgress)
	require.Error(t, err)
	require.False(t, ok)
}

func TestJobQueue_TransitionAny(t *testing.T) {
	q := newTestQueue()
	offsets := types.Offsets{Min: 0, Max: 100}
	job := types.NewJob(1, offsets)
	jobID := job.ID()

	// Test creation of new job
	prevState, existed, err := q.TransitionAny(jobID, types.JobStatusPending, func() (*JobWithMetadata, error) {
		return NewJobWithMetadata(job, 1), nil
	})
	require.NoError(t, err)
	require.False(t, existed)
	require.Equal(t, types.JobStatusUnknown, prevState)

	// Test transition of existing job
	prevState, existed, err = q.TransitionAny(jobID, types.JobStatusInProgress, nil)
	require.NoError(t, err)
	require.True(t, existed)
	require.Equal(t, types.JobStatusPending, prevState)

	// Test transition with nil createFn for non-existent job
	prevState, existed, err = q.TransitionAny("non-existent", types.JobStatusPending, nil)
	require.Error(t, err)
	require.False(t, existed)
	require.Equal(t, types.JobStatusUnknown, prevState)

	// Verify final status
	status, exists := q.Exists(jobID)
	require.True(t, exists)
	require.Equal(t, types.JobStatusInProgress, status)
}

func TestJobQueue_Dequeue(t *testing.T) {
	q := newTestQueue()

	// Add jobs with different priorities
	jobs := []struct {
		partition int32
		priority  int
	}{
		{1, 1},
		{2, 3},
		{3, 2},
	}

	var jobIDs []string
	for _, j := range jobs {
		job := types.NewJob(j.partition, types.Offsets{Min: 0, Max: 100})
		jobIDs = append(jobIDs, job.ID())
		_, _, err := q.TransitionAny(job.ID(), types.JobStatusPending, func() (*JobWithMetadata, error) {
			return NewJobWithMetadata(job, j.priority), nil
		})
		require.NoError(t, err)
	}

	// Dequeue should return highest priority job first
	job, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, jobIDs[1], job.ID()) // Priority 3

	job, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, jobIDs[2], job.ID()) // Priority 2

	job, ok = q.Dequeue()
	require.True(t, ok)
	require.Equal(t, jobIDs[0], job.ID()) // Priority 1

	// Queue should be empty now
	job, ok = q.Dequeue()
	require.False(t, ok)
	require.Nil(t, job)
}

func TestJobQueue_Lists(t *testing.T) {
	q := newTestQueue()

	// Add jobs in different states
	jobStates := map[int32]types.JobStatus{
		1: types.JobStatusPending,
		2: types.JobStatusPending,
		3: types.JobStatusInProgress,
		4: types.JobStatusInProgress,
		5: types.JobStatusComplete,
		6: types.JobStatusFailed,
	}

	for partition, status := range jobStates {
		job := types.NewJob(partition, types.Offsets{Min: 0, Max: 100})
		_, _, err := q.TransitionAny(job.ID(), types.JobStatusPending, func() (*JobWithMetadata, error) {
			return NewJobWithMetadata(job, 1), nil
		})
		require.NoError(t, err)

		if status != types.JobStatusPending {
			_, _, err = q.TransitionAny(job.ID(), status, nil)
			require.NoError(t, err)
		}
	}

	// Test ListPendingJobs
	pending := q.ListPendingJobs()
	require.Len(t, pending, 2)
	for _, j := range pending {
		require.Equal(t, types.JobStatusPending, j.Status)
	}

	// Test ListInProgressJobs
	inProgress := q.ListInProgressJobs()
	require.Len(t, inProgress, 2)
	for _, j := range inProgress {
		require.Equal(t, types.JobStatusInProgress, j.Status)
	}

	// Test ListCompletedJobs
	completed := q.ListCompletedJobs()
	require.Len(t, completed, 2) // completed and failed
	for _, j := range completed {
		require.Contains(t, []types.JobStatus{types.JobStatusComplete, types.JobStatusFailed}, j.Status)
	}
}

func TestJobQueue_LeaseExpiry(t *testing.T) {
	cfg := JobQueueConfig{
		LeaseDuration:            100 * time.Millisecond,
		LeaseExpiryCheckInterval: 10 * time.Millisecond,
	}
	q := NewJobQueue(cfg, log.NewNopLogger(), prometheus.NewRegistry())

	// Create and start a job
	job := types.NewJob(1, types.Offsets{Min: 0, Max: 100})
	jobID := job.ID()

	_, _, err := q.TransitionAny(jobID, types.JobStatusPending, func() (*JobWithMetadata, error) {
		return NewJobWithMetadata(job, 1), nil
	})
	require.NoError(t, err)

	// Move to in progress
	dequeued, ok := q.Dequeue()
	require.True(t, ok)
	require.Equal(t, jobID, dequeued.ID())

	// Wait for lease to expire
	time.Sleep(200 * time.Millisecond)
	err = q.requeueExpiredJobs()
	require.NoError(t, err)

	// Verify the job is back in pending
	status, exists := q.Exists(jobID)
	require.True(t, exists)
	require.Equal(t, types.JobStatusPending, status)

	// Check that it went through expired state
	completed := q.ListCompletedJobs()
	require.Len(t, completed, 1)
	require.Equal(t, types.JobStatusExpired, completed[0].Status)
}
