package ruler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var samplesEvicted = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rules_queue_samples_evicted_total",
	Help:      "Number of samples evicted from queue; buffer is full!",
}, []string{"user_id", "group_key"})

var samplesBufferedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rules_queue_samples_buffered_total",
	Help:      "Number of samples buffered in queue in total.",
}, []string{"user_id", "group_key"})

var samplesBufferedCurrent = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "loki",
	Name:      "recording_rules_queue_samples_buffered_current",
	Help:      "Number of samples currently buffered in queue.",
}, []string{"user_id", "group_key"})
