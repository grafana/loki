package jobqueue

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	connBackoffConfig = backoff.Config{
		MinBackoff: 500 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
	}
)

type CompactorClient interface {
	JobQueueClient() grpc.JobQueueClient
}

type JobRunner interface {
	Run(ctx context.Context, job *grpc.Job) ([]byte, error)
}

type WorkerConfig struct {
	NumWorkers int `yaml:"num_workers"`
}

func (c *WorkerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&c.NumWorkers, prefix+"num-workers", 4, "Number of workers to run for concurrent processing of jobs.")
}

func (c *WorkerConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

type WorkerManager struct {
	cfg        WorkerConfig
	grpcClient CompactorClient
	jobRunners map[grpc.JobType]JobRunner

	wg sync.WaitGroup
}

func NewWorkerManager(cfg WorkerConfig, grpcClient CompactorClient) *WorkerManager {
	wm := &WorkerManager{
		cfg:        cfg,
		grpcClient: grpcClient,
		jobRunners: make(map[grpc.JobType]JobRunner),
	}

	return wm
}

func (w *WorkerManager) RegisterJobRunner(jobType grpc.JobType, jobRunner JobRunner) error {
	if _, exists := w.jobRunners[jobType]; exists {
		return ErrJobTypeAlreadyRegistered
	}

	w.jobRunners[jobType] = jobRunner
	return nil
}

func (w *WorkerManager) Start(ctx context.Context) error {
	if len(w.jobRunners) == 0 {
		return errors.New("no job runners registered")
	}

	wg := &sync.WaitGroup{}

	for i := 0; i < w.cfg.NumWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			NewWorker(w.grpcClient, w.jobRunners).Start(ctx)
		}()
	}

	wg.Wait()
	return nil
}

type worker struct {
	grpcClient CompactorClient
	jobRunners map[grpc.JobType]JobRunner
}

func NewWorker(grpcClient CompactorClient, jobRunners map[grpc.JobType]JobRunner) *worker {
	return &worker{
		grpcClient: grpcClient,
		jobRunners: jobRunners,
	}
}

func (w *worker) Start(ctx context.Context) {
	client := w.grpcClient.JobQueueClient()

	backoff := backoff.New(ctx, connBackoffConfig)
	for backoff.Ongoing() {
		c, err := client.Loop(ctx)
		if err != nil {
			level.Warn(util_log.Logger).Log("msg", "error contacting compactor", "err", err)
			backoff.Wait()
			continue
		}

		if err := w.process(c); err != nil {
			level.Error(util_log.Logger).Log("msg", "error running jobs", "err", err)
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process pull jobs from the established stream, processes them and sends back the job result to the stream.
func (w *worker) process(c grpc.JobQueue_LoopClient) error {
	// Build a child context so we can cancel the job when the stream is closed.
	ctx, cancel := context.WithCancelCause(c.Context())
	defer cancel(errors.New("job queue stream closed"))

	for {
		job, err := c.Recv()
		if err != nil {
			return err
		}

		// Execute the job on a "background" goroutine, so we go back to
		// blocking on c.Recv(). This allows us to detect the stream closing
		// and cancel the job execution. We don't process jobs in parallel
		// here, as we're running in a lock-step with the server - each Recv is
		// paired with a Send.
		go func() {
			jobResult := &grpc.JobResult{
				JobId:   job.Id,
				JobType: job.Type,
			}

			jobRunner, ok := w.jobRunners[job.Type]
			if !ok {
				level.Error(util_log.Logger).Log("msg", "job runner for job type not registered", "jobType", job.Type)
				jobResult.Error = fmt.Sprintf("unknown job type %s", job.Type)
				if err := c.Send(jobResult); err != nil {
					level.Error(util_log.Logger).Log("msg", "error sending job result", "err", err)
				}
				return
			}

			jobResponse, err := jobRunner.Run(ctx, job)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error running job", "err", err)
				jobResult.Error = err.Error()
				if err := c.Send(jobResult); err != nil {
					level.Error(util_log.Logger).Log("msg", "error sending job result", "err", err)
				}
				return
			}

			jobResult.Result = jobResponse
			if err := c.Send(jobResult); err != nil {
				level.Error(util_log.Logger).Log("msg", "error sending job result", "err", err)
				return
			}
		}()
	}
}
