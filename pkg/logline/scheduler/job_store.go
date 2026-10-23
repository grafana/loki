package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

const jobStoreFile = "jobs.json"

// jobStore manages the persisted list of in-flight compaction jobs.
// Every mutation persists to disk before returning so that job assignments
// survive restarts. On startup, corrupt state causes a hard failure to
// prevent silently re-assigning work that may already be in progress.
//
// Not safe for concurrent use — callers must hold the service mutex.
type jobStore struct {
	jobs     map[string]*Job
	filePath string
	logger   log.Logger
}

// newJobStore creates the job directory, loads existing state, and returns
// a ready jobStore. Returns an error if the persisted file is corrupt —
// the caller must fail to start rather than silently discard state.
func newJobStore(dir string, logger log.Logger) (*jobStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create job dir %s: %w", dir, err)
	}

	js := &jobStore{
		jobs:     make(map[string]*Job),
		filePath: filepath.Join(dir, jobStoreFile),
		logger:   logger,
	}

	if err := js.load(); err != nil {
		return nil, err
	}

	return js, nil
}

// load reads and parses the persisted jobs file.
// A missing file is fine (fresh start). A corrupt file is fatal.
func (js *jobStore) load() error {
	data, err := os.ReadFile(js.filePath)
	if os.IsNotExist(err) {
		level.Info(js.logger).Log("msg", "no persisted job state found, starting fresh")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read job store %s: %w", js.filePath, err)
	}

	var jobs []*Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return fmt.Errorf("corrupt job store %s: %w", js.filePath, err)
	}

	for _, j := range jobs {
		js.jobs[j.ID] = j
	}

	level.Info(js.logger).Log("msg", "loaded persisted job state", "jobs", len(js.jobs))
	return nil
}

// save atomically persists the current job map to disk.
// Writes to a temporary file and renames over the original so that
// a crash mid-write never leaves a partial or corrupt file.
func (js *jobStore) save() error {
	jobs := make([]*Job, 0, len(js.jobs))
	for _, j := range js.jobs {
		jobs = append(jobs, j)
	}

	data, err := json.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("failed to marshal jobs: %w", err)
	}

	tmpPath := js.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write job store tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, js.filePath); err != nil {
		return fmt.Errorf("failed to rename job store tmp file: %w", err)
	}

	return nil
}

// add inserts a job and persists before returning. Returns an error if any of
// the job's source IDs overlap with an existing in-flight job — this prevents
// the same index from being compacted by two jobs concurrently.
func (js *jobStore) add(job *Job) error {
	// Guard: reject if any source overlaps with an existing job.
	for _, existing := range js.jobs {
		for _, src := range job.SourceIDs {
			for _, existingSrc := range existing.SourceIDs {
				if src == existingSrc {
					return fmt.Errorf("source %s already in-flight in job %s (assigned to %s)", src, existing.ID, existing.WorkerID)
				}
			}
		}
	}

	js.jobs[job.ID] = job
	if err := js.save(); err != nil {
		// Roll back the in-memory addition to keep state consistent.
		delete(js.jobs, job.ID)
		return err
	}
	return nil
}

// remove deletes a job by ID and persists before returning.
func (js *jobStore) remove(id string) error {
	job, exists := js.jobs[id]
	if !exists {
		return nil
	}

	delete(js.jobs, id)
	if err := js.save(); err != nil {
		// Roll back the in-memory removal to keep state consistent.
		js.jobs[id] = job
		return err
	}
	return nil
}

// get returns the job with the given ID, or nil if not found.
func (js *jobStore) get(id string) *Job {
	return js.jobs[id]
}

// getByWorker returns the job currently assigned to the given worker, or nil.
func (js *jobStore) getByWorker(workerID string) *Job {
	for _, j := range js.jobs {
		if j.WorkerID == workerID {
			return j
		}
	}
	return nil
}

// all returns a copy of all current jobs.
func (js *jobStore) all() []*Job {
	result := make([]*Job, 0, len(js.jobs))
	for _, j := range js.jobs {
		result = append(result, j)
	}
	return result
}

// removeExpired removes all jobs whose ExpiresAt is before now, persists if
// any were removed, and returns the removed jobs.
func (js *jobStore) removeExpired(now time.Time) ([]*Job, error) {
	var expired []*Job
	for id, j := range js.jobs {
		if now.After(j.ExpiresAt) {
			expired = append(expired, j)
			delete(js.jobs, id)
		}
	}

	if len(expired) == 0 {
		return nil, nil
	}

	if err := js.save(); err != nil {
		// Roll back to keep state consistent.
		for _, j := range expired {
			js.jobs[j.ID] = j
		}
		return nil, err
	}

	return expired, nil
}

// inFlightSourceIDs returns the set of all source IDs across all current jobs.
func (js *jobStore) inFlightSourceIDs() map[string]struct{} {
	ids := make(map[string]struct{})
	for _, j := range js.jobs {
		for _, src := range j.SourceIDs {
			ids[src] = struct{}{}
		}
	}
	return ids
}
