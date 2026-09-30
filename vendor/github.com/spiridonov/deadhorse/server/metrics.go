package server

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics are package-level (rather than per-TextServer/per-store) so that a
// process running exactly one of each -- the normal case, see cmd/deadhorse
// -- gets one set of series on the default registry without any wiring.
// Tests that spin up many short-lived TextServers/stores in one binary just
// share these same series; nothing here is asserted on in tests, so that's
// harmless.
var (
	// keysGauge reports the store's key count by generation ("hot"/"cold"),
	// refreshed periodically by (*store).updateKeyMetrics -- see
	// metricsUpdateInterval. It's a plain Gauge rather than a GaugeFunc
	// because a GaugeFunc's callback is fixed at registration time, and
	// there's no single store to close over at package init.
	keysGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "deadhorse",
		Name:      "keys",
		Help:      "Number of keys currently held by the store, by generation.",
	}, []string{"generation"})

	connectionsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "deadhorse",
		Name:      "connections",
		Help:      "Number of currently open DHP/1 client connections.",
	})

	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "deadhorse",
		Name:      "requests_total",
		Help:      "Number of DHP/1 command lines handled, by command.",
	}, []string{"command"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:                       "deadhorse",
		Name:                            "request_duration_seconds",
		Help:                            "Time to handle one DHP/1 command line, by command.",
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 1 * time.Hour,
	}, []string{"command"})

	throttleEntriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "deadhorse",
		Name:      "throttle_entries_total",
		Help:      "Number of THROTTLE entries evaluated, by mode (real/peek) and result (admitted/throttled/err).",
	}, []string{"mode", "result"})

	throttleBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace:                       "deadhorse",
		Name:                            "throttle_batch_size",
		Help:                            "Number of entries carried by one THROTTLE line.",
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 1 * time.Hour,
	})

	lineLength = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace:                       "deadhorse",
		Name:                            "line_length_bytes",
		Help:                            "Length in bytes of each protocol line read, excluding the terminator.",
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 1 * time.Hour,
	})

	lineTooLongTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "deadhorse",
		Name:      "line_too_long_total",
		Help:      "Number of connections dropped for exceeding the configured maximum line size.",
	})
)
