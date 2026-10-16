package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func makeJob(id, workerID string, expiresAt time.Time) *Job {
	return &Job{
		ID:        id,
		SourceIDs: []string{"2024-01-01/" + id + "_src1", "2024-01-01/" + id + "_src2"},
		WorkerID:  workerID,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
}

func TestJobStore_FreshStart(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)
	require.Empty(t, js.all())
}

func TestJobStore_AddAndGet(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	job := makeJob("job1", "worker1", time.Now().Add(time.Minute))
	require.NoError(t, js.add(job))

	got := js.get("job1")
	require.NotNil(t, got)
	require.Equal(t, "job1", got.ID)
	require.Equal(t, "worker1", got.WorkerID)
}

func TestJobStore_GetMissing(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	require.Nil(t, js.get("nonexistent"))
}

func TestJobStore_Remove(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	job := makeJob("job1", "worker1", time.Now().Add(time.Minute))
	require.NoError(t, js.add(job))
	require.NoError(t, js.remove("job1"))

	require.Nil(t, js.get("job1"))
}

func TestJobStore_RemoveMissing(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	// Removing a nonexistent job is a no-op.
	require.NoError(t, js.remove("nonexistent"))
}

func TestJobStore_GetByWorker(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	j1 := makeJob("job1", "worker-a", time.Now().Add(time.Minute))
	j2 := makeJob("job2", "worker-b", time.Now().Add(time.Minute))
	require.NoError(t, js.add(j1))
	require.NoError(t, js.add(j2))

	got := js.getByWorker("worker-a")
	require.NotNil(t, got)
	require.Equal(t, "job1", got.ID)

	require.Nil(t, js.getByWorker("worker-unknown"))
}

func TestJobStore_All(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	j1 := makeJob("job1", "w1", time.Now().Add(time.Minute))
	j2 := makeJob("job2", "w2", time.Now().Add(time.Minute))
	require.NoError(t, js.add(j1))
	require.NoError(t, js.add(j2))

	all := js.all()
	require.Len(t, all, 2)
}

func TestJobStore_RemoveExpired(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	past := time.Now().Add(-time.Second)
	future := time.Now().Add(time.Minute)

	expiredJob := makeJob("expired", "w1", past)
	activeJob := makeJob("active", "w2", future)
	require.NoError(t, js.add(expiredJob))
	require.NoError(t, js.add(activeJob))

	removed, err := js.removeExpired(time.Now())
	require.NoError(t, err)
	require.Len(t, removed, 1)
	require.Equal(t, "expired", removed[0].ID)

	// Active job survives.
	require.NotNil(t, js.get("active"))
	require.Nil(t, js.get("expired"))
}

func TestJobStore_RemoveExpired_NoneExpired(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	job := makeJob("job1", "w1", time.Now().Add(time.Minute))
	require.NoError(t, js.add(job))

	removed, err := js.removeExpired(time.Now())
	require.NoError(t, err)
	require.Empty(t, removed)
}

func TestJobStore_InFlightSourceIDs(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	job := &Job{
		ID:        "job1",
		SourceIDs: []string{"2024-01-01/aaa", "2024-01-01/bbb"},
		WorkerID:  "w1",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, js.add(job))

	ids := js.inFlightSourceIDs()
	require.Contains(t, ids, "2024-01-01/aaa")
	require.Contains(t, ids, "2024-01-01/bbb")
	require.Len(t, ids, 2)
}

func TestJobStore_Add_RejectsOverlappingSources(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	j1 := &Job{
		ID:        "job1",
		SourceIDs: []string{"2024-01-01/aaa", "2024-01-01/bbb"},
		WorkerID:  "w1",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, js.add(j1))

	// A second job sharing a source must be rejected.
	j2 := &Job{
		ID:        "job2",
		SourceIDs: []string{"2024-01-01/bbb", "2024-01-01/ccc"},
		WorkerID:  "w2",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	err = js.add(j2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already in-flight")

	// The rejected job must not be persisted.
	require.Nil(t, js.get("job2"))
}

func TestJobStore_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// Write a job in the first store instance.
	js1, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)
	job := makeJob("job1", "worker1", time.Now().Add(time.Minute))
	require.NoError(t, js1.add(job))

	// Create a new store from the same directory — should see the persisted job.
	js2, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)
	got := js2.get("job1")
	require.NotNil(t, got)
	require.Equal(t, "job1", got.ID)
	require.Equal(t, "worker1", got.WorkerID)
}

func TestJobStore_CorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()

	// Write garbage to the jobs file.
	filePath := filepath.Join(dir, jobStoreFile)
	require.NoError(t, os.WriteFile(filePath, []byte("not valid json"), 0o644))

	_, err := newJobStore(dir, log.NewNopLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt job store")
}

func TestJobStore_AtomicSave(t *testing.T) {
	dir := t.TempDir()
	js, err := newJobStore(dir, log.NewNopLogger())
	require.NoError(t, err)

	job := makeJob("job1", "worker1", time.Now().Add(time.Minute))
	require.NoError(t, js.add(job))

	// The .tmp file must not exist after a successful save.
	tmpPath := filepath.Join(dir, jobStoreFile+".tmp")
	_, statErr := os.Stat(tmpPath)
	require.True(t, os.IsNotExist(statErr), "tmp file should not exist after successful save")

	// The main file must be valid JSON.
	data, err := os.ReadFile(filepath.Join(dir, jobStoreFile))
	require.NoError(t, err)
	var jobs []*Job
	require.NoError(t, json.Unmarshal(data, &jobs))
	require.Len(t, jobs, 1)
}
