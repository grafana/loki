package ruler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var samplesEvicted = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rules_samples_evicted_total",
	Help:      "Number of samples evicted from queue; queue is full!",
}, []string{"user_id", "group_key"})

var samplesQueuedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rules_samples_queued_total",
	Help:      "Number of samples queued in total.",
}, []string{"user_id", "group_key"})

var samplesQueued = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "loki",
	Name:      "recording_rules_samples_queued_current",
	Help:      "Number of samples queued to be remote-written.",
}, []string{"user_id", "group_key"})

var samplesQueueCapacity = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "loki",
	Name:      "recording_rules_samples_queue_capacity",
	Help:      "Number of samples that can be queued before eviction of oldest samples occurs.",
}, []string{"user_id", "group_key"})

var remoteWriteErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "loki",
	Name:      "recording_rules_remote_write_errors",
	Help:      "Number of samples that failed to be remote-written due to error.",
}, []string{"user_id", "group_key"})
