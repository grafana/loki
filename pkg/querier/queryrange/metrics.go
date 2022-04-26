package queryrange

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

type Metrics struct {
	*queryrangebase.InstrumentMiddlewareMetrics
	*queryrangebase.RetryMiddlewareMetrics
	*logql.MapperMetrics
	*SplitByMetrics
	*LogResultCacheMetrics
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		InstrumentMiddlewareMetrics: queryrangebase.NewInstrumentMiddlewareMetrics(registerer),
		RetryMiddlewareMetrics:      queryrangebase.NewRetryMiddlewareMetrics(registerer),
		MapperMetrics:               logql.NewMapperMetrics(registerer),
		SplitByMetrics:              NewSplitByMetrics(registerer),
		LogResultCacheMetrics:       NewLogResultCacheMetrics(registerer),
	}
}
