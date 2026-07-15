// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package wal

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	r prometheus.Registerer

	NumActiveSeries        prometheus.Gauge
	NumDeletedSeries       prometheus.Gauge
	TotalCreatedSeries     prometheus.Counter
	TotalRemovedSeries     prometheus.Counter
	TotalAppendedSamples   prometheus.Counter
	TotalAppendedExemplars prometheus.Counter
	DiskSize               prometheus.Gauge
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	m := Metrics{r: r}
	m.NumActiveSeries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "storage_active_series",
		Help: "Current number of active series being tracked by a tenant's WAL storage",
	})

	m.NumDeletedSeries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "storage_deleted_series",
		Help: "Current number of series marked for deletion from memory",
	})

	m.TotalCreatedSeries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "storage_created_series_total",
		Help: "Total number of created series appended to a tenant's WAL",
	})

	m.TotalRemovedSeries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "storage_removed_series_total",
		Help: "Total number of created series removed from a tenant's WAL",
	})

	m.TotalAppendedSamples = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "samples_appended_total",
		Help: "Total number of samples appended to a tenant's WAL",
	})

	m.TotalAppendedExemplars = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "exemplars_appended_total",
		Help: "Total number of exemplars appended to a tenant's WAL",
	})

	m.DiskSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "disk_size",
		Help: "Size of each tenant's WAL on disk",
	})

	// why do the metrics not show up?
	// are the metrics for the wal indexed by the config name?
	// don't think so -> add
	if r != nil {
		r.MustRegister(
			m.NumActiveSeries,
			m.NumDeletedSeries,
			m.TotalCreatedSeries,
			m.TotalRemovedSeries,
			m.TotalAppendedSamples,
			m.TotalAppendedExemplars,
			m.DiskSize,
		)
	}

	return &m
}

func (m *Metrics) Unregister() {
	if m.r == nil {
		return
	}
	cs := []prometheus.Collector{
		m.NumActiveSeries,
		m.NumDeletedSeries,
		m.TotalCreatedSeries,
		m.TotalRemovedSeries,
		m.TotalAppendedSamples,
		m.TotalAppendedExemplars,
		m.DiskSize,
	}
	for _, c := range cs {
		m.r.Unregister(c)
	}
}
