package scheduler

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

var testQueueCfg = JobQueueConfig{}

func TestJobQueue_SyncJob(t *testing.T) {
	t.Run("non-existent to in-progress", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
		jobID := job.ID()

		beforeSync := time.Now()
		q.SyncJob(jobID, job)
		afterSync := time.Now()

		// Verify job is in in-progress map
		jobMeta, ok := q.inProgress[jobID]
		require.True(t, ok, "job should be in in-progress map")
		require.Equal(t, types.JobStatusInProgress, jobMeta.Status)
		require.True(t, jobMeta.StartTime.After(beforeSync) || jobMeta.StartTime.Equal(beforeSync))
		require.True(t, jobMeta.StartTime.Before(afterSync) || jobMeta.StartTime.Equal(afterSync))
	})

	t.Run("pending to in-progress", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})

		// Start with pending job
		err := q.Enqueue(job, DefaultPriority)
		require.NoError(t, err)

		beforeSync := time.Now()
		q.SyncJob(job.ID(), job)
		afterSync := time.Now()

		// Verify job moved from pending to in-progress
		_, ok := q.pending.Lookup(job.ID())
		require.False(t, ok, "job should not be in pending queue")

		jobMeta, ok := q.inProgress[job.ID()]
		require.True(t, ok, "job should be in in-progress map")
		require.Equal(t, types.JobStatusInProgress, jobMeta.Status)
		require.True(t, jobMeta.UpdateTime.After(beforeSync) || jobMeta.UpdateTime.Equal(beforeSync))
		require.True(t, jobMeta.UpdateTime.Before(afterSync) || jobMeta.UpdateTime.Equal(afterSync))
	})

	t.Run("already in-progress", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})

		// First sync to put in in-progress
		q.SyncJob(job.ID(), job)
		firstUpdate := q.inProgress[job.ID()].UpdateTime

		time.Sleep(time.Millisecond) // Ensure time difference
		beforeSecondSync := time.Now()
		q.SyncJob(job.ID(), job)
		afterSecondSync := time.Now()

		jobMeta := q.inProgress[job.ID()]
		require.True(t, jobMeta.UpdateTime.After(firstUpdate), "UpdateTime should be updated")
		require.True(t, jobMeta.UpdateTime.After(beforeSecondSync) || jobMeta.UpdateTime.Equal(beforeSecondSync))
		require.True(t, jobMeta.UpdateTime.Before(afterSecondSync) || jobMeta.UpdateTime.Equal(afterSecondSync))
	})
}

func TestJobQueue_MarkComplete(t *testing.T) {
	t.Run("in-progress to complete", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})

		// Start with in-progress job
		q.SyncJob(job.ID(), job)

		beforeComplete := time.Now()
		q.MarkComplete(job.ID(), types.JobStatusComplete)
		afterComplete := time.Now()

		// Verify job moved to completed buffer
		var foundJob *JobWithMetadata
		q.completed.Lookup(func(j *JobWithMetadata) bool {
			if j.ID() == job.ID() {
				foundJob = j
				return true
			}
			return false
		})
		require.NotNil(t, foundJob, "job should be in completed buffer")
		require.Equal(t, types.JobStatusComplete, foundJob.Status)
		require.True(t, foundJob.UpdateTime.After(beforeComplete) || foundJob.UpdateTime.Equal(beforeComplete))
		require.True(t, foundJob.UpdateTime.Before(afterComplete) || foundJob.UpdateTime.Equal(afterComplete))

		// Verify removed from in-progress
		_, ok := q.inProgress[job.ID()]
		require.False(t, ok, "job should not be in in-progress map")
	})

	t.Run("pending to complete", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})

		// Start with pending job
		err := q.Enqueue(job, DefaultPriority)
		require.NoError(t, err)

		q.MarkComplete(job.ID(), types.JobStatusComplete)

		// Verify job not in pending
		_, ok := q.pending.Lookup(job.ID())
		require.False(t, ok, "job should not be in pending queue")

		// Verify job in completed buffer
		var foundJob *JobWithMetadata
		q.completed.Lookup(func(j *JobWithMetadata) bool {
			if j.ID() == job.ID() {
				foundJob = j
				return true
			}
			return false
		})
		require.NotNil(t, foundJob, "job should be in completed buffer")
		require.Equal(t, types.JobStatusComplete, foundJob.Status)
	})

	t.Run("non-existent job", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		logger := &testLogger{t: t}
		q.logger = logger

		q.MarkComplete("non-existent", types.JobStatusComplete)
		// Should log error but not panic
	})

	t.Run("already completed job", func(t *testing.T) {
		q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)
		logger := &testLogger{t: t}
		q.logger = logger

		job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
		q.SyncJob(job.ID(), job)
		q.MarkComplete(job.ID(), types.JobStatusComplete)

		// Try to complete again
		q.MarkComplete(job.ID(), types.JobStatusComplete)
		// Should log error but not panic
	})
}

