package indexgateway

import (
	"context"
	"errors"
	"net/http"
	"sync"

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

// inFlightGate reserves RPC slots for the lifetime of a client request.
type inFlightGate struct {
	gate  gate.Gate
	limit int

	mu       sync.Mutex
	reserved int
}

// newInFlightGate creates a non-blocking gate. A max of zero disables the cap
// while retaining metrics for the configured limit and reserved slots.
func newInFlightGate(maxConcurrent int, reg prometheus.Registerer) *inFlightGate {
	limit := max(0, maxConcurrent)
	underlying := gate.NewNoop()
	if limit > 0 {
		underlying = gate.NewRejecting(limit)
	}
	return &inFlightGate{gate: gate.NewInstrumented(reg, limit, underlying), limit: limit}
}

// reserve takes up to want slots in one admission decision. Callers must
// request at least one slot and release all reserved slots after their RPCs finish.
func (g *inFlightGate) reserve(ctx context.Context, want int) (int, error) {
	if g.limit > 0 {
		g.mu.Lock()
		defer g.mu.Unlock()
		// Try one slot when full so the underlying gate records the rejection.
		want = max(1, min(want, g.limit-g.reserved))
	}
	for acquired := range want {
		if err := g.gate.Start(ctx); err != nil {
			for range acquired {
				g.gate.Done()
			}
			return 0, err
		}
	}
	if g.limit > 0 {
		g.reserved += want
	}
	return want, nil
}

func (g *inFlightGate) release(slots int) {
	if g.limit > 0 {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reserved -= slots
	}
	for range slots {
		g.gate.Done()
	}
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
