package querytee

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
)

const (
	resultSuccess = "success"
	resultFailed  = "fail"
)

type ProxyMetrics struct {
	durationMetric         *prometheus.HistogramVec
	responsesComparedTotal *prometheus.CounterVec
}

func NewProxyMetrics(registerer prometheus.Registerer) *ProxyMetrics {
	m := &ProxyMetrics{
		durationMetric: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cortex_querytee",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving HTTP requests.",
			Buckets:   instrument.DefBuckets,
		}, []string{"backend", "method", "route", "status_code"}),
		responsesComparedTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex_querytee",
			Name:      "responses_compared_total",
			Help:      "Total number of responses compared per route name by result",
		}, []string{"route_name", "result"}),
	}

	return m
}
