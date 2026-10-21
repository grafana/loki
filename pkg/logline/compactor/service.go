package compactor

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Service implements the dskit Service interface for the logline-compactor target.
// It requests compaction jobs from the scheduler over HTTP and executes them.
type Service struct {
	services.Service

	cfg        Config
	store      *store.Store
	logger     log.Logger
	metrics    *Metrics
	httpClient *http.Client

	// job tracks a single compactor job lifecycle:
	// - running: currently in-flight, eligible for abandon on shutdown.
	// - completed pending ack: finished successfully; include job ID on next
	//   request so the scheduler can remove it immediately (fast-path completion).
	job compactorJob
}

type jobState int

const (
	jobStateIdle jobState = iota
	jobStateRunning
	jobStateCompletedPendingAck
)

// compactorJob tracks the single job lifecycle. No mutex needed: runLoop owns
// all transitions sequentially, and stopping() only runs after running()
// returns (dskit BasicService guarantee).
type compactorJob struct {
	state jobState
	id    string
}

func (j *compactorJob) setRunning(id string) {
	j.state = jobStateRunning
	j.id = id
}

func (j *compactorJob) runningID() string {
	if j.state != jobStateRunning {
		return ""
	}
	return j.id
}

func (j *compactorJob) markCompletedPendingAck(id string) {
	j.state = jobStateCompletedPendingAck
	j.id = id
}

func (j *compactorJob) consumeCompletedID() string {
	if j.state != jobStateCompletedPendingAck {
		return ""
	}
	id := j.id
	j.state = jobStateIdle
	j.id = ""
	return id
}

// New creates a new compactor Service. Validates cfg and initialises metrics.
// The store is held for use during compaction but StartPolling is NOT called —
// the compactor does not need a live snapshot of the index catalogue.
func New(indexStore *store.Store, cfg Config, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid compactor config: %w", err)
	}

	s := &Service{
		cfg:        cfg,
		store:      indexStore,
		logger:     logger,
		metrics:    NewMetrics(reg),
		httpClient: &http.Client{Timeout: schedulerapi.ClientTimeout},
	}
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *Service) starting(_ context.Context) error {
	level.Info(s.logger).Log("msg", "compactor service starting", "scheduler", s.cfg.Compaction.SchedulerAddress, "compactor_id", s.cfg.Compaction.CompactorID)
	return nil
}

func (s *Service) running(ctx context.Context) error {
	return s.runLoop(ctx)
}

func (s *Service) stopping(err error) error {
	level.Info(s.logger).Log("msg", "compactor service stopping", "err", err)
	s.abandonCurrentJob()
	return nil
}

// abandonCurrentJob tells the scheduler to release the compactor's in-flight job
// so it can be reassigned immediately. Uses a generous backoff because the
// scheduler may be rolling at the same time as the compactor.
func (s *Service) abandonCurrentJob() {
	jobID := s.job.runningID()
	if jobID == "" {
		return
	}

	// Use a generous timeout and backoff: the scheduler may be mid-rollout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bo := backoff.New(ctx, backoff.Config{
		MinBackoff: 500 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: 10,
	})

	for bo.Ongoing() {
		req, err := schedulerapi.NewAbandonRequest(ctx, s.cfg.Compaction.SchedulerAddress, s.cfg.Compaction.CompactorID, jobID)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to build abandon request", "job_id", jobID, "err", err)
			return
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			level.Warn(s.logger).Log("msg", "abandon request failed, retrying", "job_id", jobID, "err", err)
			bo.Wait()
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			level.Info(s.logger).Log("msg", "job abandoned successfully", "job_id", jobID)
			return
		}

		// 4xx errors are permanent (e.g. 409 if the job was already
		// reassigned to another compactor). Only retry on 5xx / transient.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			level.Warn(s.logger).Log("msg", "abandon request rejected, not retrying", "job_id", jobID, "status", resp.StatusCode)
			return
		}

		level.Warn(s.logger).Log("msg", "abandon request returned server error, retrying", "job_id", jobID, "status", resp.StatusCode)
		bo.Wait()
	}

	level.Error(s.logger).Log("msg", "failed to abandon job after retries", "job_id", jobID)
}

// runLoop continuously requests jobs from the scheduler and executes them until
// the context is cancelled.
func (s *Service) runLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		job, err := s.requestJob(ctx, s.job.consumeCompletedID())
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			level.Error(s.logger).Log("msg", "failed to request job", "err", err)
			// Brief backoff before retrying to avoid hammering the scheduler on
			// transient connectivity issues.
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return nil
			}
			continue
		}

		if job == nil {
			// The scheduler long-poll timed out with no available work; retry immediately.
			continue
		}

		s.metrics.jobsReceived.Inc()
		s.job.setRunning(job.ID)
		completed := s.executeJob(ctx, job)
		// success! go to the stop and start again
		if completed {
			s.job.markCompletedPendingAck(job.ID)
			continue // success: request next job immediately to report completion
		}

		// service shutting down! go to the top and we'll abandon naturally
		if ctx.Err() != nil {
			continue
		}

		// On failure, keep the running job ID so shutdown can still abandon it.
		// The scheduler can return the same job ID to this compactor for retries.
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return nil
		}
	}
}

// requestJob performs a long-poll request to the scheduler. Returns (nil, nil)
// when the scheduler responds with 204 (no work available), (*schedulerapi.JobResponse, nil)
// when a job is assigned, or (nil, error) on failure.
func (s *Service) requestJob(ctx context.Context, lastCompletedJobID string) (*schedulerapi.JobResponse, error) {
	req, err := schedulerapi.NewJobRequest(ctx, s.cfg.Compaction.SchedulerAddress, s.cfg.Compaction.CompactorID, lastCompletedJobID)
	if err != nil {
		return nil, fmt.Errorf("failed to build job request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("job request failed: %w", err)
	}
	defer resp.Body.Close()

	s.metrics.jobsRequested.Inc()

	job, err := schedulerapi.ParseJobResponse(resp)
	if err != nil {
		return nil, err
	}

	return job, nil
}

// executeJob runs a single compaction job, tracking duration and recording the
// outcome metric. The job's ExpiresAt is used as a hard deadline so stale jobs
// do not block the compactor indefinitely.
func (s *Service) executeJob(ctx context.Context, job *schedulerapi.JobResponse) bool {
	level.Info(s.logger).Log("msg", "job received", "job_id", job.ID, "sources", len(job.SourceIDs), "expires_at", job.ExpiresAt)

	deadlineCtx, cancel := context.WithDeadline(ctx, job.ExpiresAt)
	defer cancel()

	start := time.Now()
	err := s.compact(deadlineCtx, job)
	s.metrics.compactionTime.Observe(time.Since(start).Seconds())

	if err != nil {
		if deadlineCtx.Err() != nil {
			level.Warn(s.logger).Log("msg", "job expired", "job_id", job.ID, "err", err)
			s.metrics.jobsExpired.Inc()
		} else {
			level.Error(s.logger).Log("msg", "job failed", "job_id", job.ID, "err", err)
			s.metrics.jobsFailed.Inc()
		}
		return false
	}

	level.Info(s.logger).Log("msg", "job completed", "job_id", job.ID, "duration", time.Since(start))
	s.metrics.jobsCompleted.Inc()
	return true
}
