package queryrange

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

const (
	gRPC = "gRPC"
)

type QueryHandlerMetrics struct {
	InflightRequests *prometheus.GaugeVec
}

func NewQueryHandlerMetrics(registerer prometheus.Registerer, metricsNamespace string) *QueryHandlerMetrics {
	return &QueryHandlerMetrics{
		InflightRequests: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "inflight_requests",
			Help:      "Current number of inflight requests.",
		}, []string{"method", "route"}),
	}
}

type Instrument struct {
	*QueryHandlerMetrics
}

var _ queryrangebase.Middleware = Instrument{}

// Wrap implements the queryrangebase.Middleware
func (i Instrument) Wrap(next queryrangebase.Handler) queryrangebase.Handler {
	return queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		route := fmt.Sprintf("%T", r)
		inflight := i.InflightRequests.WithLabelValues(gRPC, route)
		inflight.Inc()
		defer inflight.Dec()

		return next.Do(ctx, r)
	})
}
