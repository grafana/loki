package jobqueue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"go.uber.org/atomic"
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
	OnJobResponse(response *grpc.JobResult)
}

// Queue implements the job queue service
type Queue struct {
	queue                     chan *grpc.Job
	closed                    atomic.Bool
	builders                  map[grpc.JobType]Builder
	wg                        sync.WaitGroup
	stop                      chan struct{}
	checkTimedOutJobsInterval time.Duration

	// Track jobs that are being processed
	processingJobs    map[string]*processingJob
	processingJobsMtx sync.RWMutex
	jobTimeout        time.Duration
	maxRetries        int
}

type processingJob struct {
	job        *grpc.Job
	dequeued   time.Time
	retryCount int
}

// New creates a new job queue
func New() *Queue {
	return newQueue(time.Minute)
}

// newQueue creates a new job queue with a configurable timed out jobs check ticker interval (for testing)
func newQueue(checkTimedOutJobsInterval time.Duration) *Queue {
	q := &Queue{
		queue:                     make(chan *grpc.Job),
		builders:                  make(map[grpc.JobType]Builder),
		stop:                      make(chan struct{}),
		checkTimedOutJobsInterval: checkTimedOutJobsInterval,
		processingJobs:            make(map[string]*processingJob),
		// ToDo(Sandeep): make jobTimeout and maxRetries configurable(possibly job specific)
		jobTimeout: 15 * time.Minute,
		maxRetries: 3,
	}

	// Start the job timeout checker
	q.wg.Add(1)
	go q.checkJobTimeouts()

	return q
}

// RegisterBuilder registers a builder for a specific job type
func (q *Queue) RegisterBuilder(jobType grpc.JobType, builder Builder) error {
	if _, exists := q.builders[jobType]; exists {
		return ErrJobTypeAlreadyRegistered
	}

	q.builders[jobType] = builder
	return nil
}

// Start starts all registered builders
func (q *Queue) Start(ctx context.Context) error {
	for _, builder := range q.builders {
		q.wg.Add(1)
		go q.startBuilder(ctx, builder)
	}
	return nil
}

// Stop stops all builders
func (q *Queue) Stop() error {
	close(q.stop)
	q.wg.Wait()
	return nil
}

func (q *Queue) startBuilder(ctx context.Context, builder Builder) {
	defer q.wg.Done()

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

func (q *Queue) checkJobTimeouts() {
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
				if now.Sub(pj.dequeued) > q.jobTimeout {
					// Requeue the job
					select {
					case <-q.stop:
						return
					case q.queue <- pj.job:
						level.Warn(util_log.Logger).Log(
							"msg", "job timed out, requeuing",
							"job_id", jobID,
							"job_type", pj.job.Type,
							"timeout", q.jobTimeout,
						)
					}
					delete(q.processingJobs, jobID)
				}
			}
			q.processingJobsMtx.Unlock()
		}
	}
}

func (q *Queue) Loop(s grpc.JobQueue_LoopServer) error {
	for {
		var job *grpc.Job
		ctx := s.Context()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.stop:
			return nil
		case job = <-q.queue:
		}

		// Track the job as being processed
		q.processingJobsMtx.Lock()
		q.processingJobs[job.Id] = &processingJob{
			job:        job,
			dequeued:   time.Now(),
			retryCount: 0,
		}
		q.processingJobsMtx.Unlock()

		if err := s.Send(job); err != nil {
			return err
		}

		// Wait for the worker to finish the current job before we give it the next job.
		// Worker signals completion of job by sending us back the execution result of the job we sent.
		resp, err := s.Recv()
		if err != nil {
			return err
		}

		if err := q.reportJobResult(ctx, resp); err != nil {
			return err
		}
	}
}

func (q *Queue) reportJobResult(ctx context.Context, result *grpc.JobResult) error {
	if result == nil {
		return status.Error(codes.InvalidArgument, "result cannot be nil")
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
			"retry_count", pj.retryCount,
		)

		// Check if we should retry the job
		if pj.retryCount < q.maxRetries {
			pj.retryCount++
			level.Info(util_log.Logger).Log(
				"msg", "retrying failed job",
				"job_id", result.JobId,
				"job_type", result.JobType,
				"retry_count", pj.retryCount,
				"max_retries", q.maxRetries,
			)

			// Requeue the job
			select {
			case <-ctx.Done():
			case q.queue <- pj.job:
				return nil
			}
		} else {
			level.Error(util_log.Logger).Log(
				"msg", "job failed after max retries",
				"job_id", result.JobId,
				"job_type", result.JobType,
				"max_retries", q.maxRetries,
			)
		}
	} else {
		level.Debug(util_log.Logger).Log(
			"msg", "job execution succeeded",
			"job_id", result.JobId,
			"job_type", result.JobType,
		)
	}
	q.builders[result.JobType].OnJobResponse(result)

	// Remove the job from processing jobs
	delete(q.processingJobs, result.JobId)

	return nil
}

// Close closes the queue and releases all resources
func (q *Queue) Close() {
	if !q.closed.Load() {
		close(q.queue)
		q.closed.Store(true)
	}
}
