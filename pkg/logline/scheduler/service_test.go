package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/logline"
	schedulerapi "github.com/grafana/loki/v4/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v4/pkg/logline/store"
)

// stubLimits implements limits.Limits for tests.
type stubLimits struct {
	retentionPeriod time.Duration
}

func (s stubLimits) RetentionPeriod() time.Duration { return s.retentionPeriod }

// newTestService creates a Service backed by an in-memory store and temp dir.
// The jobStore is also created so tests can exercise service methods without
// running the full service lifecycle.
func newTestService(t *testing.T) (*Service, *store.Store, *objstore.InMemBucket) {
	t.Helper()

	bucket := objstore.NewInMemBucket()
	s, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	// Keep a tiny max size so small stub indexes still form deterministic pairs
	// in tests that assume 2-source jobs.
	cfg := testConfig(t, 8)

	svc, err := New(s, stubLimits{}, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	svc.longPollTimeout = 50 * time.Millisecond

	js, err := newJobStore(cfg.Compaction.JobStateDir, log.NewNopLogger())
	require.NoError(t, err)
	svc.jobs = js

	return svc, s, bucket
}

// testConfig returns a scheduler config with a fresh job state directory.
func testConfig(t *testing.T, maxJobSize flagext.Bytes) Config {
	t.Helper()
	return Config{
		Compaction: logline.CompactionConfig{
			JobTimeout:  10 * time.Minute,
			MaxJobSize:  maxJobSize,
			JobStateDir: t.TempDir(),
		},
	}
}

// populateBucket writes leaf indexes for the given hashes to an in-memory bucket.
func populateBucket(t *testing.T, bucket *objstore.InMemBucket, date string, hashes ...string) {
	t.Helper()
	for _, hash := range hashes {
		uploadMeta(t, bucket, store.Meta{
			Date:        date,
			Hash:        hash,
			Version:     "v3",
			MinLogTs:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			MaxLogTs:    time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
			MinRecordTs: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			MaxRecordTs: time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
		})
	}
}

// populateMergedBucket writes a merged index that covers the given source IDs.
func populateMergedBucket(t *testing.T, bucket *objstore.InMemBucket, date, hash string, compactedFrom []string) {
	t.Helper()
	uploadMeta(t, bucket, store.Meta{
		Date:          date,
		Hash:          hash,
		Version:       "v3",
		MinLogTs:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxLogTs:      time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
		MinRecordTs:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxRecordTs:   time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
		CompactedFrom: compactedFrom,
		CreatedAt:     time.Now(),
	})
}

// uploadMeta marshals a Meta and uploads both the index stub and meta.json.
func uploadMeta(t *testing.T, bucket *objstore.InMemBucket, m store.Meta) {
	t.Helper()
	uploadMetaWithSize(t, bucket, m, 4) // "stub" is 4 bytes
}

// uploadMetaWithSize uploads an index of exactly sizeBytes and its meta.json.
func uploadMetaWithSize(t *testing.T, bucket *objstore.InMemBucket, m store.Meta, sizeBytes int) {
	t.Helper()
	ctx := context.Background()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	require.NoError(t, bucket.Upload(ctx, m.IndexPath(), bytes.NewReader(make([]byte, sizeBytes))))
	require.NoError(t, bucket.Upload(ctx, m.MetaPath(), bytes.NewReader(data)))
}

// pollStore calls the store's poll method directly for test synchronicity.
// It clears the known-metas cache before polling so that any meta.json mutations
// made by the test (e.g., backdating CreatedAt) are picked up as a full re-fetch.
func pollStore(t *testing.T, s *store.Store) {
	t.Helper()
	s.ClearKnownMetas()
	require.NoError(t, s.Poll(context.Background()))
}

func TestService_New_InvalidConfig(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	// A zero max size is invalid.
	cfg := testConfig(t, 0)
	_, err = New(s, stubLimits{}, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.Error(t, err)

	// The job state directory is required.
	cfg = testConfig(t, 8)
	cfg.Compaction.JobStateDir = ""
	_, err = New(s, stubLimits{}, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.Error(t, err)
}

func TestService_AssignJob_NoWork(t *testing.T) {
	svc, _, _ := newTestService(t)

	// No indexes in store → no work available.
	job := svc.assignJob("worker1")
	require.Nil(t, job)
}

func TestService_AssignJob_WithWork(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("worker1")
	require.NotNil(t, job)
	require.Equal(t, "worker1", job.WorkerID)
	require.Len(t, job.SourceIDs, 2)
	require.NotZero(t, job.ExpiresAt)
	require.NotZero(t, job.CreatedAt)
}

func TestService_AssignJob_PersistsJob(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("worker1")
	require.NotNil(t, job)

	// Verify the job was persisted.
	require.NotNil(t, svc.jobs.get(job.ID))
}

func TestService_AssignJob_ExhaustsPlanner(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	j1 := svc.assignJob("worker1")
	require.NotNil(t, j1)

	// Same planner has no more groups.
	j2 := svc.assignJob("worker2")
	require.Nil(t, j2)
}

func TestService_RebuildPlanner_ExcludesInFlight(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// 4 indexes → 2 groups of 2.
	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2", "hash3", "hash4")
	pollStore(t, indexStore)

	svc.rebuildPlanner(indexStore.Snapshot())

	j1 := svc.assignJob("worker1")
	require.NotNil(t, j1)

	// Rebuild after j1 is assigned — its sources must be excluded.
	svc.rebuildPlanner(indexStore.Snapshot())

	j2 := svc.assignJob("worker2")
	require.NotNil(t, j2)

	// The two jobs must not share any source IDs.
	j1set := make(map[string]struct{})
	for _, id := range j1.SourceIDs {
		j1set[id] = struct{}{}
	}
	for _, id := range j2.SourceIDs {
		require.NotContains(t, j1set, id, "jobs must not share source IDs")
	}
}

func TestService_CheckCompletedJobs(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("worker1")
	require.NotNil(t, job)

	// Write a merged index that covers both sources.
	populateMergedBucket(t, bucket, "2024-01-01", "merged1", []string{
		"2024-01-01/hash1",
		"2024-01-01/hash2",
	})
	pollStore(t, indexStore)

	svc.resolveCompletedWork(indexStore.Snapshot())

	require.Nil(t, svc.jobs.get(job.ID))
}

func TestService_CheckCompletedJobs_PartialCompaction(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("worker1")
	require.NotNil(t, job)

	// Only one source compacted → job must remain.
	populateMergedBucket(t, bucket, "2024-01-01", "partial1", []string{
		"2024-01-01/hash1",
	})
	pollStore(t, indexStore)

	svc.resolveCompletedWork(indexStore.Snapshot())

	require.NotNil(t, svc.jobs.get(job.ID), "job should remain while not all sources are compacted")
}

func TestService_Tick_ExpiresJobs(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("worker1")
	require.NotNil(t, job)

	// Force expiry by writing to the job store directly.
	job.ExpiresAt = time.Now().Add(-time.Second)
	require.NoError(t, svc.jobs.save())

	svc.tick(indexStore.Snapshot())

	require.Nil(t, svc.jobs.get(job.ID))
}

func TestService_HandleJobRequest_MissingWorkerID(t *testing.T) {
	svc, _, _ := newTestService(t)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath, nil)
	rec := httptest.NewRecorder()

	svc.handleJobRequest(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestService_HandleJobRequest_NoWork_Returns204(t *testing.T) {
	svc, _, _ := newTestService(t)

	// No indexes → long poll times out and returns 204.
	req := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec := httptest.NewRecorder()

	svc.handleJobRequest(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestService_HandleJobRequest_ReturnsExistingJob(t *testing.T) {
	svc, _, _ := newTestService(t)

	// Pre-populate a job for w1 to simulate a worker retrying after a crash.
	existing := &Job{
		ID:        "job1",
		SourceIDs: []string{"2024-01-01/hash1", "2024-01-01/hash2"},
		WorkerID:  "w1",
		ExpiresAt: time.Now().Add(time.Minute),
		CreatedAt: time.Now(),
	}
	require.NoError(t, svc.jobs.add(existing))

	req := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec := httptest.NewRecorder()

	svc.handleJobRequest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "job1")
}

func TestService_HandleJobRequest_AssignsNewJob(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	req := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec := httptest.NewRecorder()

	svc.handleJobRequest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp schedulerapi.JobResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotEmpty(t, resp.ID)
	require.Len(t, resp.SourceIDs, 2)
}

func TestService_HandleJobRequest_LongPollWakesOnWork(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// Inject work after a short delay while the handler is long-polling.
	go func() {
		time.Sleep(10 * time.Millisecond)
		populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
		require.NoError(t, indexStore.Poll(context.Background()))
		svc.rebuildPlanner(indexStore.Snapshot())
		svc.notifyWorkers()
	}()

	// Give the poll enough room to beat the long-poll timeout.
	svc.longPollTimeout = 500 * time.Millisecond

	req := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec := httptest.NewRecorder()

	svc.handleJobRequest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestService_HandleJobRequest_LongPollClientDisconnect(t *testing.T) {
	svc, _, _ := newTestService(t)

	// Use a long timeout so the test doesn't have to wait for it.
	svc.longPollTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Handler must return promptly when the client disconnects; it should not block.
	start := time.Now()
	svc.handleJobRequest(rec, req)
	require.Less(t, time.Since(start), 200*time.Millisecond)
}

func TestService_WaitForWork_NotifyCloses(t *testing.T) {
	svc, _, _ := newTestService(t)

	ch := svc.waitForWork()

	svc.notifyWorkers()

	select {
	case <-ch:
		// Expected: channel was closed.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("workCh was not closed after notifyWorkers")
	}
}

func TestService_WaitForWork_NewChannelAfterNotify(t *testing.T) {
	svc, _, _ := newTestService(t)

	ch1 := svc.waitForWork()
	svc.notifyWorkers()
	ch2 := svc.waitForWork()

	// The new channel must be open (not yet notified).
	select {
	case <-ch2:
		t.Fatal("new workCh should not be closed yet")
	default:
		// Expected.
	}

	_ = ch1 // suppress unused variable warning
}

func TestService_HandleJobRequest_WorkerReportsCompletion(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// Set up two groups of indexes (4 total, group size 2).
	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2", "hash3", "hash4")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	// Assign a job to w1.
	req1 := httptest.NewRequest(http.MethodGet, schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec1 := httptest.NewRecorder()
	svc.handleJobRequest(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	var firstJob schedulerapi.JobResponse
	require.NoError(t, json.NewDecoder(rec1.Body).Decode(&firstJob))

	// Rebuild planner so the second group is available.
	svc.rebuildPlanner(indexStore.Snapshot())

	// w1 reports the first job as completed and requests a new one.
	req2 := httptest.NewRequest(http.MethodGet,
		schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1&"+schedulerapi.ParamLastCompletedJobID+"="+firstJob.ID, nil)
	rec2 := httptest.NewRecorder()
	svc.handleJobRequest(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	// The first job must have been removed.
	require.Nil(t, svc.jobs.get(firstJob.ID))

	// A new job was assigned.
	var secondJob schedulerapi.JobResponse
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&secondJob))
	require.NotEqual(t, firstJob.ID, secondJob.ID)
}

func TestService_HandleJobRequest_WorkerReportsCompletion_WrongWorker(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	// Assign a job to w1.
	job := svc.assignJob("w1")
	require.NotNil(t, job)

	// w2 tries to report w1's job as completed — should be ignored.
	req := httptest.NewRequest(http.MethodGet,
		schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w2&"+schedulerapi.ParamLastCompletedJobID+"="+job.ID, nil)
	rec := httptest.NewRecorder()
	svc.handleJobRequest(rec, req)

	// Job must still exist (not removed by the wrong worker).
	require.NotNil(t, svc.jobs.get(job.ID))
}

func TestService_HandleAbandonJob(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("w1")
	require.NotNil(t, job)
	require.NotNil(t, svc.jobs.get(job.ID))

	req := httptest.NewRequest(http.MethodPost,
		schedulerapi.AbandonPath+"?"+schedulerapi.ParamWorkerID+"=w1&"+schedulerapi.ParamJobID+"="+job.ID, nil)
	rec := httptest.NewRecorder()
	svc.handleAbandonJob(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, svc.jobs.get(job.ID), "job should be removed after abandon")

	// Sources should NOT be in completedSources (abandon != completion).
	svc.mu.Lock()
	for _, src := range job.SourceIDs {
		_, exists := svc.completedSources[src]
		require.False(t, exists, "abandoned sources must not be marked completed")
	}
	svc.mu.Unlock()

	// Freed sources should be immediately re-plannable: the handler rebuilds
	// the planner, so a new worker can pick them up without waiting for a tick.
	j2 := svc.assignJob("w2")
	require.NotNil(t, j2, "abandoned sources should be re-plannable immediately")
}

func TestService_HandleAbandonJob_MissingParams(t *testing.T) {
	svc, _, _ := newTestService(t)

	// Missing both params.
	req := httptest.NewRequest(http.MethodPost, schedulerapi.AbandonPath, nil)
	rec := httptest.NewRecorder()
	svc.handleAbandonJob(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Missing job_id.
	req = httptest.NewRequest(http.MethodPost, schedulerapi.AbandonPath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec = httptest.NewRecorder()
	svc.handleAbandonJob(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestService_HandleAbandonJob_WrongWorker(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("w1")
	require.NotNil(t, job)

	// w2 tries to abandon w1's job.
	req := httptest.NewRequest(http.MethodPost,
		schedulerapi.AbandonPath+"?"+schedulerapi.ParamWorkerID+"=w2&"+schedulerapi.ParamJobID+"="+job.ID, nil)
	rec := httptest.NewRecorder()
	svc.handleAbandonJob(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)

	// Job should still exist.
	require.NotNil(t, svc.jobs.get(job.ID))
}

func TestService_HandleAbandonJob_AlreadyGone(t *testing.T) {
	svc, _, _ := newTestService(t)

	// Abandon a job that doesn't exist — should be a no-op success.
	req := httptest.NewRequest(http.MethodPost,
		schedulerapi.AbandonPath+"?"+schedulerapi.ParamWorkerID+"=w1&"+schedulerapi.ParamJobID+"=nonexistent", nil)
	rec := httptest.NewRecorder()
	svc.handleAbandonJob(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestService_HandleCompleteJob(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("w1")
	require.NotNil(t, job)

	req := httptest.NewRequest(http.MethodPost,
		schedulerapi.CompletePath+"?"+schedulerapi.ParamWorkerID+"=w1&"+schedulerapi.ParamJobID+"="+job.ID, nil)
	rec := httptest.NewRecorder()
	svc.handleCompleteJob(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, svc.jobs.get(job.ID), "job should be removed after completion")
	require.Nil(t, svc.jobs.getByWorker("w1"), "a restarted w1 must not be handed the finished job")

	// Unlike abandon, the sources stay out of planning until a store poll
	// sees the merged index.
	svc.rebuildPlanner(indexStore.Snapshot())
	require.Nil(t, svc.assignJob("w2"), "completed sources must not be re-planned")
}

func TestService_HandleCompleteJob_WrongWorker(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	pollStore(t, indexStore)
	svc.rebuildPlanner(indexStore.Snapshot())

	job := svc.assignJob("w1")
	require.NotNil(t, job)

	req := httptest.NewRequest(http.MethodPost,
		schedulerapi.CompletePath+"?"+schedulerapi.ParamWorkerID+"=w2&"+schedulerapi.ParamJobID+"="+job.ID, nil)
	rec := httptest.NewRecorder()
	svc.handleCompleteJob(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.NotNil(t, svc.jobs.get(job.ID))
}

func TestService_HandleCompleteJob_AlreadyGone(t *testing.T) {
	svc, _, _ := newTestService(t)

	// A resent report for a job the scheduler already removed is a no-op success.
	req := httptest.NewRequest(http.MethodPost,
		schedulerapi.CompletePath+"?"+schedulerapi.ParamWorkerID+"=w1&"+schedulerapi.ParamJobID+"=nonexistent", nil)
	rec := httptest.NewRecorder()
	svc.handleCompleteJob(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestService_HandleCompleteJob_MissingParams(t *testing.T) {
	svc, _, _ := newTestService(t)

	req := httptest.NewRequest(http.MethodPost, schedulerapi.CompletePath+"?"+schedulerapi.ParamWorkerID+"=w1", nil)
	rec := httptest.NewRecorder()
	svc.handleCompleteJob(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestService_HandleJobRequest_WorkerReportsCompletion_AlreadyRemoved(t *testing.T) {
	svc, _, _ := newTestService(t)

	// w1 reports a job ID that doesn't exist — no-op, no panic.
	req := httptest.NewRequest(http.MethodGet,
		schedulerapi.JobPath+"?"+schedulerapi.ParamWorkerID+"=w1&"+schedulerapi.ParamLastCompletedJobID+"=nonexistent", nil)
	rec := httptest.NewRecorder()
	svc.handleJobRequest(rec, req)

	// Should get 204 (no work available) — the point is no error/panic.
	require.Equal(t, http.StatusNoContent, rec.Code)
}

// TestService_NoSourceRecompaction_Concurrent verifies that the fast-path
// completion + planner rebuild race does not cause a source to be assigned
// to two different jobs. This is the scenario that caused repeated compaction
// of the same objects in production: the data race on completedSources allowed
// rebuildPlanner to miss recently-completed sources and re-plan them.
func TestService_NoSourceRecompaction_Concurrent(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// 20 indexes → 10 groups of 2. Enough to keep workers busy across ticks.
	hashes := make([]string, 20)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("hash%02d", i)
	}
	populateBucket(t, bucket, "2024-01-01", hashes...)
	pollStore(t, indexStore)

	// Track every source ID ever assigned. If any source appears twice, the
	// invariant is broken.
	var mu sync.Mutex
	assigned := make(map[string]string) // source ID → job ID that first claimed it

	iterations := 50
	snap := indexStore.Snapshot()

	for i := 0; i < iterations; i++ {
		svc.rebuildPlanner(snap)
		svc.notifyWorkers()

		// Simulate two workers racing to complete jobs and get new ones.
		var wg sync.WaitGroup
		for _, workerID := range []string{"w1", "w2"} {
			wg.Add(1)
			go func() {
				defer wg.Done()

				job := svc.assignJob(workerID)
				if job == nil {
					return
				}

				mu.Lock()
				for _, src := range job.SourceIDs {
					if prevJob, seen := assigned[src]; seen && prevJob != job.ID {
						t.Errorf("source %s assigned to job %s, was already assigned to job %s", src, job.ID, prevJob)
					}
					assigned[src] = job.ID
				}
				mu.Unlock()

				// Simulate fast-path: remove and mark completed.
				svc.mu.Lock()
				_ = svc.jobs.remove(job.ID)
				for _, src := range job.SourceIDs {
					svc.completedSources[src] = struct{}{}
				}
				svc.mu.Unlock()
			}()
		}
		wg.Wait()
	}
}

func TestService_RebuildPlanner_ExcludesCompletedSources(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// 4 indexes → 2 groups of 2.
	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2", "hash3", "hash4")
	pollStore(t, indexStore)

	svc.rebuildPlanner(indexStore.Snapshot())
	j1 := svc.assignJob("worker1")
	require.NotNil(t, j1)

	// Simulate: worker completes the job and the scheduler removes it (fast path).
	// The store has NOT polled yet, so the snapshot still shows sources as active.
	svc.mu.Lock()
	require.NoError(t, svc.jobs.remove(j1.ID))
	for _, id := range j1.SourceIDs {
		svc.completedSources[id] = struct{}{}
	}
	svc.mu.Unlock()

	// Rebuild planner from the SAME (stale) snapshot — completed sources must
	// be excluded even though they still appear as active.
	svc.rebuildPlanner(indexStore.Snapshot())
	j2 := svc.assignJob("worker2")
	require.NotNil(t, j2)

	// The second job must use the other two indexes, not re-plan the completed ones.
	for _, id := range j2.SourceIDs {
		for _, completedID := range j1.SourceIDs {
			require.NotEqual(t, id, completedID, "completed sources must not be re-planned")
		}
	}

	// After the store polls and confirms the sources are compacted, the
	// completedSources cache should be pruned.
	populateMergedBucket(t, bucket, "2024-01-01", "merged1", j1.SourceIDs)
	pollStore(t, indexStore)

	svc.resolveCompletedWork(indexStore.Snapshot())

	svc.mu.Lock()
	remaining := len(svc.completedSources)
	svc.mu.Unlock()
	require.Zero(t, remaining, "completed sources should be pruned after snapshot catches up")
}

func TestService_DeleteEligibleIndexes_CompactedSource(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// Write two source indexes and a merged index covering them.
	populateBucket(t, bucket, "2024-01-01", "hash1", "hash2")
	populateMergedBucket(t, bucket, "2024-01-01", "merged1", []string{
		"2024-01-01/hash1",
		"2024-01-01/hash2",
	})
	pollStore(t, indexStore)

	snap := indexStore.Snapshot()
	require.True(t, snap.IsCompacted("2024-01-01/hash1"))
	require.True(t, snap.IsCompacted("2024-01-01/hash2"))

	// Grace period hasn't elapsed — nothing should be deleted.
	svc.cfg.Retention.CompactedSourceDeleteDelay = 1 * time.Hour
	svc.deleteEligibleIndexes(context.Background(), snap)
	// Sources should still exist in bucket.
	exists, err := bucket.Exists(context.Background(), "2024-01-01/hash1/meta.json")
	require.NoError(t, err)
	require.True(t, exists, "source should still exist before grace period")

	// Backdate the merged index's CreatedAt so grace period has elapsed.
	uploadMeta(t, bucket, store.Meta{
		Date:          "2024-01-01",
		Hash:          "merged1",
		Version:       "v3",
		MinLogTs:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxLogTs:      time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
		MinRecordTs:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxRecordTs:   time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
		CompactedFrom: []string{"2024-01-01/hash1", "2024-01-01/hash2"},
		CreatedAt:     time.Now().Add(-2 * time.Hour),
	})
	pollStore(t, indexStore)
	snap = indexStore.Snapshot()

	svc.deleteEligibleIndexes(context.Background(), snap)

	// Source meta.json files should be deleted from bucket.
	exists, err = bucket.Exists(context.Background(), "2024-01-01/hash1/meta.json")
	require.NoError(t, err)
	require.False(t, exists, "source should be deleted after grace period")

	exists, err = bucket.Exists(context.Background(), "2024-01-01/hash2/meta.json")
	require.NoError(t, err)
	require.False(t, exists, "source should be deleted after grace period")

	// The merged index should still exist.
	exists, err = bucket.Exists(context.Background(), "2024-01-01/merged1/meta.json")
	require.NoError(t, err)
	require.True(t, exists, "merged index should still exist")
}

func TestService_DeleteEligibleIndexes_RetentionExpired(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	now := time.Now().UTC()

	// Write an active index with MaxLogTs far in the past.
	uploadMeta(t, bucket, store.Meta{
		Date:        "2023-01-01",
		Hash:        "oldhash1oldhash1",
		Version:     "v3",
		MinLogTs:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxLogTs:    time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC),
		MinRecordTs: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxRecordTs: time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC),
	})
	// Write a recent active index that should NOT be deleted.
	uploadMeta(t, bucket, store.Meta{
		Date:        "2026-02-25",
		Hash:        "recent1recent1re",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
	})
	pollStore(t, indexStore)

	svc.limits = stubLimits{retentionPeriod: 30 * 24 * time.Hour}
	snap := indexStore.Snapshot()

	svc.deleteEligibleIndexes(context.Background(), snap)

	// Old index should be gone from bucket.
	exists, err := bucket.Exists(context.Background(), "2023-01-01/oldhash1oldhash1/meta.json")
	require.NoError(t, err)
	require.False(t, exists, "old index should have been deleted")

	// Recent index should still be present.
	exists, err = bucket.Exists(context.Background(), "2026-02-25/recent1recent1re/meta.json")
	require.NoError(t, err)
	require.True(t, exists, "recent index should still exist")
}

func TestService_DeleteEligibleIndexes_MergedWithLiveSourcesNotDeleted(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	// Write sources and a merged index, but DON'T let the grace period
	// elapse on the sources. Then put old MaxLogTs on the merged index
	// itself to make it eligible for retention — but it shouldn't be
	// deleted because its sources still exist.
	uploadMeta(t, bucket, store.Meta{
		Date:        "2023-01-01",
		Hash:        "src1xxxxxxxxxxxx",
		Version:     "v3",
		MinLogTs:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxLogTs:    time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC),
		MinRecordTs: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxRecordTs: time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC),
	})
	uploadMeta(t, bucket, store.Meta{
		Date:          "2023-01-01",
		Hash:          "mrgdxxxxxxxxxxxx",
		Version:       "v3",
		MinLogTs:      time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxLogTs:      time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC),
		MinRecordTs:   time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxRecordTs:   time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC),
		CompactedFrom: []string{"2023-01-01/src1xxxxxxxxxxxx"},
		CreatedAt:     time.Now(),
	})
	pollStore(t, indexStore)

	svc.cfg.Retention.CompactedSourceDeleteDelay = 1 * time.Hour
	snap := indexStore.Snapshot()

	svc.deleteEligibleIndexes(context.Background(), snap)
	pollStore(t, indexStore)

	snap = indexStore.Snapshot()
	// The merged index must still exist because its source is still present.
	// (canDelete prevents deletion of merged indexes with live sources.)
	var ids []string
	for _, m := range snap.Active() {
		ids = append(ids, m.ID())
	}
	// Source should have been deleted (retention expired, it's not merged).
	// Wait, the source IS compacted (it's in the merged's CompactedFrom),
	// so it goes through the grace period path, not the retention path.
	// Since CreatedAt is now (< 1h grace), the source is NOT eligible yet.
	// Neither is the merged index (its source still exists, canDelete=false).
	// So nothing should be deleted.
	require.Contains(t, ids, "2023-01-01/mrgdxxxxxxxxxxxx", "merged index should still exist")
}

func TestPlanner_MaxJobSize_SkipsOversizedGroup(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000001", SizeBytes: 100},
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000002", SizeBytes: 200},
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000003", SizeBytes: 50},
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000004", SizeBytes: 50},
	}

	// Limit of 150: planner should skip the unpairable 200-byte index and
	// still form a valid 100+50 group.
	p := newDateGroupPlanner(metas, 150)

	group := p.Plan()
	require.NotNil(t, group)
	require.Equal(t, []string{"2024-01-01/aaaa000000000001", "2024-01-01/aaaa000000000003"}, group)

	// No more groups.
	require.Nil(t, p.Plan())

	// Verify skipped count (oversized 200-byte index + leftover singleton 50-byte index).
	require.Equal(t, 2, p.(*dateGroupPlanner).skipped)
}

func TestPlanner_MaxJobSize_ZeroProducesNoWork(t *testing.T) {
	metas := []store.Meta{
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000001", SizeBytes: 1000000},
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000002", SizeBytes: 1000000},
	}

	// With a zero limit, any positive-sized index is unmergeable.
	p := newDateGroupPlanner(metas, 0)

	require.Nil(t, p.Plan())
	require.Equal(t, 2, p.(*dateGroupPlanner).skipped)
}

func TestPlanner_MaxJobSize_UnknownSizePassesThrough(t *testing.T) {
	// SizeBytes=0 means unknown — should not be blocked.
	metas := []store.Meta{
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000001", SizeBytes: 0},
		{Version: "v3", Date: "2024-01-01", Hash: "aaaa000000000002", SizeBytes: 0},
	}

	p := newDateGroupPlanner(metas, 100)

	group := p.Plan()
	require.NotNil(t, group)
	require.Len(t, group, 2)
}

func TestService_RebuildPlanner_RespectsMaxJobSize(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	cfg := testConfig(t, 500) // 500 bytes limit

	svc, err := New(s, stubLimits{}, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	js, err := newJobStore(cfg.Compaction.JobStateDir, log.NewNopLogger())
	require.NoError(t, err)
	svc.jobs = js

	baseMeta := store.Meta{
		Version:     "v3",
		MinLogTs:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxLogTs:    time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
		MinRecordTs: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxRecordTs: time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC),
	}

	// Two large indexes (300 bytes each → 600 total > 500 limit).
	large1 := baseMeta
	large1.Date = "2024-01-01"
	large1.Hash = "aaaa000000000001"
	uploadMetaWithSize(t, bucket, large1, 300)

	large2 := baseMeta
	large2.Date = "2024-01-01"
	large2.Hash = "aaaa000000000002"
	uploadMetaWithSize(t, bucket, large2, 300)

	// Two small indexes (100 bytes each → 200 total < 500 limit).
	small1 := baseMeta
	small1.Date = "2024-01-01"
	small1.Hash = "bbbb000000000001"
	uploadMetaWithSize(t, bucket, small1, 100)

	small2 := baseMeta
	small2.Date = "2024-01-01"
	small2.Hash = "bbbb000000000002"
	uploadMetaWithSize(t, bucket, small2, 100)

	pollStore(t, s)
	svc.rebuildPlanner(s.Snapshot())

	// Planner must respect MaxJobSize: no group should exceed 500 bytes.
	// With 2×300 and 2×100 byte indexes, the planner must avoid grouping
	// both 300-byte indexes together (600 > 500).
	job := svc.assignJob("worker1")
	require.NotNil(t, job)
	require.GreaterOrEqual(t, len(job.SourceIDs), 2)

	metaByID := make(map[string]store.Meta)
	for _, m := range s.Snapshot().Active() {
		metaByID[m.ID()] = m
	}
	var totalSize int64
	for _, id := range job.SourceIDs {
		m, ok := metaByID[id]
		require.True(t, ok, "source %s not in snapshot", id)
		totalSize += m.SizeBytes
	}
	require.LessOrEqual(t, totalSize, int64(500), "job exceeds MaxJobSize")
}

// TestDeleteEligibleIndexes_UsesLimitsRetention verifies that when limits.RetentionPeriod
// returns a non-zero value, it is used as the retention cutoff.
func TestDeleteEligibleIndexes_UsesLimitsRetention(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	cfg := testConfig(t, 8)

	// limits.RetentionPeriod returns 30 days.
	lim := stubLimits{retentionPeriod: 30 * 24 * time.Hour}
	svc, err := New(s, lim, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	js, err := newJobStore(cfg.Compaction.JobStateDir, log.NewNopLogger())
	require.NoError(t, err)
	svc.jobs = js

	// Index from 60 days ago — outside limits retention (30d).
	oldMeta := store.Meta{
		Date:        "2023-06-01",
		Hash:        "oldhash1oldhash1",
		Version:     "v3",
		MinLogTs:    time.Now().Add(-60 * 24 * time.Hour),
		MaxLogTs:    time.Now().Add(-60 * 24 * time.Hour),
		MinRecordTs: time.Now().Add(-60 * 24 * time.Hour),
		MaxRecordTs: time.Now().Add(-60 * 24 * time.Hour),
	}
	uploadMeta(t, bucket, oldMeta)

	// Recent index — should not be deleted.
	recentMeta := store.Meta{
		Date:        "2026-03-11",
		Hash:        "recent1recent1re",
		Version:     "v3",
		MinLogTs:    time.Now().Add(-1 * time.Hour),
		MaxLogTs:    time.Now(),
		MinRecordTs: time.Now().Add(-1 * time.Hour),
		MaxRecordTs: time.Now(),
	}
	uploadMeta(t, bucket, recentMeta)

	pollStore(t, s)
	snap := s.Snapshot()
	svc.deleteEligibleIndexes(context.Background(), snap)

	// Old index should be deleted (outside limits 30-day window).
	exists, err := bucket.Exists(context.Background(), oldMeta.MetaPath())
	require.NoError(t, err)
	require.False(t, exists, "index beyond limits retention should be deleted")

	// Recent index should still exist.
	exists, err = bucket.Exists(context.Background(), recentMeta.MetaPath())
	require.NoError(t, err)
	require.True(t, exists, "recent index should not be deleted")
}

// TestDeleteEligibleIndexes_ZeroRetention verifies that when
// limits.RetentionPeriod returns 0, no active indexes are deleted.
func TestDeleteEligibleIndexes_ZeroRetention(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	cfg := testConfig(t, 8)

	// stubLimits{} returns 0 for RetentionPeriod — means unlimited, keep forever.
	svc, err := New(s, stubLimits{}, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	js, err := newJobStore(cfg.Compaction.JobStateDir, log.NewNopLogger())
	require.NoError(t, err)
	svc.jobs = js

	// Index from 60 days ago.
	oldMeta := store.Meta{
		Date:        "2023-06-01",
		Hash:        "oldhash2oldhash2",
		Version:     "v3",
		MinLogTs:    time.Now().Add(-60 * 24 * time.Hour),
		MaxLogTs:    time.Now().Add(-60 * 24 * time.Hour),
		MinRecordTs: time.Now().Add(-60 * 24 * time.Hour),
		MaxRecordTs: time.Now().Add(-60 * 24 * time.Hour),
	}
	uploadMeta(t, bucket, oldMeta)

	pollStore(t, s)
	snap := s.Snapshot()
	svc.deleteEligibleIndexes(context.Background(), snap)

	// Zero retention means keep forever — old index should NOT be deleted.
	exists, err := bucket.Exists(context.Background(), oldMeta.MetaPath())
	require.NoError(t, err)
	require.True(t, exists, "zero retention means keep forever — index should not be deleted")
}
