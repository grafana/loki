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
	TotalCorruptions       prometheus.Counter
	TotalFailedRepairs     prometheus.Counter
	TotalSucceededRepairs  prometheus.Counter
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	m := Metrics{r: r}
	m.NumActiveSeries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:      "ruler_wal_storage_active_series",
		Namespace: "loki",
		Help:      "Current number of active series being tracked by the WAL storage",
	})

	m.NumDeletedSeries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:      "ruler_wal_storage_deleted_series",
		Namespace: "loki",
		Help:      "Current number of series marked for deletion from memory",
	})

	m.TotalCreatedSeries = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_storage_created_series_total",
		Namespace: "loki",
		Help:      "Total number of created series appended to the WAL",
	})

	m.TotalRemovedSeries = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_storage_removed_series_total",
		Namespace: "loki",
		Help:      "Total number of created series removed from the WAL",
	})

	m.TotalAppendedSamples = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_samples_appended_total",
		Namespace: "loki",
		Help:      "Total number of samples appended to the WAL",
	})

	m.TotalAppendedExemplars = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_exemplars_appended_total",
		Namespace: "loki",
		Help:      "Total number of exemplars appended to the WAL",
	})

	m.TotalCorruptions = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_corruptions_total",
		Namespace: "loki",
		Help:      "Total number of corruptions observed in the WAL",
	})

	m.TotalFailedRepairs = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_corruptions_repair_failed_total",
		Namespace: "loki",
		Help:      "Total number of corruptions unsuccessfully repaired in the WAL",
	})

	m.TotalSucceededRepairs = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "ruler_wal_corruptions_repair_succeeded_total",
		Namespace: "loki",
		Help:      "Total number of corruptions successfully repaired in the WAL",
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
			m.TotalCorruptions,
			m.TotalFailedRepairs,
			m.TotalSucceededRepairs,
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
		m.TotalCorruptions,
		m.TotalFailedRepairs,
		m.TotalSucceededRepairs,
	}
	for _, c := range cs {
		m.r.Unregister(c)
	}
}
