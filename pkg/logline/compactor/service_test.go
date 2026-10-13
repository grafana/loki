package compactor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline"
	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
)

// newTestService creates a compactor Service suitable for lifecycle tests.
// A nil store is acceptable here because tests do not execute compaction jobs.
func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := Config{Compaction: logline.CompactionConfig{
		SchedulerAddress: "http://localhost:9999",
		CompactorID:      "test-compactor",
	}}
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	svc, err := New(nil, cfg, logger, reg)
	require.NoError(t, err)
	return svc
}

func TestNew(t *testing.T) {
	svc := newTestService(t)
	require.NotNil(t, svc)
	require.NotNil(t, svc.Service)
}

func TestNew_InvalidConfig(t *testing.T) {
	cfg := Config{} // missing SchedulerAddress
	_, err := New(nil, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.Error(t, err)
}

func TestServiceLifecycle(t *testing.T) {
	svc := newTestService(t)

	ctx := context.Background()
	err := svc.StartAsync(ctx)
	require.NoError(t, err)

	err = svc.AwaitRunning(ctx)
	require.NoError(t, err)

	svc.StopAsync()
	err = svc.AwaitTerminated(ctx)
	require.NoError(t, err)
}

func TestServiceStopBeforeStart(t *testing.T) {
	svc := newTestService(t)

	// Stop without starting should work cleanly.
	svc.StopAsync()
	err := svc.AwaitTerminated(context.Background())
	require.NoError(t, err)
}

func TestServiceRunningContext(t *testing.T) {
	svc := newTestService(t)

	ctx := context.Background()
	err := svc.StartAsync(ctx)
	require.NoError(t, err)

	err = svc.AwaitRunning(ctx)
	require.NoError(t, err)

	// Give the run loop a moment to spin before stopping.
	time.Sleep(10 * time.Millisecond)

	svc.StopAsync()
	err = svc.AwaitTerminated(ctx)
	require.NoError(t, err)
}

func TestAbandonCurrentJob_CallsScheduler(t *testing.T) {
	svc := newTestService(t)

	var called bool
	var gotCompactorID, gotJobID string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotCompactorID = r.URL.Query().Get(schedulerapi.ParamWorkerID)
		gotJobID = r.URL.Query().Get(schedulerapi.ParamJobID)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	svc.cfg.Compaction.SchedulerAddress = ts.URL
	svc.job.setRunning("job123")

	svc.abandonCurrentJob()

	require.True(t, called, "abandon endpoint should have been called")
	require.Equal(t, "test-compactor", gotCompactorID)
	require.Equal(t, "job123", gotJobID)
}

func TestAbandonCurrentJob_NoJob(t *testing.T) {
	svc := newTestService(t)

	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	svc.cfg.Compaction.SchedulerAddress = ts.URL
	// No current job set — should be a no-op.
	svc.abandonCurrentJob()

	require.False(t, called, "abandon should not be called when no job is in-flight")
}

func TestAbandonCurrentJob_RetriesOnFailure(t *testing.T) {
	svc := newTestService(t)

	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	svc.cfg.Compaction.SchedulerAddress = ts.URL
	svc.job.setRunning("job456")

	svc.abandonCurrentJob()

	require.Equal(t, 3, attempts, "should retry until success")
}

func TestAbandonCurrentJob_DoesNotRetryOn4xx(t *testing.T) {
	svc := newTestService(t)

	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusConflict) // 409 — permanent
	}))
	defer ts.Close()

	svc.cfg.Compaction.SchedulerAddress = ts.URL
	svc.job.setRunning("job789")

	svc.abandonCurrentJob()

	require.Equal(t, 1, attempts, "should not retry on 4xx")
}

func TestExecuteJob_TracksCurrentJob(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "aaaaaaaaaaaaaaaa", now)
	meta2 := writeRealIndex(t, s, "2026-02-25", "bbbbbbbbbbbbbbbb", now)

	job := &schedulerapi.JobResponse{
		ID:        "trackjob",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// executeJob does not mutate job lifecycle state; runLoop owns it.
	require.Equal(t, "", svc.job.runningID())
	completed := svc.executeJob(context.Background(), job)
	require.True(t, completed)
	require.Equal(t, "", svc.job.runningID())
}

func TestExecuteJob_ContextCancelled_PreservesCurrentJob(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "aaaaaaaaaaaaaaaa", now)
	meta2 := writeRealIndex(t, s, "2026-02-25", "bbbbbbbbbbbbbbbb", now)

	job := &schedulerapi.JobResponse{
		ID:        "shutdownjob",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// Cancel the context before calling executeJob to simulate shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// runLoop sets current job before executeJob.
	svc.job.setRunning(job.ID)
	completed := svc.executeJob(ctx, job)
	require.False(t, completed)
	// During shutdown with incomplete job, runLoop keeps currentJobID set so
	// stopping() can abandon it with the scheduler.
	require.Equal(t, "shutdownjob", svc.job.runningID())
}

func TestExecuteJob_SuccessThenShutdown_DoesNotAbandon(t *testing.T) {
	// On success, runLoop should transition running -> completed pending ack.
	// That clears current job visibility and prevents abandon on shutdown.
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "aaaaaaaaaaaaaaaa", now)
	meta2 := writeRealIndex(t, s, "2026-02-25", "bbbbbbbbbbbbbbbb", now)

	job := &schedulerapi.JobResponse{
		ID:        "successjob",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	ctx := context.Background()
	svc.job.setRunning(job.ID)
	completed := svc.executeJob(ctx, job)
	require.True(t, completed)
	svc.job.markCompletedPendingAck(job.ID)

	// Compaction succeeded.
	require.Equal(t, "successjob", svc.job.consumeCompletedID())
	// currentJobID must be cleared — no abandon should fire even if ctx
	// were cancelled after this point.
	require.Equal(t, "", svc.job.runningID(), "completed job must not be abandonable")
}

func TestExecuteJob_Success_SetsLastCompletedJobID(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	now := time.Now().UTC().Truncate(time.Second)
	meta1 := writeRealIndex(t, s, "2026-02-25", "aaaaaaaaaaaaaaaa", now)
	meta2 := writeRealIndex(t, s, "2026-02-25", "bbbbbbbbbbbbbbbb", now)

	job := &schedulerapi.JobResponse{
		ID:        "successjob",
		SourceIDs: []string{meta1.ID(), meta2.ID()},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	require.Equal(t, "", svc.job.consumeCompletedID())
	svc.job.setRunning(job.ID)
	completed := svc.executeJob(context.Background(), job)
	require.True(t, completed)
	svc.job.markCompletedPendingAck(job.ID)
	require.Equal(t, "successjob", svc.job.consumeCompletedID())
}

func TestExecuteJob_Failure_DoesNotSetLastCompletedJobID(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestCompactorStore(t, bucket)
	svc := newTestCompactor(t, s)

	// Missing source index → compact will fail.
	job := &schedulerapi.JobResponse{
		ID:        "failjob",
		SourceIDs: []string{"2026-02-25/does_not_exist"},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	svc.job.setRunning(job.ID)
	completed := svc.executeJob(context.Background(), job)
	require.False(t, completed)
	require.Equal(t, "", svc.job.consumeCompletedID())
	require.Equal(t, "failjob", svc.job.runningID())
}
