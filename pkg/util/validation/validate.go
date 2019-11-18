package validation

import "github.com/prometheus/client_golang/prometheus"

const (
	discardReasonLabel = "reason"

	// RateLimited is one of the values for the reason to discard samples.
	// Declared here to avoid duplication in ingester and distributor.
	RateLimited = "rate_limited"
)

// DiscardedBytes is a metric of the total discarded bytes, by reason.
var DiscardedBytes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "loki_discarded_bytes_total",
		Help: "The total number of bytes that were discarded.",
	},
	[]string{discardReasonLabel, "user"},
)

// DiscardedSamples is a metric of the number of discarded samples, by reason.
var DiscardedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "loki_discarded_samples_total",
		Help: "The total number of samples that were discarded.",
	},
	[]string{discardReasonLabel, "user"},
)

func init() {
	prometheus.MustRegister(DiscardedSamples)
}
