package engine

import (
	"net"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

type SchedulerParams struct {
	Logger log.Logger // Logger for optional log messages.

	// Listen address of the underlying network listener.
	// This must only be set when the scheduler runs in remote transport mode
	// and accepts remote connections.
	// If nil, the scheduler only listens for in-process connections.
	Addr net.Addr

	// Absolute path of the endpoint where the frame handler is registered.
	// Used for connecting to scheduler and other workers.
	Endpoint string
}

// Scheduler is a service that can schedule tasks to connected [Worker]
// instances.
type Scheduler struct {
	// Our public API is a lightweight wrapper around the internal API.

	Endpoint string
	inner    *scheduler.Scheduler
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

	var listener wire.Listener
	if params.Addr != nil {
		listener = wire.NewHTTP2Listener(
			params.Addr,
			wire.WithHTTP2ListenerLogger(params.Logger),
			wire.WithHTTP2ListenerMaxPendingConns(10),
		)
	}

	inner, err := scheduler.New(scheduler.Config{
		Logger:         params.Logger,
		RemoteListener: listener,
	})
	if err != nil {
		return nil, err
	}

	return &Scheduler{Endpoint: params.Endpoint, inner: inner}, nil
}

// RegisterWorkerServer registers the [wire.Listener] of the inner scheduler as http.Handler on the privided router.
func (s *Scheduler) RegisterSchedulerServer(router *mux.Router) {
	router.Path(s.Endpoint).Methods("POST").Handler(s.inner.Handler())
}

// Service returns the service used to manage the lifecycle of the Scheduler.
func (s *Scheduler) Service() services.Service {
	return s.inner.Service()
}
