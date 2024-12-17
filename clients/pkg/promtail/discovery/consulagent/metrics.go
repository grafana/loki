// This code was adapted from the consul service discovery
// package in prometheus: https://github.com/prometheus/prometheus/blob/main/discovery/consul/metrics.go
// which is copyrighted: 2015 The Prometheus Authors
// and licensed under the Apache License, Version 2.0 (the "License");

package consulagent

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/prometheus/prometheus/discovery"
)

var _ discovery.DiscovererMetrics = (*consulMetrics)(nil)

type consulMetrics struct {
	rpcFailuresCount prometheus.Counter
	rpcDuration      *prometheus.SummaryVec

	servicesRPCDuration prometheus.Observer
	serviceRPCDuration  prometheus.Observer

	metricRegisterer discovery.MetricRegisterer
}

func newDiscovererMetrics(reg prometheus.Registerer, _ discovery.RefreshMetricsInstantiator) discovery.DiscovererMetrics {
	m := &consulMetrics{
		rpcFailuresCount: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "sd_consulagent_rpc_failures_total",
				Help:      "The number of Consul Agent RPC call failures.",
			}),
		rpcDuration: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace:  namespace,
				Name:       "sd_consulagent_rpc_duration_seconds",
				Help:       "The duration of a Consul Agent RPC call in seconds.",
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			},
			[]string{"endpoint", "call"},
		),
	}

	m.metricRegisterer = discovery.NewMetricRegisterer(reg, []prometheus.Collector{
		m.rpcFailuresCount,
		m.rpcDuration,
	})

	// Initialize metric vectors.
	m.servicesRPCDuration = m.rpcDuration.WithLabelValues("agent", "services")
	m.serviceRPCDuration = m.rpcDuration.WithLabelValues("agent", "service")

	return m
}

// Register implements discovery.DiscovererMetrics.
func (m *consulMetrics) Register() error {
	return m.metricRegisterer.RegisterMetrics()
}

// Unregister implements discovery.DiscovererMetrics.
func (m *consulMetrics) Unregister() {
	m.metricRegisterer.UnregisterMetrics()
}
