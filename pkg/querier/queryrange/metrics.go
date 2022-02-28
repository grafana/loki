package queryrange

import (
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	*queryrangebase.InstrumentMiddlewareMetrics
	*queryrangebase.RetryMiddlewareMetrics
	*logql.ShardingMetrics
	*SplitByMetrics
	*LogCacheMetrics
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		InstrumentMiddlewareMetrics: queryrangebase.NewInstrumentMiddlewareMetrics(registerer),
		RetryMiddlewareMetrics:      queryrangebase.NewRetryMiddlewareMetrics(registerer),
		ShardingMetrics:             logql.NewShardingMetrics(registerer),
		SplitByMetrics:              NewSplitByMetrics(registerer),
		LogCacheMetrics:             NewLogCacheMetrics(registerer),
	}
}
