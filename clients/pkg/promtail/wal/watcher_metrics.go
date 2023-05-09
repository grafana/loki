package wal

import "github.com/prometheus/client_golang/prometheus"

type WatcherMetrics struct {
	recordsRead               *prometheus.CounterVec
	recordDecodeFails         *prometheus.CounterVec
	droppedWriteNotifications *prometheus.CounterVec
	timerSegmentReads         *prometheus.CounterVec
	currentSegment            *prometheus.GaugeVec
	watchersRunning           *prometheus.GaugeVec
}

func NewWatcherMetrics(reg prometheus.Registerer) *WatcherMetrics {
	m := &WatcherMetrics{
		recordsRead: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "wal_watcher",
				Name:      "records_read_total",
				Help:      "Number of records read by the WAL watcher from the WAL.",
			},
			[]string{"id"},
		),
		recordDecodeFails: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "wal_watcher",
				Name:      "record_decode_failures_total",
				Help:      "Number of records read by the WAL watcher that resulted in an error when decoding.",
			},
			[]string{"id"},
		),
		droppedWriteNotifications: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "wal_watcher",
				Name:      "dropped_write_notifications",
				Help:      "Number of dropped write notifications due to having one already buffered.",
			},
			[]string{"id"},
		),
		timerSegmentReads: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "wal_watcher",
				Name:      "read_segment_by_timer",
				Help:      "Number of segment reads triggered by the backup timer firing.",
			},
			[]string{"id"},
		),
		currentSegment: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "loki",
				Subsystem: "wal_watcher",
				Name:      "current_segment",
				Help:      "Current segment the WAL watcher is reading records from.",
			},
			[]string{"id"},
		),
		watchersRunning: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "loki",
				Subsystem: "wal_watcher",
				Name:      "running",
				Help:      "Number of WAL watchers running.",
			},
			nil,
		),
	}

	if reg != nil {
		reg.MustRegister(m.recordsRead)
		reg.MustRegister(m.recordDecodeFails)
		reg.MustRegister(m.droppedWriteNotifications)
		reg.MustRegister(m.timerSegmentReads)
		reg.MustRegister(m.currentSegment)
		reg.MustRegister(m.watchersRunning)
	}

	return m
}
