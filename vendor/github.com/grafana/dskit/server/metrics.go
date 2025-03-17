// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/server/metrics.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package server

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
)

type Metrics struct {
	TCPConnections           *prometheus.GaugeVec
	TCPConnectionsLimit      *prometheus.GaugeVec
	RequestDuration          *prometheus.HistogramVec
	PerTenantRequestDuration *prometheus.HistogramVec
	PerTenantRequestTotal    *prometheus.CounterVec
	ReceivedMessageSize      *prometheus.HistogramVec
	SentMessageSize          *prometheus.HistogramVec
	InflightRequests         *prometheus.GaugeVec
	RequestThroughput        *prometheus.HistogramVec
}

func NewServerMetrics(cfg Config) *Metrics {
	reg := promauto.With(cfg.registererOrDefault())

	return &Metrics{
		TCPConnections: reg.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: cfg.MetricsNamespace,
			Name:      "tcp_connections",
			Help:      "Current number of accepted TCP connections.",
		}, []string{"protocol"}),
		TCPConnectionsLimit: reg.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: cfg.MetricsNamespace,
			Name:      "tcp_connections_limit",
			Help:      "The max number of TCP connections that can be accepted (0 means no limit).",
		}, []string{"protocol"}),
		RequestDuration: reg.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:                       cfg.MetricsNamespace,
			Name:                            "request_duration_seconds",
			Help:                            "Time (in seconds) spent serving HTTP requests.",
			Buckets:                         instrument.DefBuckets,
			NativeHistogramBucketFactor:     cfg.MetricsNativeHistogramFactor,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"method", "route", "status_code", "ws"}),
		PerTenantRequestDuration: reg.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:                       cfg.MetricsNamespace,
			Name:                            "per_tenant_request_duration_seconds",
			Help:                            "Time (in seconds) spent serving HTTP requests for a particular tenant.",
			Buckets:                         instrument.DefBuckets,
			NativeHistogramBucketFactor:     cfg.MetricsNativeHistogramFactor,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"method", "route", "status_code", "ws", "tenant"}),
		PerTenantRequestTotal: reg.NewCounterVec(prometheus.CounterOpts{
			Namespace: cfg.MetricsNamespace,
			Name:      "per_tenant_request_total",
			Help:      "Total count of requests for a particular tenant.",
		}, []string{"method", "route", "status_code", "ws", "tenant"}),
		ReceivedMessageSize: reg.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.MetricsNamespace,
			Name:      "request_message_bytes",
			Help:      "Size (in bytes) of messages received in the request.",
			Buckets:   middleware.BodySizeBuckets,
		}, []string{"method", "route"}),
		SentMessageSize: reg.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.MetricsNamespace,
			Name:      "response_message_bytes",
			Help:      "Size (in bytes) of messages sent in response.",
			Buckets:   middleware.BodySizeBuckets,
		}, []string{"method", "route"}),
		InflightRequests: reg.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: cfg.MetricsNamespace,
			Name:      "inflight_requests",
			Help:      "Current number of inflight requests.",
		}, []string{"method", "route"}),
		RequestThroughput: reg.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:                       cfg.MetricsNamespace,
			Name:                            "request_throughput_" + cfg.Throughput.Unit,
			Help:                            "Server throughput of running requests.",
			ConstLabels:                     prometheus.Labels{"cutoff_ms": strconv.FormatInt(cfg.Throughput.LatencyCutoff.Milliseconds(), 10)},
			Buckets:                         instrument.DefBuckets,
			NativeHistogramBucketFactor:     cfg.MetricsNativeHistogramFactor,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"method", "route"}),
	}
}
