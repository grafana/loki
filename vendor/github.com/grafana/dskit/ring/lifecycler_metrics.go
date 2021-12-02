package ring

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type LifecyclerMetrics struct {
	consulHeartbeats prometheus.Counter
	tokensOwned      prometheus.Gauge
	tokensToOwn      prometheus.Gauge
	shutdownDuration *prometheus.HistogramVec
}

func NewLifecyclerMetrics(ringName string, reg prometheus.Registerer) *LifecyclerMetrics {
	return &LifecyclerMetrics{
		consulHeartbeats: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        "member_consul_heartbeats_total",
			Help:        "The total number of heartbeats sent to consul.",
			ConstLabels: prometheus.Labels{"name": ringName},
		}),
		tokensOwned: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name:        "member_ring_tokens_owned",
			Help:        "The number of tokens owned in the ring.",
			ConstLabels: prometheus.Labels{"name": ringName},
		}),
		tokensToOwn: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name:        "member_ring_tokens_to_own",
			Help:        "The number of tokens to own in the ring.",
			ConstLabels: prometheus.Labels{"name": ringName},
		}),
		shutdownDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:        "shutdown_duration_seconds",
			Help:        "Duration (in seconds) of shutdown procedure (ie transfer or flush).",
			Buckets:     prometheus.ExponentialBuckets(10, 2, 8), // Biggest bucket is 10*2^(9-1) = 2560, or 42 mins.
			ConstLabels: prometheus.Labels{"name": ringName},
		}, []string{"op", "status"}),
	}

}
