package indexgateway

import (
	"errors"

	"github.com/grafana/dskit/gate"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newQueryGate builds the admission control gate shared by all index gateway
// RPC handlers. A MaxConcurrent of zero disables admission control entirely.
func newQueryGate(cfg Config, reg prometheus.Registerer) gate.Gate {
	if cfg.MaxConcurrent <= 0 {
		return gate.NewNoop()
	}
	g := gate.NewBlocking(cfg.MaxConcurrent)
	g = gate.NewInstrumented(prometheus.WrapRegistererWithPrefix("loki_index_gateway_", reg), cfg.MaxConcurrent, g)
	return gate.NewTimeoutGate(cfg.MaxConcurrentQueueTimeout, g)
}

// mapGateError converts a gate queue timeout into a retryable gRPC
// Unavailable status. All other errors, including context cancellation while
// queued, pass through unchanged.
func mapGateError(err error) error {
	if errors.Is(err, gate.ErrGateTimeout) {
		return status.Error(codes.Unavailable, "the index gateway is at its concurrent request limit; retry another replica")
	}
	return err
}
