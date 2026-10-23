package scheduler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/logline/format"
	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// RegisterHandlers registers HTTP handlers for the logline-scheduler target.
func RegisterHandlers(srv *server.Server, svc *Service, logger log.Logger) error {
	level.Info(logger).Log("msg", "registering scheduler handlers")
	srv.HTTP.HandleFunc(schedulerapi.JobPath, svc.whenRunning(svc.handleJobRequest))
	srv.HTTP.HandleFunc(schedulerapi.AbandonPath, svc.whenRunning(svc.handleAbandonJob))
	srv.HTTP.HandleFunc(schedulerapi.StatusPath, svc.whenRunning(svc.handleStatus))
	srv.HTTP.HandleFunc(schedulerapi.StoreDumpPath, svc.whenRunning(svc.handleStoreDump))

	// Deprecated routes, see schedulerapi.LegacyJobPath.
	srv.HTTP.HandleFunc(schedulerapi.LegacyJobPath, svc.whenRunning(svc.handleJobRequest))
	srv.HTTP.HandleFunc(schedulerapi.LegacyAbandonPath, svc.whenRunning(svc.handleAbandonJob))
	srv.HTTP.HandleFunc(schedulerapi.LegacyStatusPath, svc.whenRunning(svc.handleStatus))
	srv.HTTP.HandleFunc(schedulerapi.LegacyStoreDumpPath, svc.whenRunning(svc.handleStoreDump))
	return nil
}

// whenRunning answers 503 until the service is running. The HTTP server is a
// dependency of the scheduler, so it serves requests before starting() has
// loaded the job store. Compactors retry on 5xx.
func (s *Service) whenRunning(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.State() != services.Running {
			http.Error(w, "scheduler is not running", http.StatusServiceUnavailable)
			return
		}
		h(w, r)
	}
}

// handleJobRequest serves job assignments to workers.
//
// Workers poll this endpoint to receive the next compaction job. If no work
// is immediately available, the handler holds the connection open (long poll)
// until work arrives, the poll timeout expires, or the client disconnects.
//
// GET /logline/api/v1/compaction/job?worker_id=<id>
//
// Responses:
//
//	200 OK            — job assigned (JSON body: jobResponse)
//	204 No Content    — long poll timed out, no work available; retry
//	400 Bad Request   — missing worker_id
func (s *Service) handleJobRequest(w http.ResponseWriter, r *http.Request) {
	workerID := schedulerapi.WorkerID(r)
	if workerID == "" {
		http.Error(w, "worker_id is required", http.StatusBadRequest)
		return
	}

	s.metrics.jobRequestsTotal.Inc()

	// Process fast-path completion and check for an existing job under one lock.
	if existing := s.prepareWorker(workerID, r); existing != nil {
		writeJob(w, existing)
		return
	}

	// Try to assign a job immediately before entering the long poll.
	if job := s.assignJob(workerID); job != nil {
		writeJob(w, job)
		return
	}

	// No work available — hold the connection open until work arrives or
	// the poll timeout expires.
	s.metrics.longPollWaiting.Inc()
	defer s.metrics.longPollWaiting.Dec()

	timeout := time.NewTimer(s.longPollTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-s.waitForWork():
			// Planner was rebuilt — try again.
			if job := s.assignJob(workerID); job != nil {
				writeJob(w, job)
				return
			}
			// Someone else took the last slot; keep waiting.

		case <-timeout.C:
			w.WriteHeader(http.StatusNoContent)
			return

		case <-r.Context().Done():
			// Client disconnected; nothing to write.
			return
		}
	}
}

