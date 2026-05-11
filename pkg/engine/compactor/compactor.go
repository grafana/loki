package compactor

import (
	"context"
	"fmt"
	"net"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/engine"
)

// Compactor is the dataobj-compactor target service. It hosts an embedded
// engine.Scheduler that compaction workers connect to, and (in a
// follow-up change) a coordinator polling loop. This scaffold ships only
// the lifecycle plumbing: the coordinator polling loop is a no-op stub
// that blocks until shutdown.
type Compactor struct {
	*services.BasicService

	cfg       Config
	logger    log.Logger
	scheduler *engine.Scheduler
}

// New constructs a Compactor. The scaffold takes no bucket dependency;
// the coordinator loop that needs object-storage access is added in a
// follow-up change.
func New(cfg Config, logger log.Logger) (*Compactor, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	advertiseAddr, err := resolveAdvertiseAddr(cfg.Scheduler.AdvertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("dataobj compactor: resolve scheduler advertise address: %w", err)
	}

	scheduler, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:        log.With(logger, "component", "dataobj-compaction-scheduler"),
		AdvertiseAddr: advertiseAddr,
		Endpoint:      cfg.Scheduler.Endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("dataobj compactor: construct scheduler: %w", err)
	}

	c := &Compactor{
		cfg:       cfg,
		logger:    logger,
		scheduler: scheduler,
	}
	c.BasicService = services.NewBasicService(c.starting, c.running, c.stopping)
	return c, nil
}

// Scheduler returns the embedded engine.Scheduler. Used by the Loki
// module-init wiring to register the scheduler's HTTP handler on the
// Loki router (when running in remote-transport mode) and to register
// scheduler metrics.
func (c *Compactor) Scheduler() *engine.Scheduler {
	return c.scheduler
}

// starting is the BasicService starting callback. It brings the embedded
// scheduler service up.
func (c *Compactor) starting(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting dataobj compactor",
		"scheduler_endpoint", c.cfg.Scheduler.Endpoint,
	)

	if err := services.StartAndAwaitRunning(ctx, c.scheduler.Service()); err != nil {
		return fmt.Errorf("dataobj compactor: start scheduler service: %w", err)
	}
	return nil
}

// running is the BasicService running callback. The coordinator polling
// loop is a no-op stub in this scaffold; it simply blocks until shutdown
// so the service stays healthy.
func (c *Compactor) running(ctx context.Context) error {
	level.Info(c.logger).Log("msg", "dataobj compactor running")
	<-ctx.Done()
	return nil
}

// stopping is the BasicService stopping callback. It tears down the
// embedded scheduler service. The error parameter is the reason the
// service is shutting down; it is logged but does not gate cleanup.
func (c *Compactor) stopping(runErr error) error {
	if runErr != nil {
		level.Warn(c.logger).Log("msg", "dataobj compactor stopping after run error", "err", runErr)
	}
	// TODO: the dskit stopping() callback signature doesn't accept a
	// context, so we use Background(). Once the coordinator polling loop
	// is doing real work, revisit adding an upper bound (e.g., a derived
	// context with a configurable shutdown deadline) so a stuck scheduler
	// can't wedge Loki shutdown indefinitely.
	if err := services.StopAndAwaitTerminated(context.Background(), c.scheduler.Service()); err != nil {
		level.Warn(c.logger).Log("msg", "stop dataobj compaction scheduler", "err", err)
		return fmt.Errorf("dataobj compactor: stop scheduler: %w", err)
	}
	return nil
}

// resolveAdvertiseAddr converts the raw config string into a *net.TCPAddr
// suitable for engine.SchedulerParams.AdvertiseAddr. Empty string keeps
// the scheduler in-process-only (no remote listener) by returning nil.
func resolveAdvertiseAddr(raw string) (net.Addr, error) {
	if raw == "" {
		return nil, nil
	}
	addr, err := net.ResolveTCPAddr("tcp", raw)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", raw, err)
	}
	return addr, nil
}
