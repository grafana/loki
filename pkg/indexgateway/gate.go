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

// newInFlightGate builds the cap on requests a single GatewayClient may have in
// flight. Unlike the server gate it never queues: a request arriving at a full
// gate is rejected straight away. A max of zero disables the cap.
func newInFlightGate(max int, reg prometheus.Registerer) gate.Gate {
	if max <= 0 {
		return gate.NewNoop()
	}
	return gate.NewInstrumented(reg, max, gate.NewRejecting(max))
}

// mapInFlightGateError converts a client-side in-flight rejection into the 503
// the index gateway itself returns when it sheds load.
func mapInFlightGateError(err error) error {
	if errors.Is(err, gate.ErrMaxConcurrent) {
		return httpgrpc.Error(http.StatusServiceUnavailable, "the index gateway client is at its in-flight request limit")
	}
	return err
}

// isLoadShed reports whether err carries a 503, the status both the index
// gateway and its client use to say a request was refused to protect capacity
// rather than because it could not be served.
func isLoadShed(err error) bool {
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	return ok && resp.Code == http.StatusServiceUnavailable
}
