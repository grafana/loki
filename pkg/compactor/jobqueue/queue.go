package jobqueue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	// ErrJobTypeAlreadyRegistered is returned when trying to register a job type that is already registered
	ErrJobTypeAlreadyRegistered = errors.New("job type already registered")
)

// Builder defines the interface for building jobs that will be added to the queue
type Builder interface {
	// BuildJobs builds new jobs and sends them to the provided channel
	// It should be a blocking call and returns when ctx is cancelled.
	BuildJobs(ctx context.Context, jobsChan chan<- *grpc.Job)

	// OnJobResponse reports back the response of the job execution.
	OnJobResponse(response *grpc.JobResult) error
}

// Queue implements the job queue service
type Queue struct {
	queue                     chan *grpc.Job
	builders                  map[grpc.JobType]builder
	wg                        sync.WaitGroup
	stop                      chan struct{}
	checkTimedOutJobsInterval time.Duration
	metrics                   *queueMetrics

	// Track jobs that are being processed
	processingJobs    map[string]*processingJob
	processingJobsMtx sync.RWMutex
}

type processingJob struct {
	job               *grpc.Job
	dequeued          time.Time
	attemptsLeft      int
	lastAttemptFailed bool
}

type builder struct {
	Builder
	jobTimeout time.Duration
	maxRetries int
}

// NewQueue creates a new job queue
func NewQueue(r prometheus.Registerer) *Queue {
	return newQueue(time.Minute, r)
}

// newQueue creates a new job queue with a configurable timed out jobs check ticker interval (for testing)
func newQueue(checkTimedOutJobsInterval time.Duration, r prometheus.Registerer) *Queue {
	q := &Queue{
		queue:                     make(chan *grpc.Job),
		builders:                  make(map[grpc.JobType]builder),
		stop:                      make(chan struct{}),
		checkTimedOutJobsInterval: checkTimedOutJobsInterval,
		processingJobs:            make(map[string]*processingJob),
		metrics:                   newQueueMetrics(r),
	}

	// Start the job timeout checker
	q.wg.Add(1)
	go q.retryFailedJobs()

	return q
}

// RegisterBuilder registers a builder for a specific job type
func (q *Queue) RegisterBuilder(jobType grpc.JobType, b Builder, jobTimeout time.Duration, maxRetries int) error {
	if _, exists := q.builders[jobType]; exists {
		return ErrJobTypeAlreadyRegistered
	}

	q.builders[jobType] = builder{
		Builder:    b,
		jobTimeout: jobTimeout,
		maxRetries: maxRetries,
	}
	return nil
}

// Start starts all registered builders
func (q *Queue) Start(ctx context.Context) {
	buildersWg := sync.WaitGroup{}
	for _, builder := range q.builders {
		buildersWg.Add(1)
		go func() {
			defer buildersWg.Done()
			q.startBuilder(ctx, builder)
		}()
	}

	buildersWg.Wait()
	close(q.stop)
	q.wg.Wait()
	close(q.queue)
}

func (q *Queue) startBuilder(ctx context.Context, builder Builder) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		builder.BuildJobs(ctx, q.queue)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stop:
			return
		}
	}
}

// retryFailedJobs retries the jobs which are failed. It includes jobs which have hit a timeout.
func (q *Queue) retryFailedJobs() {
	defer q.wg.Done()

	ticker := time.NewTicker(q.checkTimedOutJobsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-q.stop:
			return
		case <-ticker.C:
			q.processingJobsMtx.Lock()
			now := time.Now()
			for jobID, pj := range q.processingJobs {
				if pj.attemptsLeft <= 0 {
					level.Error(util_log.Logger).Log("msg", "job ran out of attempts, dropping it", "jobID", jobID)
					q.metrics.jobsDropped.Inc()

					delete(q.processingJobs, jobID)
					continue
				}
				timeout := q.builders[pj.job.Type].jobTimeout
				if pj.lastAttemptFailed || now.Sub(pj.dequeued) > timeout {
					// Requeue the job
					select {
					case <-q.stop:
						return
					case q.queue <- pj.job:
						reason := "timeout"
						if pj.lastAttemptFailed {
							reason = "failed"
						}
						q.metrics.jobRetries.WithLabelValues(reason).Inc()
						level.Warn(util_log.Logger).Log(
							"msg", "requeued job",
							"job_id", jobID,
							"job_type", pj.job.Type,
							"timeout", timeout,
							"reason", reason,
						)
						// reset the dequeued time so that the timeout is calculated from the time when the job is sent for processing.
						q.processingJobs[jobID].dequeued = time.Now()
						q.processingJobs[jobID].lastAttemptFailed = false
						q.processingJobs[jobID].attemptsLeft--
					}
				}
			}
			q.processingJobsMtx.Unlock()
		}
	}
}

func (q *Queue) Loop(s grpc.JobQueue_LoopServer) error {
	for {
		var job *grpc.Job
		var ok bool
		ctx := s.Context()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.stop:
			return nil
		case job, ok = <-q.queue:
			if !ok {
				return nil
			}
		}

		// Track the job as being processed
		q.processingJobsMtx.Lock()
		if _, ok := q.processingJobs[job.Id]; !ok {
			q.processingJobs[job.Id] = &processingJob{
				job:          job,
				dequeued:     time.Now(),
				attemptsLeft: q.builders[job.Type].maxRetries,
			}
		}
		q.processingJobsMtx.Unlock()

		if err := s.Send(job); err != nil {
			return err
		}
		q.metrics.jobsSent.Inc()

		// Wait for the worker to finish the current job before we give it the next job.
		// Worker signals completion of job by sending us back the execution result of the job we sent.
		resp, err := s.Recv()
		if err != nil {
			return err
		}

		if err := q.reportJobResult(resp); err != nil {
			return err
		}
	}
}

func (q *Queue) reportJobResult(result *grpc.JobResult) error {
	if result == nil {
		return status.Error(codes.InvalidArgument, "result cannot be nil")
	}

	if _, ok := q.builders[result.JobType]; !ok {
		return status.Error(codes.InvalidArgument, "unknown job type")
	}

	q.processingJobsMtx.Lock()
	defer q.processingJobsMtx.Unlock()
	pj, exists := q.processingJobs[result.JobId]
	if !exists {
		return status.Error(codes.NotFound, "job not found")
	}

	if result.Error != "" {
		level.Error(util_log.Logger).Log(
			"msg", "job execution failed",
			"job_id", result.JobId,
			"job_type", result.JobType,
			"error", result.Error,
		)

		// Check if we should retry the job
		if pj.attemptsLeft > 0 {
			level.Info(util_log.Logger).Log(
				"msg", "retrying failed job",
				"job_id", result.JobId,
				"job_type", result.JobType,
				"attempts_left", pj.attemptsLeft,
			)

			pj.lastAttemptFailed = true
			return nil
		}

		level.Error(util_log.Logger).Log(
			"msg", "job failed after max attempts",
			"job_id", result.JobId,
			"job_type", result.JobType,
		)
	} else {
		q.metrics.jobsProcessed.Inc()

		level.Debug(util_log.Logger).Log(
			"msg", "job execution succeeded",
			"job_id", result.JobId,
			"job_type", result.JobType,
		)
	}
	if err := q.builders[result.JobType].OnJobResponse(result); err != nil {
		level.Error(util_log.Logger).Log(
			"msg", "failed to process job response",
			"job_id", result.JobId,
			"job_type", result.JobType,
			"error", err,
		)
		return err
	}

	// Remove the job from processing jobs
	delete(q.processingJobs, result.JobId)

	return nil
}
