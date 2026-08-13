package queryrange

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestExprHasApproxCountDistinct(t *testing.T) {
	expr, err := syntax.ParseExpr(`approx_count_distinct(mac, {job="devices"}[1h]) by (version)`)
	require.NoError(t, err)
	require.True(t, ExprHasApproxCountDistinct(expr))

	expr, err = syntax.ParseExpr(`count_over_time({job="devices"}[1h])`)
	require.NoError(t, err)
	require.False(t, ExprHasApproxCountDistinct(expr))
}

func TestApproxCountDistinctFeatureGate(t *testing.T) {
	next := base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
		return &LokiPromResponse{}, nil
	})

	t.Run("rejects when disabled", func(t *testing.T) {
		mw := newApproxCountDistinctFeatureGateMiddleware(fakeLimits{}, nil)
		req := &LokiInstantRequest{
			Query: `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`,
			Plan: &plan.QueryPlan{
				AST: mustParseSample(t, `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`),
			},
		}
		_, err := mw.Wrap(next).Do(user.InjectOrgID(context.Background(), "fake"), req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not enabled")
	})

	t.Run("allows when globally enabled", func(t *testing.T) {
		mw := newApproxCountDistinctFeatureGateMiddleware(fakeLimits{}, []string{logql.SupportApproxCountDistinct})
		req := &LokiInstantRequest{
			Query: `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`,
			Plan: &plan.QueryPlan{
				AST: mustParseSample(t, `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`),
			},
		}
		_, err := mw.Wrap(next).Do(user.InjectOrgID(context.Background(), "fake"), req)
		require.NoError(t, err)
	})

	t.Run("rejects range queries", func(t *testing.T) {
		mw := newApproxCountDistinctFeatureGateMiddleware(fakeLimits{}, []string{logql.SupportApproxCountDistinct})
		req := &LokiRequest{
			Query: `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`,
			Plan: &plan.QueryPlan{
				AST: mustParseSample(t, `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`),
			},
		}
		_, err := mw.Wrap(next).Do(user.InjectOrgID(context.Background(), "fake"), req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "only supported on instant queries")
	})
}

func TestApproxCountDistinctCacheBypass(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query", nil)
	q := req.URL.Query()
	q.Set("query", `approx_count_distinct(mac, {job="devices"}[1h]) by (version)`)
	q.Set("time", time.Unix(100, 0).Format(time.RFC3339Nano))
	req.URL.RawQuery = q.Encode()

	decoded, err := DefaultCodec.DecodeRequest(context.Background(), req, nil)
	require.NoError(t, err)
	instant, ok := decoded.(*LokiInstantRequest)
	require.True(t, ok)
	require.True(t, instant.CachingOptions.Disabled)
}

func mustParseSample(t *testing.T, q string) syntax.Expr {
	t.Helper()
	expr, err := syntax.ParseExpr(q)
	require.NoError(t, err)
	return expr
}
