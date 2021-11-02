// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package http

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ClientMetrics holds a collection of metrics that can be used to instrument a http client.
// By setting this field in HTTPClientConfig, NewHTTPClient will create an instrumented client.
type ClientMetrics struct {
	inFlightGauge            prometheus.Gauge
	requestTotalCount        *prometheus.CounterVec
	dnsLatencyHistogram      *prometheus.HistogramVec
	tlsLatencyHistogram      *prometheus.HistogramVec
	requestDurationHistogram *prometheus.HistogramVec
}

// NewClientMetrics creates a new instance of ClientMetrics.
// It will also register the metrics with the included register.
// This ClientMetrics should be re-used for diff clients with the same purpose.
// e.g. 1 ClientMetrics should be used for all the clients that talk to Alertmanager.
func NewClientMetrics(reg prometheus.Registerer) *ClientMetrics {
	var m ClientMetrics

	m.inFlightGauge = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Subsystem: "http_client",
		Name:      "in_flight_requests",
		Help:      "A gauge of in-flight requests.",
	})

	m.requestTotalCount = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Subsystem: "http_client",
		Name:      "request_total",
		Help:      "Total http client request by code and method.",
	}, []string{"code", "method"})

	m.dnsLatencyHistogram = promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: "http_client",
			Name:      "dns_duration_seconds",
			Help:      "Trace dns latency histogram.",
			Buckets:   []float64{0.025, .05, .1, .5, 1, 5, 10},
		},
		[]string{"event"},
	)

	m.tlsLatencyHistogram = promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: "http_client",
			Name:      "tls_duration_seconds",
			Help:      "Trace tls latency histogram.",
			Buckets:   []float64{0.025, .05, .1, .5, 1, 5, 10},
		},
		[]string{"event"},
	)

	m.requestDurationHistogram = promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: "http_client",
			Name:      "request_duration_seconds",
			Help:      "A histogram of request latencies.",
			Buckets:   []float64{0.025, .05, .1, .5, 1, 5, 10},
		},
		[]string{"code", "method"},
	)

	return &m
}

// InstrumentedRoundTripper instruments the given roundtripper with metrics that are
// registered in the provided ClientMetrics.
func InstrumentedRoundTripper(tripper http.RoundTripper, m *ClientMetrics) http.RoundTripper {
	if m == nil {
		return tripper
	}

	trace := &promhttp.InstrumentTrace{
		DNSStart: func(t float64) {
			m.dnsLatencyHistogram.WithLabelValues("dns_start").Observe(t)
		},
		DNSDone: func(t float64) {
			m.dnsLatencyHistogram.WithLabelValues("dns_done").Observe(t)
		},
		TLSHandshakeStart: func(t float64) {
			m.tlsLatencyHistogram.WithLabelValues("tls_handshake_start").Observe(t)
		},
		TLSHandshakeDone: func(t float64) {
			m.tlsLatencyHistogram.WithLabelValues("tls_handshake_done").Observe(t)
		},
	}

	return promhttp.InstrumentRoundTripperInFlight(
		m.inFlightGauge,
		promhttp.InstrumentRoundTripperCounter(
			m.requestTotalCount,
			promhttp.InstrumentRoundTripperTrace(
				trace,
				promhttp.InstrumentRoundTripperDuration(
					m.requestDurationHistogram,
					tripper,
				),
			),
		),
	)
}
