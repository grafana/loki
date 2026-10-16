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

// newInFlightGate creates a non-blocking gate. A max of zero disables the cap
// while retaining metrics for the configured limit and in-flight requests.
func newInFlightGate(maxConcurrent int, reg prometheus.Registerer) gate.Gate {
	if maxConcurrent <= 0 {
		return gate.NewInstrumented(reg, 0, gate.NewNoop())
	}
	return gate.NewInstrumented(reg, maxConcurrent, gate.NewRejecting(maxConcurrent))
}

// mapInFlightGateError maps a capacity rejection to HTTP 503.
func mapInFlightGateError(err error) error {
	if errors.Is(err, gate.ErrMaxConcurrent) {
		return httpgrpc.Error(http.StatusServiceUnavailable, "the index gateway client is at its in-flight request limit")
	}
	return err
}

// isServiceUnavailable reports whether err carries an HTTP 503 response.
// The status alone does not identify why the request was unavailable.
func isServiceUnavailable(err error) bool {
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	return ok && resp.Code == http.StatusServiceUnavailable
}
