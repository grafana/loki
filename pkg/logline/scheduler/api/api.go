package schedulerapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Routes served by the scheduler.
const (
	JobPath       = "/logline/api/v1/compaction/job"
	AbandonPath   = "/logline/api/v1/compaction/abandon"
	StatusPath    = "/logline/api/v1/compaction/status"
	StoreDumpPath = "/logline/api/v1/store"
	// CompletePath has no legacy route. Schedulers that predate it answer 404.
	CompletePath = "/logline/api/v1/compaction/complete"
)

// Deprecated:
// TODO(segflow): To remove them, first switch the compactor to the /logline routes and roll it out,
// then stop serving these.
const (
	LegacyJobPath       = "/api/v1/compaction/job"
	LegacyAbandonPath   = "/api/v1/compaction/abandon"
	LegacyStatusPath    = "/api/v1/compaction/status"
	LegacyStoreDumpPath = "/api/v1/store"
)

// Query parameters shared between scheduler and worker.
const (
	ParamWorkerID           = "worker_id"
	ParamJobID              = "job_id"
	ParamLastCompletedJobID = "last_completed_job_id"
	ParamState              = "state"
)

// Long poll timing. Both are constants rather than config because the
// scheduler and the workers roll out independently and must agree on them.
const (
	// LongPollTimeout is how long the scheduler holds a job request when no
	// work is available.
	LongPollTimeout = 30 * time.Second
	// ClientTimeout bounds one worker request to the scheduler.
	ClientTimeout = 60 * time.Second
)

// JobResponse is the wire type for compaction jobs returned by the scheduler.
// Intentionally separate from the scheduler's internal Job to avoid leaking
// internal fields (WorkerID, CreatedAt) over the API.
type JobResponse struct {
	ID        string    `json:"id"`
	SourceIDs []string  `json:"source_ids"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewJobRequest builds an HTTP GET request for requesting a job from the scheduler.
// If lastCompletedJobID is non-empty, it is included so the scheduler can
// immediately remove the finished job (fast-path completion).
func NewJobRequest(ctx context.Context, baseURL, workerID, lastCompletedJobID string) (*http.Request, error) {
	jobURL := baseURL + LegacyJobPath + "?" + ParamWorkerID + "=" + url.QueryEscape(workerID)
	if lastCompletedJobID != "" {
		jobURL += "&" + ParamLastCompletedJobID + "=" + url.QueryEscape(lastCompletedJobID)
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
}

// ErrSchedulerUnavailable is wrapped by the error ParseJobResponse returns
// for a 503, which the scheduler answers until it is running.
var ErrSchedulerUnavailable = errors.New("scheduler unavailable")

// ParseJobResponse decodes an HTTP response into a JobResponse.
// Returns (nil, nil) for 204 No Content, (*JobResponse, nil) for 200 OK,
// or (nil, error) for anything else. The error for a 503 wraps
// ErrSchedulerUnavailable and carries the scheduler's reason.
func ParseJobResponse(resp *http.Response) (*JobResponse, error) {
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		reason, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("%w: %s", ErrSchedulerUnavailable, strings.TrimSpace(string(reason)))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected scheduler response: %d", resp.StatusCode)
	}
	var job JobResponse
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("failed to decode job response: %w", err)
	}
	return &job, nil
}

// NewAbandonRequest builds an HTTP POST request telling the scheduler that the
// worker is abandoning its current job (e.g. during shutdown). The scheduler
// removes the job so it can be reassigned immediately rather than waiting for
// expiration.
func NewAbandonRequest(ctx context.Context, baseURL, workerID, jobID string) (*http.Request, error) {
	abandonURL := baseURL + LegacyAbandonPath + "?" + ParamWorkerID + "=" + url.QueryEscape(workerID) +
		"&" + ParamJobID + "=" + url.QueryEscape(jobID)
	return http.NewRequestWithContext(ctx, http.MethodPost, abandonURL, nil)
}

// NewCompleteRequest builds an HTTP POST request reporting that the worker
// finished a job, without asking for a new one (e.g. during shutdown). The
// scheduler removes the job and keeps its sources out of planning until a store
// poll sees the merged index.
func NewCompleteRequest(ctx context.Context, baseURL, workerID, jobID string) (*http.Request, error) {
	completeURL := baseURL + CompletePath + "?" + ParamWorkerID + "=" + url.QueryEscape(workerID) +
		"&" + ParamJobID + "=" + url.QueryEscape(jobID)
	return http.NewRequestWithContext(ctx, http.MethodPost, completeURL, nil)
}

// WorkerID extracts the worker ID from an inbound HTTP request.
func WorkerID(r *http.Request) string {
	return r.URL.Query().Get(ParamWorkerID)
}

// JobID extracts the job ID from an inbound HTTP request.
func JobID(r *http.Request) string {
	return r.URL.Query().Get(ParamJobID)
}

// LastCompletedJobID extracts the last completed job ID from an inbound HTTP request.
func LastCompletedJobID(r *http.Request) string {
	return r.URL.Query().Get(ParamLastCompletedJobID)
}
