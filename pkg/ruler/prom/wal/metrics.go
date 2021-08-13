package wal

import "github.com/prometheus/client_golang/prometheus"

type StorageMetrics struct {
	r prometheus.Registerer

	NumActiveSeries        prometheus.Gauge
	NumDeletedSeries       prometheus.Gauge
	TotalCreatedSeries     prometheus.Counter
	TotalRemovedSeries     prometheus.Counter
	TotalAppendedSamples   prometheus.Counter
	TotalAppendedExemplars prometheus.Counter
}

func NewStorageMetrics(r prometheus.Registerer) *StorageMetrics {
	m := StorageMetrics{r: r}
	m.NumActiveSeries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agent_wal_storage_active_series",
		Help: "Current number of active series being tracked by the WAL storage",
	})

	m.NumDeletedSeries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agent_wal_storage_deleted_series",
		Help: "Current number of series marked for deletion from memory",
	})

	m.TotalCreatedSeries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_wal_storage_created_series_total",
		Help: "Total number of created series appended to the WAL",
	})

	m.TotalRemovedSeries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_wal_storage_removed_series_total",
		Help: "Total number of created series removed from the WAL",
	})

	m.TotalAppendedSamples = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_wal_samples_appended_total",
		Help: "Total number of samples appended to the WAL",
	})

	m.TotalAppendedExemplars = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_wal_exemplars_appended_total",
		Help: "Total number of exemplars appended to the WAL",
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
		)
	}

	return &m
}

func (m *StorageMetrics) Unregister() {
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
	}
	for _, c := range cs {
		m.r.Unregister(c)
	}
}
