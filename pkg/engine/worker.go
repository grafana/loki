package engine

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// WorkerConfig represents the configuration for the [Worker].
type WorkerConfig struct {
	WorkerThreads int

	SchedulerLookupAddress  string
	SchedulerLookupInterval time.Duration
}

// WorkerParams holds parameters for constructing a new [Worker].
type WorkerParams struct {
	Logger log.Logger      // Logger for optional log messages.
	Bucket objstore.Bucket // Bucket to read stored data from.

	// DataBucket reads source log objects during LogMerge compaction. When
	// Bucket is prefixed with the index-storage prefix (compaction wiring),
	// source log objects live at the unprefixed dataobj root and must be read
	// through this bucket. Optional; nil falls back to Bucket.
	DataBucket objstore.Bucket

	Config WorkerConfig // Configuration for the worker.

	// Address to advertise to other workers and schedulers. Required.
	AdvertiseAddr net.Addr

	// Absolute path of the endpoint where the frame handler is registered.
	// Used for connecting to scheduler and other workers.
	Endpoint string

	// ScratchStore holds the temporary data of compaction tasks. Optional; nil
	// keeps the data in memory.
	ScratchStore scratch.Store

	// IndexobjCfg is the builder config for compacted index objects.
	IndexobjCfg logsobj.BuilderBaseConfig

	// LogsobjCfg is the builder config for compacted log objects.
	LogsobjCfg logsobj.BuilderBaseConfig

	// IndexMergeObserver is used  by compaction to populate output-size
	// histograms. Optional; nil disables observation.
	IndexMergeObserver executor.IndexMergeObserver

	// NewLogMergeObserver returns the LogMerge observer for each worker
	// thread. Optional; nil disables observation.
	NewLogMergeObserver executor.NewLogMergeObserverFunc
}

// Worker requests tasks from a scheduler and executes them. Task results are
// sent to other [Worker] instances or back to the scheduler.
type Worker struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner          *worker.Worker
	endpoint       string
	handler        http.Handler
	builderMetrics *logsobj.BuilderMetrics
}

// NewWorker creates a new Worker instance. Use [Worker.Service] to manage the
// lifecycle of the Worker.
func NewWorker(params WorkerParams, reg prometheus.Registerer) (*Worker, error) {
	if params.Config.SchedulerLookupAddress != "" && params.Config.SchedulerLookupInterval == 0 {
		return nil, errors.New("scheduler lookup interval must be non-zero when a scheduler lookup address is provided")
	}

	if params.Config.SchedulerLookupInterval < time.Second {
		level.Warn(params.Logger).Log("msg", "scheduler lookup interval is configured to be less than 1 second, overriding to 1 second")
		params.Config.SchedulerLookupInterval = time.Second
	}

	if params.Endpoint == "" {
		params.Endpoint = "/api/v2/frame"
	}

	if params.AdvertiseAddr == nil {
		return nil, errors.New("an advertise address must be provided")
	}

	listener := wire.NewHTTP2Listener(
		params.AdvertiseAddr,
		wire.WithHTTP2ListenerLogger(params.Logger),
	)
	dialer := wire.NewHTTP2Dialer(params.Endpoint)

	builderMetrics := logsobj.NewBuilderMetrics()
	if err := builderMetrics.Register(reg); err != nil {
		return nil, fmt.Errorf("registering logs object builder metrics: %w", err)
	}

	inner, err := worker.New(worker.Config{
		Logger:     params.Logger,
		Bucket:     params.Bucket,
		DataBucket: params.DataBucket,

		Dialer:   dialer,
		Listener: listener,

		SchedulerLookupAddress:  params.Config.SchedulerLookupAddress,
		SchedulerLookupInterval: params.Config.SchedulerLookupInterval,

		NumThreads: params.Config.WorkerThreads,

		Endpoint: params.Endpoint,

		ScratchStore:   params.ScratchStore,
		IndexobjCfg:    params.IndexobjCfg,
		LogsobjCfg:     params.LogsobjCfg,
		BuilderMetrics: builderMetrics,

		IndexMergeObserver:  params.IndexMergeObserver,
		NewLogMergeObserver: params.NewLogMergeObserver,
	})
	if err != nil {
		return nil, err
	}

	return &Worker{
		inner:          inner,
		endpoint:       params.Endpoint,
		handler:        listener,
		builderMetrics: builderMetrics,
	}, nil
}

// RegisterWorkerServer registers the [wire.Listener] of the inner worker as
// http.Handler on the provided router.
func (w *Worker) RegisterWorkerServer(router *mux.Router) {
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
	w.builderMetrics.Unregister(reg)
}
