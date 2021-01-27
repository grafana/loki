package healthcheck

import (
	"context"

	"github.com/gogo/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/util/services"
)

// HealthCheck fulfills the grpc_health_v1.HealthServer interface by ensuring
// the services being managed by the provided service manager are healthy.
type HealthCheck struct {
	sm *services.Manager
}

// New returns a new HealthCheck for the provided service manager.
func New(sm *services.Manager) *HealthCheck {
	return &HealthCheck{
		sm: sm,
	}
}

// Check implements the grpc healthcheck.
func (h *HealthCheck) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if !h.isHealthy() {
		return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, nil
	}

	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Watch implements the grpc healthcheck.
func (h *HealthCheck) Watch(_ *grpc_health_v1.HealthCheckRequest, _ grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watching is not supported")
}

// isHealthy returns whether the Cortex instance should be considered healthy.
func (h *HealthCheck) isHealthy() bool {
	states := h.sm.ServicesByState()

	// Given this is an health check endpoint for the whole instance, we should consider
	// it healthy after all services have been started (running) and until all
	// services are terminated. Some services, like ingesters, are still
	// fully functioning while stopping.
	if len(states[services.New]) > 0 || len(states[services.Starting]) > 0 || len(states[services.Failed]) > 0 {
		return false
	}

	return len(states[services.Running]) > 0 || len(states[services.Stopping]) > 0
}
