package indexgateway

import (
	"errors"
	"net/http"

	"github.com/grafana/dskit/gate"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/client_golang/prometheus"
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

func mapGateError(err error) error {
	if errors.Is(err, gate.ErrGateTimeout) {
		return httpgrpc.Error(http.StatusServiceUnavailable, "the index gateway is at its concurrent request limit; retry another replica")
	}
	return err
}
