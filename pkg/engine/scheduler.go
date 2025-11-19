package engine

import (
	"net"
	"net/http"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

type SchedulerParams struct {
	Logger log.Logger // Logger for optional log messages.

	// Address to advertise to workers. Must be set when the scheduler runs in
	// remote transport mode.
	//
	// If nil, the scheduler only listens for in-process connections.
	AdvertiseAddr net.Addr

	// Absolute path of the endpoint where the frame handler is registered.
	// Used for connecting to scheduler and other workers.
	Endpoint string
}

// Scheduler is a service that can schedule tasks to connected [Worker]
// instances.
type Scheduler struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner    *scheduler.Scheduler
	endpoint string
	listener wire.Listener
	handler  http.Handler
}

// NewScheduler creates a new Scheduler. Use [Scheduler.Service] to manage the
// lifecycle of the Scheduler.
func NewScheduler(params SchedulerParams) (*Scheduler, error) {
	if params.Logger == nil {
		params.Logger = log.NewNopLogger()
	}
	if params.Endpoint == "" {
		params.Endpoint = "/api/v2/frame"
	}

	var (
		listener wire.Listener
		handler  http.Handler
	)

	if params.AdvertiseAddr != nil {
		remoteListener := wire.NewHTTP2Listener(
			params.AdvertiseAddr,
			wire.WithHTTP2ListenerLogger(params.Logger),
			wire.WithHTTP2ListenerMaxPendingConns(10),
		)
		listener, handler = remoteListener, remoteListener
	} else {
		listener = &wire.Local{Address: wire.LocalScheduler}
	}

	inner, err := scheduler.New(scheduler.Config{
		Logger:   params.Logger,
		Listener: listener,
	})
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		inner:    inner,
		endpoint: params.Endpoint,
		listener: listener,
		handler:  handler,
	}, nil
}

// RegisterSchedulerServer registers the [wire.Listener] of the inner scheduler
// as http.Handler on the provided router.
//
// RegisterSchedulerServer is a no-op if an advertise address is not provided.
func (s *Scheduler) RegisterSchedulerServer(router *mux.Router) {
	if s.handler == nil {
		return
	}
	router.Path(s.endpoint).Methods("POST").Handler(s.handler)
}

// Service returns the service used to manage the lifecycle of the Scheduler.
func (s *Scheduler) Service() services.Service {
	return s.inner.Service()
}

// RegisterMetrics registers metrics about s to report to reg.
func (s *Scheduler) RegisterMetrics(reg prometheus.Registerer) error {
	return s.inner.RegisterMetrics(reg)
}

// UnregisterMetrics unregisters metrics about s from reg.
func (s *Scheduler) UnregisterMetrics(reg prometheus.Registerer) {
	s.inner.UnregisterMetrics(reg)
}
