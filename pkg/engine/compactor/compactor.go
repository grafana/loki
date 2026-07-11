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
)

// PlannerParams collects the constructor arguments for [New].
//
// Config.Enabled is the user-facing YAML/CLI gate consulted by the Loki
// module-init wiring: when false, the planner module returns a nil service
// and [New] is never called. [New] therefore treats every Planner it
// constructs as enabled — Bucket and MetastoreWriter are unconditionally
// required.
type PlannerParams struct {
	Config          Config
	Bucket          objstore.Bucket                  // required
	MetastoreWriter *metastore.TableOfContentsWriter // required
	Logger          log.Logger
	Registerer      prometheus.Registerer
}

// Planner is the dataobj-compaction-planner target service. It hosts an
// embedded scheduler that compaction workers connect to and drives the
// stateless per-cycle coordinator.
type Planner struct {
	*services.BasicService

	cfg         Config
	logger      log.Logger
	scheduler   *Scheduler
	coordinator *coordinator
}

// New constructs a compaction Planner. Returns an error if the scheduler
// cannot be constructed or if any required dependency is missing.
//
// The disabled-compactor case (Config.Enabled = false) is handled at the
// module-init layer by returning nil from initDataObjCompactionPlanner; New
// is only reached on the enabled path.
func New(params PlannerParams) (*Planner, error) {
	logger := params.Logger
	if logger == nil {
		logger = log.NewNopLogger()
	}

	if params.Bucket == nil {
		return nil, errors.New("dataobj compaction planner: bucket is required")
	}
	if params.MetastoreWriter == nil {
		return nil, errors.New("dataobj compaction planner: metastore writer is required")
	}

	sched, err := newScheduler(
		params.Config.Scheduler,
		log.With(logger, "component", "dataobj-compaction-scheduler"),
	)
	if err != nil {
		return nil, fmt.Errorf("dataobj compaction planner: construct scheduler: %w", err)
	}

	p := &Planner{
		cfg:       params.Config,
		logger:    logger,
		scheduler: sched,
		coordinator: newCoordinator(
			params.Config,
			log.With(logger, "component", "dataobj-compaction-coordinator"),
			params.Bucket,
			sched.inner,
			params.MetastoreWriter,
			params.Registerer,
		),
	}
	p.BasicService = services.NewBasicService(p.starting, p.running, p.stopping)
	return p, nil
}

// Scheduler returns the embedded compactor Scheduler. Used by the Loki
// module-init wiring to register the scheduler's HTTP handler on the
// Loki router (when running in remote-transport mode) and to register
// scheduler metrics.
func (c *Planner) Scheduler() *Scheduler {
	return c.scheduler
}

// starting is the BasicService starting callback. It brings the embedded
// scheduler service up.
func (c *Planner) starting(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compaction planner",
		"scheduler_endpoint", c.cfg.Scheduler.Endpoint,
	)

	if err := services.StartAndAwaitRunning(ctx, c.scheduler.Service()); err != nil {
		return fmt.Errorf("dataobj compaction planner: start scheduler service: %w", err)
	}
	return nil
}

// running is the BasicService running callback. It drives the stateless
// coordinator polling loop until the context is cancelled.
func (c *Planner) running(ctx context.Context) error {
	level.Info(c.logger).Log("msg", "dataobj compaction planner running")
	err := c.coordinator.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil // clean shutdown
	}
	return err
}

// stopping is the BasicService stopping callback. It tears down the
// embedded scheduler service. The error parameter is the reason the
// service is shutting down; it is logged but does not gate cleanup.
func (c *Planner) stopping(runErr error) error {
	if runErr != nil {
		level.Warn(c.logger).Log("msg", "dataobj compaction planner stopping after run error", "err", runErr)
	}
	// TODO: the dskit stopping() callback signature doesn't accept a
	// context, so we use Background(). Once the coordinator polling loop
	// is doing real work, revisit adding an upper bound (e.g., a derived
	// context with a configurable shutdown deadline) so a stuck scheduler
	// can't wedge Loki shutdown indefinitely.
	if err := services.StopAndAwaitTerminated(context.Background(), c.scheduler.Service()); err != nil {
		level.Warn(c.logger).Log("msg", "stop dataobj compaction scheduler", "err", err)
		return fmt.Errorf("dataobj compaction planner: stop scheduler: %w", err)
	}
	return nil
}