func TestJobQueue_Enqueue(t *testing.T) {
	q := NewJobQueue(testQueueCfg, log.NewNopLogger(), nil)

	job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})

	beforeComplete := time.Now()
	err := q.Enqueue(job, 1)
	afterComplete := time.Now()
	require.NoError(t, err)

	status, ok := q.Exists(job)
	require.True(t, ok, "job should exist")
	require.Equal(t, types.JobStatusPending, status)

	// Verify job in pending queue
	foundJob, ok := q.pending.Lookup(job.ID())
	require.True(t, ok, "job should be in pending queue")
	require.Equal(t, job, foundJob.Job)
	require.Equal(t, 1, foundJob.Priority)
	require.True(t, foundJob.StartTime.IsZero())
	require.True(t, foundJob.UpdateTime.After(beforeComplete) || foundJob.UpdateTime.Equal(beforeComplete))
	require.True(t, foundJob.UpdateTime.Before(afterComplete) || foundJob.UpdateTime.Equal(afterComplete))

	// allow enqueueing of job with same ID if expired
	job2 := types.NewJob(2, types.Offsets{Min: 100, Max: 200})
	q.statusMap[job2.ID()] = types.JobStatusExpired

	err = q.Enqueue(job2, 2)
	require.NoError(t, err)

	status, ok = q.Exists(job2)
	require.True(t, ok, "job should exist")
	require.Equal(t, types.JobStatusPending, status)

	// Verify job2 in pending queue
	foundJob, ok = q.pending.Lookup(job2.ID())
	require.True(t, ok, "job2 should be in pending queue")
	require.Equal(t, job2, foundJob.Job)
	require.Equal(t, 2, foundJob.Priority)

	// do not allow enqueueing of job with same ID if not expired
	job3 := types.NewJob(3, types.Offsets{Min: 120, Max: 230})
	q.statusMap[job3.ID()] = types.JobStatusInProgress

	err = q.Enqueue(job3, DefaultPriority)
	require.Error(t, err)
}

func TestJobQueue_RequeueExpiredJobs(t *testing.T) {
	q := NewJobQueue(JobQueueConfig{
		LeaseDuration: 5 * time.Minute,
	}, log.NewNopLogger(), nil)

	job1 := &JobWithMetadata{
		Job:        types.NewJob(1, types.Offsets{Min: 100, Max: 200}),
		Priority:   1,
		Status:     types.JobStatusInProgress,
		StartTime:  time.Now().Add(-time.Hour),
		UpdateTime: time.Now().Add(-time.Minute),
	}
	// expired job
	job2 := &JobWithMetadata{
		Job:        types.NewJob(2, types.Offsets{Min: 300, Max: 400}),
		Priority:   2,
		Status:     types.JobStatusInProgress,
		StartTime:  time.Now().Add(-time.Hour),
		UpdateTime: time.Now().Add(-6 * time.Minute),
	}

	q.inProgress[job1.ID()] = job1
	q.inProgress[job2.ID()] = job2
	q.statusMap[job1.ID()] = types.JobStatusInProgress
	q.statusMap[job2.ID()] = types.JobStatusInProgress

	beforeRequeue := time.Now()
	err := q.requeueExpiredJobs()
	require.NoError(t, err)

	status, ok := q.statusMap[job1.ID()]
	require.True(t, ok)
	require.Equal(t, types.JobStatusInProgress, status)

	got, ok := q.inProgress[job1.ID()]
	require.True(t, ok)
	require.Equal(t, job1, got)

	status, ok = q.statusMap[job2.ID()]
	require.True(t, ok)
	require.Equal(t, types.JobStatusPending, status)

	got, ok = q.pending.Lookup(job2.ID())
	require.True(t, ok)
	require.Equal(t, job2.Job, got.Job)
	require.Equal(t, types.JobStatusPending, got.Status)
	require.Equal(t, job2.Priority, got.Priority)
	require.True(t, got.StartTime.IsZero())
	require.True(t, got.UpdateTime.After(beforeRequeue) || got.UpdateTime.Equal(beforeRequeue))

	require.Equal(t, 1, q.completed.Len())
	got, ok = q.completed.Pop()
	require.True(t, ok)
	job2.Status = types.JobStatusExpired
	require.Equal(t, job2, got)
}

// testLogger implements log.Logger for testing
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Log(keyvals ...interface{}) error {
	l.t.Log(keyvals...)
	return nil
}
