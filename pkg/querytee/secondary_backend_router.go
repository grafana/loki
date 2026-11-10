package querytee

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// Values of the decision label of the secondary decisions metric. A selection strategy adds its own
// reasons for not selecting a request.
const (
	decisionSent          = "sent"
	decisionStrategyError = "strategy_error"
	decisionInvalidTenant = "invalid_tenant"
)

// SelectionStrategy decides which requests the secondary backend receives. The proxy calls it from
// many request goroutines, before it calls the primary backend. An implementation must be safe for
// concurrent use, cheap, and free of I/O.
type SelectionStrategy interface {
	// Select reports whether the secondary backend gets a copy of the request. The decision covers
	// the whole request. The proxy does not split its time range. The request can be of any type
	// that the codec decodes, not only a range or instant query.
	Select(tenants []string, req queryrangebase.Request) Selection
}

// Selection is the answer of a SelectionStrategy for one request.
type Selection struct {
	Selected bool
	// Reason is a value of the decision label. It comes from a small fixed set per strategy. A
	// strategy sets it only when Selected is false. The router counts a selected request as "sent".
	Reason string
}

// secondaryBackendRouter decides per request whether the secondary backend gets a copy. A router
// without a strategy sends every request and counts no decision. The proxy builds no strategy when
// it has no secondary backend.
type secondaryBackendRouter struct {
	strategy  SelectionStrategy
	metrics   *ProxyMetrics
	routeName string
	logger    log.Logger
}

func newSecondaryBackendRouter(strategy SelectionStrategy, metrics *ProxyMetrics, routeName string, logger log.Logger) *secondaryBackendRouter {
	return &secondaryBackendRouter{strategy: strategy, metrics: metrics, routeName: routeName, logger: logger}
}

// shouldRoute reports whether the secondary backend gets a copy of the request, and counts the
// decision. A request without a valid tenant stays off the secondary backend.
//
// shouldRoute recovers from a panic in the strategy, so the client still gets the primary backend's
// response. The request then stays off the secondary backend, because there is no decision.
func (r *secondaryBackendRouter) shouldRoute(ctx context.Context, req queryrangebase.Request) (selected bool) {
	if r.strategy == nil {
		return true
	}

	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		level.Warn(r.logger).Log("msg", "failed to extract tenant IDs", "route", r.routeName, "err", err)
		r.metrics.secondaryBackendDecisions.WithLabelValues(r.routeName, decisionInvalidTenant).Inc()
		return false
	}

	decision := decisionSent
	defer func() {
		if p := recover(); p != nil {
			level.Error(r.logger).Log("msg", "panic in the selection strategy", "route", r.routeName, "tenants", strings.Join(tenants, "|"), "query", req.GetQuery(), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			selected, decision = false, decisionStrategyError
		}
		r.metrics.secondaryBackendDecisions.WithLabelValues(r.routeName, decision).Inc()
	}()

	selection := r.strategy.Select(tenants, req)
	if !selection.Selected {
		decision = selection.Reason
	}
	return selection.Selected
}
