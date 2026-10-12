package queryrange

import (
	"context"
	"time"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// Values of the outcome label of the eligibility metric.
const (
	dataObjOutcomeEligible            = "eligible"
	dataObjOutcomeUnsupportedQuery    = "unsupported_query"
	dataObjOutcomeStreamFirstDisabled = "stream_first_disabled"
	dataObjOutcomeDataObjectsDisabled = "data_objects_disabled"
	dataObjOutcomeOutsideDataObjRange = "outside_data_object_range"
)

// dataObjOutcomes lists every value of the outcome label.
var dataObjOutcomes = []string{
	dataObjOutcomeEligible,
	dataObjOutcomeUnsupportedQuery,
	dataObjOutcomeStreamFirstDisabled,
	dataObjOutcomeDataObjectsDisabled,
	dataObjOutcomeOutsideDataObjRange,
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
		eligibility.WithLabelValues(outcome)
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
	// A request without a valid tenant ID has no tenants, so it is never eligible. The middleware
	// then clears the options that the request carried.
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
		m.metrics.eligibility.WithLabelValues(dataObjOutcomeUnsupportedQuery).Inc()
		return false
	}

	res := dataObjResolve(m.cfg, m.limits, tenantIDs, expr, start, end)
	m.metrics.eligibility.WithLabelValues(res.outcome).Inc()
	return res.outcome == dataObjOutcomeEligible
}
