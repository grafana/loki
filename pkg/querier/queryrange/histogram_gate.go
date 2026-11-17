package queryrange

import (
	"context"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// NewHistogramGateMiddleware refuses a query that uses the histogram functions while they are not enabled in
// opts. The engine refuses it too, but the frontend only runs the engine for a query that it shards or splits by
// range. It passes any other query on, and a querier would refuse it after the frontend had looked up the index
// and split it. A frontend that is set up differently from its queriers would also serve a part of the histogram
// queries and refuse the rest, depending on how each of them was planned.
func NewHistogramGateMiddleware(opts logql.EngineOpts) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			if expr := metricQueryExpr(r); expr != nil {
				if err := opts.CheckHistograms(expr); err != nil {
					return nil, err
				}
			}
			return next.Do(ctx, r)
		})
	})
}

// metricQueryExpr returns the parsed query of a range or an instant query request. It is nil for any other
// request, and for one that has no plan: the engine reports that.
func metricQueryExpr(r queryrangebase.Request) syntax.Expr {
	switch r := r.(type) {
	case *LokiRequest:
		if r.Plan != nil {
			return r.Plan.AST
		}
	case *LokiInstantRequest:
		if r.Plan != nil {
			return r.Plan.AST
		}
	}
	return nil
}