// handleAbandonJob allows a worker to abandon its in-flight job. The scheduler
// removes the job so its sources become eligible for re-planning immediately
// rather than waiting for expiration.
//
// POST /logline/api/v1/compaction/abandon?worker_id=<id>&job_id=<id>
//
// Responses:
//
//	200 OK            — job removed (or was already gone)
//	400 Bad Request   — missing worker_id or job_id
//	409 Conflict      — job exists but belongs to a different worker
func (s *Service) handleAbandonJob(w http.ResponseWriter, r *http.Request) {
	workerID := schedulerapi.WorkerID(r)
	jobID := schedulerapi.JobID(r)
	if workerID == "" || jobID == "" {
		http.Error(w, "worker_id and job_id are required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.jobs.get(jobID)
	if job == nil {
		// Already gone (expired, completed, or previously abandoned). Success.
		w.WriteHeader(http.StatusOK)
		return
	}

	if job.WorkerID != workerID {
		http.Error(w, "job belongs to a different worker", http.StatusConflict)
		return
	}

	if err := s.jobs.remove(jobID); err != nil {
		level.Error(s.logger).Log("msg", "failed to remove abandoned job", "job_id", jobID, "worker_id", workerID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	level.Info(s.logger).Log("msg", "job abandoned by worker", "job_id", jobID, "worker_id", workerID)
	s.metrics.jobsCompletedTotal.WithLabelValues("abandon").Inc()
	s.metrics.jobsInFlight.Set(float64(len(s.jobs.jobs)))

	// Rebuild immediately so abandoned sources are schedulable without waiting
	// for the next periodic store poll tick.
	s.rebuildPlannerLocked(s.store.Snapshot())
	s.notifyWorkersLocked()

	w.WriteHeader(http.StatusOK)
}

// writeJob writes a job response as JSON with the appropriate content type.
func writeJob(w http.ResponseWriter, job *Job) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job.Response())
}

// statusSnapshot is a point-in-time capture of scheduler state for the
// status endpoint. Collected under the service mutex, rendered outside it.
type statusSnapshot struct {
	Jobs             []*Job   `json:"jobs"`
	CompletedSources []string `json:"completed_sources"`
}

// status snapshots the scheduler state under the lock.
func (s *Service) status() statusSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs := s.jobs.all()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })

	sources := make([]string, 0, len(s.completedSources))
	for id := range s.completedSources {
		sources = append(sources, id)
	}
	sort.Strings(sources)

	return statusSnapshot{
		Jobs:             jobs,
		CompletedSources: sources,
	}
}

// handleStatus dumps scheduler state as markdown (default) or JSON.
//
// GET /logline/api/v1/compaction/status
//
// Responses:
//
//	200 OK — scheduler state (text/markdown or application/json)
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	snap := s.status()

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")

	var b strings.Builder
	now := time.Now()

	b.WriteString("# Scheduler Status\n\n")

	// Jobs.
	fmt.Fprintf(&b, "## Jobs (%d)\n\n", len(snap.Jobs))
	if len(snap.Jobs) == 0 {
		b.WriteString("_No in-flight jobs._\n\n")
	} else {
		b.WriteString("| Job ID | Worker | Sources | Age | Expires In |\n")
		b.WriteString("|--------|--------|---------|-----|------------|\n")
		for _, j := range snap.Jobs {
			age := now.Sub(j.CreatedAt).Truncate(time.Second)
			expiresIn := j.ExpiresAt.Sub(now).Truncate(time.Second)
			if expiresIn < 0 {
				expiresIn = 0
			}
			fmt.Fprintf(&b, "| `%s` | %s | %d | %s | %s |\n",
				j.ID, j.WorkerID, len(j.SourceIDs), age, expiresIn)
		}
		b.WriteString("\n")
	}

	// Completed sources.
	fmt.Fprintf(&b, "## Completed Sources (%d)\n\n", len(snap.CompletedSources))
	if len(snap.CompletedSources) == 0 {
		b.WriteString("_No recently-completed sources pending confirmation._\n")
	} else {
		for _, id := range snap.CompletedSources {
			fmt.Fprintf(&b, "- `%s`\n", id)
		}
	}

	_, _ = w.Write([]byte(b.String()))
}

// storeDumpEntry is a single index for the store dump response.
type storeDumpEntry struct {
	ID            string    `json:"id"`
	Date          string    `json:"date"`
	Hash          string    `json:"hash"`
	Version       string    `json:"version"`
	State         string    `json:"state"` // "active" or "compacted"
	SizeBytes     int64     `json:"size_bytes"`
	MinLogTs      time.Time `json:"min_log_ts"`
	MaxLogTs      time.Time `json:"max_log_ts"`
	MinRecordTs   time.Time `json:"min_rec_ts"`
	MaxRecordTs   time.Time `json:"max_rec_ts"`
	CreatedAt     time.Time `json:"created_at"`
	CompactedFrom []string  `json:"compacted_from,omitempty"`

	// DocumentInterval is the time range each document ID covers.
	DocumentInterval time.Duration `json:"document_interval,omitempty"`

	// Shard identity. ShardCount=0 means unsharded.
	ShardCount     int    `json:"shard_count"`
	ShardAlgorithm string `json:"shard_algorithm"`
	ShardValue     int    `json:"shard_value"`

	// IndexHeader is included only when ?index_header=true is passed.
	IndexHeader *format.HeaderInfo `json:"index_header,omitempty"`
}

