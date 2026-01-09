package passthroughgateway

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight prometheus.Gauge
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)

	return &Metrics{
		RequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "lokistack_gateway_requests_total",
				Help: "Total number of requests processed by the LokiStack gateway.",
			},
			[]string{"method", "code"},
		),
		RequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "lokistack_gateway_request_duration_seconds",
				Help:    "Duration of requests processed by the LokiStack gateway.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
		RequestsInFlight: factory.NewGauge(
			prometheus.GaugeOpts{
				Name: "lokistack_gateway_requests_in_flight",
				Help: "Current number of requests being processed by the LokiStack gateway.",
			},
		),
	}
}
