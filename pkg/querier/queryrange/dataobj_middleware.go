package queryrange

import (
	"context"
	"time"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v4/pkg/querier/plan"
	"github.com/grafana/loki/v4/pkg/querier/queryrange/queryrangebase"
)

// dataObjOutcomes lists every value of the outcome label of the eligibility metric.
var dataObjOutcomes = []DataObjResolutionOutcome{
	DataObjOutcomeEligible,
	DataObjOutcomeUnsupportedQuery,
	DataObjOutcomeStreamFirstDisabled,
	DataObjOutcomeDataObjectsDisabled,
	DataObjOutcomeOutsideDataObjRange,
}

// DataObjMetrics holds the metrics of the data-object routing in the query-frontend.
type DataObjMetrics struct {
	eligibility *prometheus.CounterVec
}

// NewDataObjMetrics registers the data-object routing metrics.
func NewDataObjMetrics(registerer prometheus.Registerer, metricsNamespace string) *DataObjMetrics {
	eligibility := promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "query_frontend_dataobj_eligibility_total",
		Help:      "Query requests that the query-frontend sends to queriers, by data-object eligibility outcome. Sharding and range splitting turn one user query into many requests, and a retry sends a request again. Each request adds one count.",
	}, []string{"outcome"})

	// A series that does not exist yet hides its first count from rate() and increase().
	for _, outcome := range dataObjOutcomes {
		eligibility.WithLabelValues(string(outcome))
	}

	return &DataObjMetrics{eligibility: eligibility}
}

// dataObjEligibilityMiddleware sets the data-object options of every metric request it passes on.
//
// It must run last in the chain, after the middlewares that split, shard and rewrite requests. A
// querier judges the query it receives, and the rewritten sub-queries are the ones it receives.
type dataObjEligibilityMiddleware struct {
	next    queryrangebase.Handler
	cfg     Config
	limits  Limits
	metrics *DataObjMetrics
}

// newDataObjEligibilityMiddleware returns a middleware that sets the data-object options from the
// eligibility of each request. It overwrites the options that a request carries.
//
// When the config turns data objects off, the middleware returns the next handler as it is.
func newDataObjEligibilityMiddleware(cfg Config, limits Limits, metrics *DataObjMetrics) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		if !cfg.DataObjEnabled {
			return next
		}
		return &dataObjEligibilityMiddleware{next: next, cfg: cfg, limits: limits, metrics: metrics}
	})
}

func (m *dataObjEligibilityMiddleware) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	// This error should not happen: other middlewares read the tenant IDs first. Without tenants,
	// the request is not eligible.
	tenantIDs, _ := tenant.TenantIDs(ctx)

	// The request may be in use elsewhere, so the middleware sets the options on a copy.
	switch req := r.(type) {
	case *LokiRequest:
		return m.next.Do(ctx, req.WithDataObjOptions(dataObjOptionsFor(m.eligible(tenantIDs, req.Plan, req.Query, req.StartTs, req.EndTs))))
	case *LokiInstantRequest:
		return m.next.Do(ctx, req.WithDataObjOptions(dataObjOptionsFor(m.eligible(tenantIDs, req.Plan, req.Query, req.TimeTs, req.TimeTs))))
	default:
		return m.next.Do(ctx, r)
	}
}

// eligible runs the eligibility check of a request and counts the outcome.
func (m *dataObjEligibilityMiddleware) eligible(tenantIDs []string, queryPlan *plan.QueryPlan, query string, start, end time.Time) bool {
	expr, err := planExpr(queryPlan, query)
	if err != nil {
		m.metrics.eligibility.WithLabelValues(string(DataObjOutcomeUnsupportedQuery)).Inc()
		return false
	}

	res := dataObjResolve(m.cfg, m.limits, tenantIDs, expr, start, end)
	m.metrics.eligibility.WithLabelValues(string(res.Outcome)).Inc()
	return res.Eligible()
}
