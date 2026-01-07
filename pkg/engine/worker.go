package engine

import (
	"flag"
	"net"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
)

// WorkerConfig represents the configuration for the [Worker].
type WorkerConfig struct {
	WorkerThreads int `yaml:"worker_threads" category:"experimental"`

	SchedulerLookupAddress  string        `yaml:"scheduler_lookup_address" category:"experimental"`
	SchedulerLookupInterval time.Duration `yaml:"scheduler_lookup_interval" category:"experimental"`
}

func (cfg *WorkerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.WorkerThreads, prefix+"worker-threads", 0, "Experimental: Number of worker threads to spawn. Each worker thread runs one task at a time. 0 means to use GOMAXPROCS value.")
	f.StringVar(&cfg.SchedulerLookupAddress, prefix+"scheduler-lookup-address", "", "Experimental: Address holding DNS SRV records of schedulers to connect to.")
	f.DurationVar(&cfg.SchedulerLookupInterval, prefix+"scheduler-lookup-interval", 10*time.Second, "Experimental: Interval at which to lookup new schedulers by DNS SRV records.")
}

// WorkerParams holds parameters for constructing a new [Worker].
type WorkerParams struct {
	Logger    log.Logger          // Logger for optional log messages.
	Bucket    objstore.Bucket     // Bucket to read stored data from.
	Metastore metastore.Metastore // Metastore to access indexes.

	Config   WorkerConfig   // Configuration for the worker.
	Executor ExecutorConfig // Configuration for task execution.

	// Local scheduler to connect to. If LocalScheduler is nil, the worker can
	// still connect to remote schedulers.
	LocalScheduler *Scheduler

	// Address to advertise to other workers and schedulers. Must be set when
	// the worker runs in remote transport mode.
	//
	// If nil, the worker only listens for in-process connections.
	AdvertiseAddr net.Addr

	// Absolute path of the endpoint where the frame handler is registered.
	// Used for connecting to scheduler and other workers.
	Endpoint string
}

// Worker requests tasks from a [Scheduler] and executes them. Task results are
// sent to other [Worker] instances or back to the [Scheduler].
type Worker struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner    *worker.Worker
	endpoint string
	handler  http.Handler
}

// NewWorker creates a new Worker instance. Use [Worker.Service] to manage the
// lifecycle of the Worker.
func NewWorker(params WorkerParams) (*Worker, error) {
	if params.Config.SchedulerLookupAddress != "" && params.Config.SchedulerLookupInterval == 0 {
		return nil, errors.New("scheduler lookup interval must be non-zero when a scheduler lookup address is provided")
	}

	if params.Endpoint == "" {
		params.Endpoint = "/api/v2/frame"
	}

	var (
		listener wire.Listener
		handler  http.Handler
		dialer   wire.Dialer

		// localSchedulerAddress is the local scheduler to connect to. Left nil
		// when using remote transport.
		localSchedulerAddress net.Addr
	)

	switch {
	case params.AdvertiseAddr != nil:
		remoteListener := wire.NewHTTP2Listener(
			params.AdvertiseAddr,
			wire.WithHTTP2ListenerLogger(params.Logger),
			wire.WithHTTP2ListenerMaxPendingConns(10),
		)
		listener, handler = remoteListener, remoteListener
		dialer = wire.NewHTTP2Dialer(params.Endpoint)

	case params.LocalScheduler != nil:
		localListener := &wire.Local{Address: wire.LocalWorker}
		listener = localListener

		schedulerListener, ok := params.LocalScheduler.listener.(*wire.Local)
		if !ok {
			return nil, errors.New("scheduler is not configured for local traffic")
		}

		dialer = wire.NewLocalDialer(localListener, schedulerListener)
		localSchedulerAddress = wire.LocalScheduler

	default:
		return nil, errors.New("either an advertise address or a local scheduler listener must be provided")
	}

	inner, err := worker.New(worker.Config{
		Logger:    params.Logger,
		Bucket:    params.Bucket,
		Metastore: params.Metastore,

		Dialer:   dialer,
		Listener: listener,

		SchedulerAddress:        localSchedulerAddress,
		SchedulerLookupAddress:  params.Config.SchedulerLookupAddress,
		SchedulerLookupInterval: params.Config.SchedulerLookupInterval,

		BatchSize:  int64(params.Executor.BatchSize),
		NumThreads: params.Config.WorkerThreads,

		Endpoint: params.Endpoint,
	})
	if err != nil {
		return nil, err
	}

	return &Worker{
		inner:    inner,
		endpoint: params.Endpoint,
		handler:  handler,
	}, nil
}

// RegisterWorkerServer registers the [wire.Listener] of the inner worker as
// http.Handler on the provided router.
//
// RegisterWorkerServer is a no-op if an advertise address is not provided.
func (w *Worker) RegisterWorkerServer(router *mux.Router) {
	if w.handler == nil {
		return
	}
	router.Path(w.endpoint).Methods("POST").Handler(w.handler)
}

// Service returns the service used to manage the lifecycle of the Worker.
func (w *Worker) Service() services.Service {
	return w.inner.Service()
}

// RegisterMetrics registers metrics about w to report to reg.
func (w *Worker) RegisterMetrics(reg prometheus.Registerer) error {
	return w.inner.RegisterMetrics(reg)
}

// UnregisterMetrics unregisters metrics about w from reg.
func (w *Worker) UnregisterMetrics(reg prometheus.Registerer) {
	w.inner.UnregisterMetrics(reg)
}
