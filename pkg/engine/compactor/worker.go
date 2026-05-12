package compactor

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
)

// WorkerParams collects the constructor arguments for NewWorker.
// Keeping these as a struct (rather than positional args) mirrors
// engine.WorkerParams and makes future field additions backwards-compatible.
type WorkerParams struct {
	Config WorkerConfig

	Bucket    objstore.Bucket
	Metastore metastore.Metastore // may be nil in tests where no task ever arrives

	Logger     log.Logger
	Registerer prometheus.Registerer
}

// Worker is the dataobj-compactor-worker target service. It wraps an
// embedded engine.Worker pointed at the compaction scheduler's DNS-SRV
// record. This scaffold ships only the lifecycle plumbing; the real
// CompactionMerge / IndexConsolidate executors arrive in follow-up changes.
type Worker struct {
	*services.BasicService

	cfg    WorkerConfig
	logger log.Logger
	worker *engine.Worker
}

// NewWorker constructs a compaction Worker. Returns an error if the worker
// config is unusable (missing scheduler lookup address, missing advertise
// address, unparseable advertise addr) or if the engine worker constructor
// rejects the parameters.
//
// Worker-specific config preconditions are enforced here rather than in
// Config.Validate() so that planner-only deployments (which keep the worker
// fields at their defaults) validate cleanly.
func NewWorker(params WorkerParams) (*Worker, error) {
	if params.Bucket == nil {
		return nil, errors.New("dataobj compaction worker: bucket is required")
	}
	if params.Config.SchedulerLookupAddress == "" {
		return nil, errors.New("dataobj compaction worker: scheduler_lookup_address is required")
	}
	if params.Config.AdvertiseAddr == "" {
		// engine.NewWorker rejects nil AdvertiseAddr when no LocalScheduler
		// is set; the compaction worker never sets LocalScheduler. Catch
		// this here with a clear operator-facing message rather than
		// letting the generic engine error bubble up.
		return nil, errors.New("dataobj compaction worker: advertise_addr is required")
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
		Logger:    log.With(logger, "component", "dataobj-compaction-worker-inner"),
		Bucket:    params.Bucket,
		Metastore: params.Metastore,
		Config: engine.WorkerConfig{
			WorkerThreads:           params.Config.WorkerThreads,
			SchedulerLookupAddress:  params.Config.SchedulerLookupAddress,
			SchedulerLookupInterval: params.Config.SchedulerLookupInterval,
		},
		// ExecutorConfig left zero: real CompactionMerge/IndexConsolidate
		// executors arrive in follow-up changes. The zero value produces
		// a no-op task-results cache and a default batch size.
		Executor:      engine.ExecutorConfig{},
		AdvertiseAddr: advertiseAddr,
		Endpoint:      params.Config.Endpoint,
		// LocalScheduler left nil: the compaction worker only ever
		// connects to remote schedulers via DNS-SRV.
	}, registerer)
	if err != nil {
		return nil, fmt.Errorf("dataobj compaction worker: construct engine worker: %w", err)
	}

	w := &Worker{
		cfg:    params.Config,
		logger: logger,
		worker: inner,
	}
	w.BasicService = services.NewBasicService(w.starting, w.running, w.stopping)
	return w, nil
}

// Inner returns the embedded engine.Worker. Used by the Loki module-init
// wiring to register the worker's HTTP frame handler on the Loki router
// (when an advertise address is configured) and to register/unregister
// worker metrics.
func (w *Worker) Inner() *engine.Worker { return w.worker }

// starting brings the embedded engine worker up.
func (w *Worker) starting(ctx context.Context) error {
	level.Info(w.logger).Log(
		"msg", "starting dataobj compaction worker",
		"scheduler_lookup_address", w.cfg.SchedulerLookupAddress,
		"endpoint", w.cfg.Endpoint,
	)

	if err := services.StartAndAwaitRunning(ctx, w.worker.Service()); err != nil {
		return fmt.Errorf("dataobj compaction worker: start engine worker: %w", err)
	}
	return nil
}

// running blocks until shutdown. The engine worker's own service does the
// actual task work; this callback exists only to satisfy BasicService.
func (w *Worker) running(ctx context.Context) error {
	level.Info(w.logger).Log("msg", "dataobj compaction worker running")
	<-ctx.Done()
	return nil
}

// stopping tears down the embedded engine worker. The error parameter is
// the reason the service is shutting down; it is logged but does not
// gate cleanup.
func (w *Worker) stopping(runErr error) error {
	if runErr != nil {
		level.Warn(w.logger).Log("msg", "dataobj compaction worker stopping after run error", "err", runErr)
	}
	// TODO: the dskit stopping() callback signature doesn't accept a
	// context, so we use Background(). When real executors run inside the
	// worker, revisit adding an upper bound (e.g., a derived context with
	// a configurable shutdown deadline) so a stuck in-flight task can't
	// wedge Loki shutdown indefinitely.
	if err := services.StopAndAwaitTerminated(context.Background(), w.worker.Service()); err != nil {
		level.Warn(w.logger).Log("msg", "stop dataobj compaction worker engine worker", "err", err)
		return fmt.Errorf("dataobj compaction worker: stop engine worker: %w", err)
	}
	return nil
}
