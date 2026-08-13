package queryrange

import (
	"context"
	"net/http"
	"slices"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// ExprHasApproxCountDistinct reports whether expr contains approx_count_distinct
// or an internal count-distinct sketch plan node.
func ExprHasApproxCountDistinct(expr syntax.Expr) bool {
	if expr == nil {
		return false
	}
	found := false
	expr.Walk(func(e syntax.Expr) bool {
		switch e.(type) {
		case *syntax.LabelAggregationExpr, *syntax.CountDistinctSketchExpr:
			found = true
			return false
		}
		return true
	})
	return found
}

func requestHasApproxCountDistinct(r base.Request) bool {
	var planned syntax.Expr
	switch req := r.(type) {
	case *LokiRequest:
		if req.Plan != nil {
			planned = req.Plan.AST
		}
	case *LokiInstantRequest:
		if req.Plan != nil {
			planned = req.Plan.AST
		}
	}
	if ExprHasApproxCountDistinct(planned) {
		return true
	}
	parsed, err := syntax.ParseExpr(r.GetQuery())
	return err == nil && ExprHasApproxCountDistinct(parsed)
}

func approxCountDistinctEnabled(globallyEnabled []string, limits Limits, tenantIDs []string) bool {
	if slices.Contains(globallyEnabled, logql.SupportApproxCountDistinct) {
		return true
	}
	tenantAggs := validation.IntersectionPerTenant(tenantIDs, func(tenant string) []string {
		return limits.ShardAggregations(tenant)
	})
	return slices.Contains(tenantAggs, logql.SupportApproxCountDistinct)
}

// newApproxCountDistinctFeatureGateMiddleware rejects approx_count_distinct
// unless it is enabled for the request tenants, and rejects range queries.
func newApproxCountDistinctFeatureGateMiddleware(limits Limits, globallyEnabled []string) base.Middleware {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		return base.HandlerFunc(func(ctx context.Context, r base.Request) (base.Response, error) {
			if !requestHasApproxCountDistinct(r) {
				return next.Do(ctx, r)
			}

			if _, ok := r.(*LokiRequest); ok {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, "approx_count_distinct is only supported on instant queries")
			}

			if slices.Contains(globallyEnabled, logql.SupportApproxCountDistinct) {
				return next.Do(ctx, r)
			}

			tenantIDs, err := tenant.TenantIDs(ctx)
			if err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
			}
			if !approxCountDistinctEnabled(nil, limits, tenantIDs) {
				return nil, httpgrpc.Errorf(
					http.StatusBadRequest,
					"approx_count_distinct is not enabled. See -limits.shard_aggregations",
				)
			}
			return next.Do(ctx, r)
		})
	})
}