// handleStoreDump dumps the current store snapshot as markdown (default) or JSON.
//
// GET /logline/api/v1/store[?state=all|active|compacted][&index_header=true]
//
// Query parameters:
//
//	state        — filter indexes by state: "all" (default), "active", "compacted"
//	index_header — include full index header in response (default: false)
//
// Responses:
//
//	200 OK — store snapshot (text/markdown or application/json)
//	400 Bad Request — invalid state parameter
func (s *Service) handleStoreDump(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()

	state := r.URL.Query().Get(schedulerapi.ParamState)
	if state == "" {
		state = "all"
	}
	includeHeader := r.URL.Query().Get("index_header") == "true"

	var entries []storeDumpEntry
	switch state {
	case "all":
		for _, m := range snap.All() {
			entries = append(entries, toStoreDumpEntry(m, snap, includeHeader))
		}
	case "active":
		for _, m := range snap.Active() {
			entries = append(entries, toStoreDumpEntry(m, snap, includeHeader))
		}
	case "compacted":
		for _, m := range snap.All() {
			if snap.IsCompacted(m.ID()) {
				entries = append(entries, toStoreDumpEntry(m, snap, includeHeader))
			}
		}
	default:
		http.Error(w, fmt.Sprintf("invalid state %q: must be all, active, or compacted", state), http.StatusBadRequest)
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Date != entries[j].Date {
			return entries[i].Date < entries[j].Date
		}
		return entries[i].ID < entries[j].ID
	})

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")

	var b strings.Builder
	b.WriteString("# Index Store\n\n")
	fmt.Fprintf(&b, "**Filter:** %s | **Count:** %d\n\n", state, len(entries))

	if len(entries) == 0 {
		b.WriteString("_No indexes._\n")
		_, _ = w.Write([]byte(b.String()))
		return
	}

	b.WriteString("| ID | Ver | State | Shard | Size | Log Range | Record Range | Created | Compacted From |\n")
	b.WriteString("|----|-----|-------|-------|------|-----------|--------------|---------|----------------|\n")
	for _, e := range entries {
		size := formatBytes(e.SizeBytes)
		logRange := fmt.Sprintf("%s — %s",
			e.MinLogTs.UTC().Format("2006-01-02 15:04"),
			e.MaxLogTs.UTC().Format("2006-01-02 15:04"))
		recRange := fmt.Sprintf("%s — %s",
			e.MinRecordTs.UTC().Format("2006-01-02 15:04"),
			e.MaxRecordTs.UTC().Format("2006-01-02 15:04"))
		created := e.CreatedAt.UTC().Format("2006-01-02 15:04")
		compacted := ""
		if len(e.CompactedFrom) > 0 {
			compacted = fmt.Sprintf("%d sources", len(e.CompactedFrom))
		}
		shardInfo := "—"
		if e.ShardCount > 1 {
			shardInfo = fmt.Sprintf("%s %d/%d", e.ShardAlgorithm, e.ShardValue, e.ShardCount)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			e.ID, e.Version, e.State, shardInfo, size, logRange, recRange, created, compacted)
	}

	_, _ = w.Write([]byte(b.String()))
}

func toStoreDumpEntry(m store.Meta, snap *store.Snapshot, includeHeader bool) storeDumpEntry {
	state := "active"
	if snap.IsCompacted(m.ID()) {
		state = "compacted"
	}
	e := storeDumpEntry{
		ID:               m.ID(),
		Date:             m.Date,
		Hash:             m.Hash,
		Version:          m.Version,
		State:            state,
		SizeBytes:        m.SizeBytes,
		MinLogTs:         m.MinLogTs,
		MaxLogTs:         m.MaxLogTs,
		MinRecordTs:      m.MinRecordTs,
		MaxRecordTs:      m.MaxRecordTs,
		CreatedAt:        m.CreatedAt,
		CompactedFrom:    m.CompactedFrom,
		DocumentInterval: m.DocumentInterval,
		ShardCount:       m.ShardCount,
		ShardAlgorithm:   m.ShardAlgorithm,
		ShardValue:       m.ShardValue,
	}
	if includeHeader {
		e.IndexHeader = m.IndexHeader
	}
	return e
}

func formatBytes(b int64) string {
	switch {
	case b == 0:
		return "—"
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(b)/1024)
	case b < 1024*1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1024*1024*1024))
	}
}
