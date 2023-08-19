package queryrange

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

type Metrics struct {
	*queryrangebase.InstrumentMiddlewareMetrics
	*queryrangebase.RetryMiddlewareMetrics
	*MiddlewareMapperMetrics
	*SplitByMetrics
	*LogResultCacheMetrics
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

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		InstrumentMiddlewareMetrics: queryrangebase.NewInstrumentMiddlewareMetrics(registerer),
		RetryMiddlewareMetrics:      queryrangebase.NewRetryMiddlewareMetrics(registerer),
		MiddlewareMapperMetrics:     NewMiddlewareMapperMetrics(registerer),
		SplitByMetrics:              NewSplitByMetrics(registerer),
		LogResultCacheMetrics:       NewLogResultCacheMetrics(registerer),
		ResultsCacheMetrics:         queryrangebase.NewResultsCacheMetrics(registerer),
	}
}
