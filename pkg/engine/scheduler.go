package engine

import (
	"net"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
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
}

// Scheduler is a service that can schedule tasks to connected [Worker]
// instances.
type Scheduler struct {
	// Our public API is a lightweight wrapper around the internal API.

	inner    *scheduler.Scheduler
	listener wire.Listener
}

// NewScheduler creates a new Scheduler. Use [Scheduler.Service] to manage the
// lifecycle of the Scheduler.
func NewScheduler(params SchedulerParams) (*Scheduler, error) {
	if params.Logger == nil {
		params.Logger = log.NewNopLogger()
	}

	var listener wire.Listener

	if params.AdvertiseAddr != nil {
		listener = wire.NewGRPCListener(
			params.AdvertiseAddr,
			wire.WithGRPCListenerLogger(params.Logger),
		)
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
		listener: listener,
	}, nil
}

// RegisterSchedulerServer registers the [wire.GRPCListener] of the inner
// scheduler on the provided gRPC server.
//
// RegisterSchedulerServer is a no-op if an advertise address is not provided.
func (s *Scheduler) RegisterSchedulerServer(srv *grpc.Server) {
	grpcLis, ok := s.listener.(*wire.GRPCListener)
	if !ok {
		return
	}
	wirepb.RegisterWireServiceServer(srv, grpcLis)
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
