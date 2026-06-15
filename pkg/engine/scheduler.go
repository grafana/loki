package engine

import (
	"net"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

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

	// Endpoint is retained for backwards compatibility. It is unused by the
	// gRPC transport, which serves on the shared gRPC server at a fixed method
	// path.
	Endpoint string
}

// Scheduler is a service that can schedule tasks to connected [Worker]
// instances.
type Scheduler struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner    *scheduler.Scheduler
	listener wire.Listener

	// grpcListener is the remote transport listener when running in remote
	// (distributed) mode. nil when the scheduler only serves in-process
	// connections.
	grpcListener *wire.GRPCListener
}

// NewScheduler creates a new Scheduler. Use [Scheduler.Service] to manage the
// lifecycle of the Scheduler.
func NewScheduler(params SchedulerParams) (*Scheduler, error) {
	if params.Logger == nil {
		params.Logger = log.NewNopLogger()
	}

	var (
		listener     wire.Listener
		grpcListener *wire.GRPCListener
	)

	if params.AdvertiseAddr != nil {
		grpcListener = wire.NewGRPCListener(
			params.AdvertiseAddr,
			wire.WithGRPCListenerLogger(params.Logger),
		)
		listener = grpcListener
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
		inner:        inner,
		listener:     listener,
		grpcListener: grpcListener,
	}, nil
}

// RegisterSchedulerServer registers the [wire.Listener] of the inner scheduler
// on the provided gRPC server.
//
// RegisterSchedulerServer is a no-op if an advertise address is not provided.
func (s *Scheduler) RegisterSchedulerServer(srv *grpc.Server) {
	if s.grpcListener == nil {
		return
	}
	s.grpcListener.Register(srv)
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
