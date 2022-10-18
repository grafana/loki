package distributor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
)

type ratestoreMetrics struct {
	rateRefreshFailures *prometheus.CounterVec
	streamCount         prometheus.Gauge
	maxStreamShardCount prometheus.Gauge
	maxStreamRate       prometheus.Gauge
	maxUniqueStreamRate prometheus.Gauge
	refreshDuration     *instrument.HistogramCollector
}

func newRateStoreMetrics(reg prometheus.Registerer) *ratestoreMetrics {
	return &ratestoreMetrics{
		rateRefreshFailures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "rate_store_refresh_failures_total",
			Help:      "The total number of failed attempts to refresh the distributor's view of stream rates",
		}, []string{"source"}),
		streamCount: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_streams",
			Help:      "The number of unique streams reported by all ingesters. Sharded streams are combined",
		}),
		maxStreamShardCount: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_stream_shards",
			Help:      "The number of shards for a single stream reported by ingesters during a sync operation.",
		}),
		maxStreamRate: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_stream_rate_bytes",
			Help:      "The maximum stream rate for any stream reported by ingesters during a sync operation. Sharded Streams are combined.",
		}),
		maxUniqueStreamRate: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_unique_stream_rate_bytes",
			Help:      "The maximum stream rate for any stream reported by ingesters during a sync operation. Sharded Streams are considered separate.",
		}),
		refreshDuration: instrument.NewHistogramCollector(
			promauto.With(reg).NewHistogramVec(
				prometheus.HistogramOpts{
					Namespace: "loki",
					Name:      "rate_store_refresh_duration_seconds",
					Help:      "Time spent refreshing the rate store",
					Buckets:   prometheus.DefBuckets,
				}, instrument.HistogramCollectorBuckets,
			),
		),
	}
}
