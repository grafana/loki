package engine

import (
	"flag"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
)

// WorkerConfig represents the configuration for the [Worker].
type WorkerConfig struct {
	WorkerThreads int `yaml:"worker_threads" category:"experimental"`
}

func (cfg *WorkerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.WorkerThreads, prefix+"worker-threads", 0, "Experimental: Number of worker threads to spawn. Each worker thread runs one task at a time. 0 means to use GOMAXPROCS value.")
}

// WorkerParams holds parameters for constructing a new [Worker].
type WorkerParams struct {
	Logger log.Logger      // Logger for optional log messages.
	Bucket objstore.Bucket // Bucket to read stored data from.

	Config   WorkerConfig   // Configuration for the worker.
	Executor ExecutorConfig // Configuration for task execution.

	// Local scheduler to connect to. If LocalScheduler is nil, the worker can
	// still connect to remote schedulers.
	LocalScheduler *Scheduler
}

// Worker requests tasks from a [Scheduler] and executes them. Task results are
// sent to other [Worker] instances or back to the [Scheduler].
type Worker struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner *worker.Worker
}

// NewWorker creates a new Worker instance. Use [Worker.Service] to manage the
// lifecycle of the Worker.
func NewWorker(params WorkerParams) (*Worker, error) {
	inner, err := worker.New(worker.Config{
		Logger:         params.Logger,
		Bucket:         params.Bucket,
		LocalScheduler: params.LocalScheduler.inner,

		BatchSize:  int64(params.Executor.BatchSize),
		NumThreads: params.Config.WorkerThreads,
	})
	if err != nil {
		return nil, err
	}

	return &Worker{inner: inner}, nil
}

// Service returns the service used to manage the lifecycle of the Worker.
func (w *Worker) Service() services.Service {
	return w.inner.Service()
}
