package compactor

import (
	"fmt"
	"net"
	"net/http"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// Scheduler is the compactor-side scheduler wrapper. It mirrors the
// *engine.Scheduler wrapper used by the query path but lives here so the
// coordinator can access the internal *scheduler.Scheduler.
type Scheduler struct {
	inner    *scheduler.Scheduler
	endpoint string
	listener wire.Listener
	handler  http.Handler
}

// newScheduler constructs a compactor Scheduler. Empty AdvertiseAddr keeps
// the scheduler in-process-only (no remote listener registered).
func newScheduler(cfg SchedulerConfig, logger log.Logger) (*Scheduler, error) {
	advertiseAddr, err := resolveAdvertiseAddr(cfg.AdvertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("dataobj compaction scheduler: resolve advertise address: %w", err)
	}

	var (
		listener wire.Listener
		handler  http.Handler
	)
	if advertiseAddr != nil {
		rl := wire.NewHTTP2Listener(advertiseAddr, wire.WithHTTP2ListenerLogger(logger))
		listener, handler = rl, rl
	} else {
		listener = &wire.Local{Address: wire.LocalScheduler}
	}

	inner, err := scheduler.New(scheduler.Config{
		Logger:   logger,
		Listener: listener,
	})
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		inner:    inner,
		endpoint: cfg.Endpoint,
		listener: listener,
		handler:  handler,
	}, nil
}

// Service returns the lifecycle service for the embedded scheduler. The Loki
// module wiring drives Start / Stop through this.
func (s *Scheduler) Service() services.Service { return s.inner.Service() }

// RegisterMetrics registers the scheduler's Prometheus metrics with reg.
func (s *Scheduler) RegisterMetrics(reg prometheus.Registerer) error {
	return s.inner.RegisterMetrics(reg)
}

// UnregisterMetrics unregisters the scheduler's Prometheus metrics from reg.
func (s *Scheduler) UnregisterMetrics(reg prometheus.Registerer) {
	s.inner.UnregisterMetrics(reg)
}

// RegisterSchedulerServer installs the wire.Listener HTTP handler on the
// supplied router at the configured endpoint. No-op when no advertise
// address was provided (in-process-only mode).
func (s *Scheduler) RegisterSchedulerServer(router *mux.Router) {
	if s.handler == nil {
		return
	}
	router.Path(s.endpoint).Methods("POST").Handler(s.handler)
}

// resolveAdvertiseAddr converts the raw config string into a net.Addr suitable
// for wire.NewHTTP2Listener. Empty string returns nil (keeps the scheduler
// in-process-only).
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
