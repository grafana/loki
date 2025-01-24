package queryrange

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

type Metrics struct {
	*queryrangebase.InstrumentMiddlewareMetrics
	*queryrangebase.RetryMiddlewareMetrics
	*MiddlewareMapperMetrics
	*SplitByMetrics
	*LogResultCacheMetrics
	*QueryMetrics
	*queryrangebase.ResultsCacheMetrics
}

type MiddlewareMapperMetrics struct {
	shardMapper *logql.MapperMetrics
	rangeMapper *logql.MapperMetrics
}

func NewMiddlewareMapperMetrics(registerer prometheus.Registerer) *MiddlewareMapperMetrics {
	return &MiddlewareMapperMetrics{
		shardMapper: logql.NewShardMapperMetrics(registerer),
		rangeMapper: logql.NewRangeMapperMetrics(registerer),
	}
}

func NewMetrics(registerer prometheus.Registerer, metricsNamespace string) *Metrics {
	return &Metrics{
		InstrumentMiddlewareMetrics: queryrangebase.NewInstrumentMiddlewareMetrics(registerer, metricsNamespace),
		RetryMiddlewareMetrics:      queryrangebase.NewRetryMiddlewareMetrics(registerer, metricsNamespace),
		MiddlewareMapperMetrics:     NewMiddlewareMapperMetrics(registerer),
		SplitByMetrics:              NewSplitByMetrics(registerer),
		LogResultCacheMetrics:       NewLogResultCacheMetrics(registerer),
		QueryMetrics:                NewMiddlewareQueryMetrics(registerer, metricsNamespace),
		ResultsCacheMetrics:         queryrangebase.NewResultsCacheMetrics(registerer),
	}
}

type QueryMetrics struct {
	receivedLabelFilters prometheus.Histogram
}

func NewMiddlewareQueryMetrics(registerer prometheus.Registerer, metricsNamespace string) *QueryMetrics {
	return &QueryMetrics{
		receivedLabelFilters: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "query_frontend_query_label_filters",
			Help:      "Number of label matcher expressions per query.",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 9), // 1 -> 256
		}),
	}
}

// QueryMetricsMiddleware can be inserted into the middleware chain to expose timing information.
func QueryMetricsMiddleware(metrics *QueryMetrics) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			var expr syntax.Expr
			switch r := req.(type) {
			case *LokiRequest:
				if r.Plan != nil {
					expr = r.Plan.AST
				}
			case *LokiInstantRequest:
				if r.Plan != nil {
					expr = r.Plan.AST
				}
			default:
				return next.Do(ctx, req)
			}

			// The plan should always be present, but if it's not, we'll parse the query to get the filters.
			if expr == nil {
				var err error
				expr, err = syntax.ParseExpr(req.GetQuery())
				if err != nil {
					return nil, err
				}
			}

			filters := v1.ExtractTestableLabelMatchers(expr)
			metrics.receivedLabelFilters.Observe(float64(len(filters)))

			return next.Do(ctx, req)
		})
	})
}
