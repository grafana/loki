package jobqueue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	// ErrBuilderAlreadyRegistered is returned when trying to register a builder for a job type that already has one
	ErrBuilderAlreadyRegistered = errors.New("builder already registered for this job type")
)

// Builder defines the interface for building jobs that will be added to the queue
type Builder interface {
	// BuildJobs builds new jobs and sends them to the provided channel
	// It should be a blocking call and returns when ctx is cancelled.
	BuildJobs(ctx context.Context, jobsChan chan<- *Job) error

	// OnJobResponse reports back the response of the job execution.
	OnJobResponse(response *ReportJobResultRequest)
}

// Queue implements the job queue service
type Queue struct {
	queue                     chan *Job
	closed                    bool
	builders                  map[JobType]Builder
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
	job        *Job
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
		queue:                     make(chan *Job, 1000),
		builders:                  make(map[JobType]Builder),
		stop:                      make(chan struct{}),
		checkTimedOutJobsInterval: checkTimedOutJobsInterval,
		processingJobs:            make(map[string]*processingJob),
		jobTimeout:                15 * time.Minute,
		maxRetries:                3,
	}

	// Start the job timeout checker
	q.wg.Add(1)
	go q.checkJobTimeouts()

	return q
}

// RegisterBuilder registers a builder for a specific job type
func (q *Queue) RegisterBuilder(jobType JobType, builder Builder) error {
	if _, exists := q.builders[jobType]; exists {
		return ErrBuilderAlreadyRegistered
	}

	q.builders[jobType] = builder
	return nil
}

// Start starts all registered builders
func (q *Queue) Start(ctx context.Context) error {
	for jobType, builder := range q.builders {
		q.wg.Add(1)
		go q.startBuilder(ctx, jobType, builder)
	}
	return nil
}

// Stop stops all builders
func (q *Queue) Stop() error {
	close(q.stop)
	q.wg.Wait()
	return nil
}

func (q *Queue) startBuilder(ctx context.Context, jobType JobType, builder Builder) {
	defer q.wg.Done()

	// Start the builder in a separate goroutine
	builderErrChan := make(chan error, 1)
	go func() {
		builderErrChan <- builder.BuildJobs(ctx, q.queue)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stop:
			return
		case err := <-builderErrChan:
			if err != nil && !errors.Is(err, context.Canceled) {
				level.Error(util_log.Logger).Log("msg", "builder error", "job_type", jobType, "error", err)
			}
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

// Dequeue implements the gRPC Dequeue method
func (q *Queue) Dequeue(ctx context.Context, _ *DequeueRequest) (*DequeueResponse, error) {
	if q.closed {
		return &DequeueResponse{}, nil
	}

	select {
	case <-ctx.Done():
		return nil, status.Error(codes.Canceled, ctx.Err().Error())
	case job, ok := <-q.queue:
		if !ok {
			return &DequeueResponse{}, nil
		}

		// Track the job as being processed
		q.processingJobsMtx.Lock()
		defer q.processingJobsMtx.Unlock()
		q.processingJobs[job.Id] = &processingJob{
			job:        job,
			dequeued:   time.Now(),
			retryCount: 0,
		}

		return &DequeueResponse{
			Job: job,
		}, nil
	}
}

// ReportJobResponse implements the gRPC ReportJobResponse method
func (q *Queue) ReportJobResponse(ctx context.Context, req *ReportJobResultRequest) (*ReportJobResultResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	q.processingJobsMtx.Lock()
	defer q.processingJobsMtx.Unlock()
	pj, exists := q.processingJobs[req.JobId]
	if !exists {
		return nil, status.Error(codes.NotFound, "job not found")
	}

	if req.Error != "" {
		level.Error(util_log.Logger).Log(
			"msg", "job execution failed",
			"job_id", req.JobId,
			"job_type", req.JobType,
			"error", req.Error,
			"retry_count", pj.retryCount,
		)

		// Check if we should retry the job
		if pj.retryCount < q.maxRetries {
			pj.retryCount++
			level.Info(util_log.Logger).Log(
				"msg", "retrying failed job",
				"job_id", req.JobId,
				"job_type", req.JobType,
				"retry_count", pj.retryCount,
				"max_retries", q.maxRetries,
			)

			// Requeue the job
			select {
			case <-ctx.Done():
			case q.queue <- pj.job:
				return &ReportJobResultResponse{}, nil
			}
		} else {
			level.Error(util_log.Logger).Log(
				"msg", "job failed after max retries",
				"job_id", req.JobId,
				"job_type", req.JobType,
				"max_retries", q.maxRetries,
			)
		}
	} else {
		level.Debug(util_log.Logger).Log(
			"msg", "job execution succeeded",
			"job_id", req.JobId,
			"job_type", req.JobType,
		)
	}
	q.builders[req.JobType].OnJobResponse(req)

	// Remove the job from processing jobs
	delete(q.processingJobs, req.JobId)

	return &ReportJobResultResponse{}, nil
}

// Close closes the queue and releases all resources
func (q *Queue) Close() {
	if !q.closed {
		close(q.queue)
		q.closed = true
	}
}
