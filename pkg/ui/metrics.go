package ui

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// GoldfishMetrics contains all goldfish-related metrics
type GoldfishMetrics struct {
	queryDuration     *prometheus.HistogramVec
	requests          *prometheus.CounterVec
	queryRows         *prometheus.HistogramVec
	errors            *prometheus.CounterVec
	dbConnections     *prometheus.GaugeVec
	dbConnectionWait  *prometheus.HistogramVec
	dbConnectionError *prometheus.CounterVec
}

// NewGoldfishMetrics creates and registers goldfish metrics
func NewGoldfishMetrics(reg prometheus.Registerer) *GoldfishMetrics {
	return &GoldfishMetrics{
		queryDuration: promauto.With(reg).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "query_duration_seconds",
				Help:      "Database query latency",
				Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
			},
			[]string{"query_type", "status"},
		),
		requests: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "requests_total",
				Help:      "Total number of goldfish API requests",
			},
			[]string{"status"},
		),
		queryRows: promauto.With(reg).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "query_rows_total",
				Help:      "Number of rows returned from database queries",
				Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1 to 1024
			},
			[]string{"query_type"},
		),
		errors: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "errors_total",
				Help:      "Total number of errors by type",
			},
			[]string{"error_type"},
		),
		dbConnections: promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "db_connections_active",
				Help:      "Number of active database connections",
			},
			[]string{"state"}, // active, idle
		),
		dbConnectionWait: promauto.With(reg).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "db_connection_wait_seconds",
				Help:      "Time spent waiting for database connections",
				Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to 1s
			},
			[]string{"pool"},
		),
		dbConnectionError: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "loki",
				Subsystem: "ui_goldfish",
				Name:      "db_connection_errors_total",
				Help:      "Total number of database connection errors",
			},
			[]string{"error_type"},
		),
	}
}

// RecordQueryDuration records the duration of a database query
func (m *GoldfishMetrics) RecordQueryDuration(queryType, status string, duration float64) {
	m.queryDuration.WithLabelValues(queryType, status).Observe(duration)
}

// IncrementRequests increments the request counter
func (m *GoldfishMetrics) IncrementRequests(status string) {
	m.requests.WithLabelValues(status).Inc()
}

// RecordQueryRows records the number of rows returned
func (m *GoldfishMetrics) RecordQueryRows(queryType string, rows float64) {
	m.queryRows.WithLabelValues(queryType).Observe(rows)
}

// IncrementErrors increments the error counter
func (m *GoldfishMetrics) IncrementErrors(errorType string) {
	m.errors.WithLabelValues(errorType).Inc()
}

// SetDBConnections sets the current connection count
func (m *GoldfishMetrics) SetDBConnections(state string, count float64) {
	m.dbConnections.WithLabelValues(state).Set(count)
}

// RecordDBConnectionWait records connection wait time
func (m *GoldfishMetrics) RecordDBConnectionWait(pool string, duration float64) {
	m.dbConnectionWait.WithLabelValues(pool).Observe(duration)
}

// IncrementDBConnectionErrors increments connection errors
func (m *GoldfishMetrics) IncrementDBConnectionErrors(errorType string) {
	m.dbConnectionError.WithLabelValues(errorType).Inc()
}
