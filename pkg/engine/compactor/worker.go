package compactor

import (
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
)

// WorkerParams collects the constructor arguments for NewWorker.
type WorkerParams struct {
	Config WorkerConfig

	Bucket    objstore.Bucket
	Metastore metastore.Metastore // may be nil in tests where no task ever arrives

	Logger     log.Logger
	Registerer prometheus.Registerer
}

// Worker is the dataobj-compaction-worker target service. It wraps an
// engine.Worker pointed at the compaction scheduler's DNS-SRV record.
type Worker struct {
	inner *engine.Worker
}

// NewWorker constructs a compaction Worker. Returns an error if the
// worker config is unusable or if the engine worker constructor rejects
// the parameters.
//
// Worker-specific config preconditions are enforced here rather than in
// Config.Validate() so that planner-only deployments (which keep the
// worker fields at their defaults) validate cleanly.
func NewWorker(params WorkerParams) (*Worker, error) {
	if params.Bucket == nil {
		return nil, errors.New("dataobj compaction worker: bucket is required")
	}
	if params.Config.SchedulerLookupAddress == "" {
		return nil, errors.New("dataobj compaction worker: scheduler_lookup_address is required")
	}
	if params.Config.AdvertiseAddr == "" {
		// engine.NewWorker rejects nil AdvertiseAddr when no LocalScheduler
		// is set; the compaction worker never sets LocalScheduler.
		return nil, errors.New("dataobj compaction worker: advertise_addr is required")
	}
	if params.Config.Endpoint == "" {
		return nil, errors.New("dataobj compaction worker: endpoint is required")
	}

	logger := params.Logger
	if logger == nil {
		logger = log.NewNopLogger()
	}
	registerer := params.Registerer
	if registerer == nil {
		registerer = prometheus.NewRegistry()
	}

	// Reuse the package-local resolveAdvertiseAddr from compactor.go so
	// planner and worker both surface identical addr-parse errors.
	advertiseAddr, err := resolveAdvertiseAddr(params.Config.AdvertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("dataobj compaction worker: resolve worker advertise address: %w", err)
	}

	inner, err := engine.NewWorker(engine.WorkerParams{
		Logger:    log.With(logger, "component", "dataobj-compaction-worker"),
		Bucket:    params.Bucket,
		Metastore: params.Metastore,
		Config: engine.WorkerConfig{
			WorkerThreads:           params.Config.WorkerThreads,
			SchedulerLookupAddress:  params.Config.SchedulerLookupAddress,
			SchedulerLookupInterval: params.Config.SchedulerLookupInterval,
		},
		// ExecutorConfig left zero: real IndexMerge and
		// TableOfContentsConsolidate executors (and LogMerge for v2.0)
		// arrive in follow-up changes. The zero value produces a no-op
		// task-results cache and a default batch size.
		Executor:      engine.ExecutorConfig{},
		AdvertiseAddr: advertiseAddr,
		Endpoint:      params.Config.Endpoint,
		// LocalScheduler left nil: the compaction worker only ever
		// connects to remote schedulers via DNS-SRV.
	}, registerer)
	if err != nil {
		return nil, fmt.Errorf("dataobj compaction worker: construct engine worker: %w", err)
	}

	return &Worker{inner: inner}, nil
}

// Service returns the service used to manage the lifecycle of the Worker.
func (w *Worker) Service() services.Service { return w.inner.Service() }

// RegisterWorkerServer registers the worker's HTTP frame handler on the
// provided router. No-op when the worker was constructed without an
// advertise address.
func (w *Worker) RegisterWorkerServer(router *mux.Router) { w.inner.RegisterWorkerServer(router) }

// RegisterMetrics registers metrics about w to report to reg.
func (w *Worker) RegisterMetrics(reg prometheus.Registerer) error {
	return w.inner.RegisterMetrics(reg)
}

// UnregisterMetrics unregisters metrics about w from reg.
func (w *Worker) UnregisterMetrics(reg prometheus.Registerer) {
	w.inner.UnregisterMetrics(reg)
}
