package scheduler

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

func TestJobQueue_SyncJob(t *testing.T) {
	t.Run("non-existent to in-progress", func(t *testing.T) {
		q := NewJobQueue(log.NewNopLogger(), nil)
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
		q := NewJobQueue(log.NewNopLogger(), nil)
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
		q := NewJobQueue(log.NewNopLogger(), nil)
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
		q := NewJobQueue(log.NewNopLogger(), nil)
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
		q := NewJobQueue(log.NewNopLogger(), nil)
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
		q := NewJobQueue(log.NewNopLogger(), nil)
		logger := &testLogger{t: t}
		q.logger = logger

		q.MarkComplete("non-existent", types.JobStatusComplete)
		// Should log error but not panic
	})

	t.Run("already completed job", func(t *testing.T) {
		q := NewJobQueue(log.NewNopLogger(), nil)
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

// testLogger implements log.Logger for testing
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Log(keyvals ...interface{}) error {
	l.t.Log(keyvals...)
	return nil
}
