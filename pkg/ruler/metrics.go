package ruler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var samplesDiscarded = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rule_queue_samples_discarded_total",
	Help:      "Number of samples discarded from queue - buffer is full.",
}, []string{"user_id", "group_key"})

var samplesBuffered = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rule_queue_samples_buffered_total",
	Help:      "Number of samples buffered in queue.",
}, []string{"user_id", "group_key"})
