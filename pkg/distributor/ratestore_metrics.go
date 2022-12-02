package distributor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
)

type ratestoreMetrics struct {
	rateRefreshFailures *prometheus.CounterVec
	expiredCount        prometheus.Counter
	streamCount         prometheus.Gauge
	maxStreamShardCount *prometheus.GaugeVec
	maxStreamRate       *prometheus.GaugeVec
	maxUniqueStreamRate *prometheus.GaugeVec
	streamShardCount    prometheus.Histogram
	streamRate          prometheus.Histogram
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
		expiredCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "rate_store_expired_streams_total",
			Help:      "The number of streams that have been expired by the ratestore",
		}),
		maxStreamShardCount: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_stream_shards",
			Help:      "The number of shards for a single stream reported by ingesters during a sync operation.",
		}, []string{"tenant"}),
		streamShardCount: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "rate_store_stream_shards",
			Help:      "The distribution of number of shards for a single stream reported by ingesters during a sync operation.",
			Buckets:   []float64{0, 2, 4, 8, 16, 32, 64, 128},
		}),
		maxStreamRate: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_stream_rate_bytes",
			Help:      "The maximum stream rate for any stream reported by ingesters during a sync operation. Sharded Streams are combined.",
		}, []string{"tenant"}),
		streamRate: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "rate_store_stream_rate_bytes",
			Help:      "The distribution of stream rates for any stream reported by ingesters during a sync operation. Sharded Streams are combined.",
			Buckets:   prometheus.ExponentialBuckets(20000, 2, 14), // biggest bucket is 20000*2^(14-1) = 163,840,000 (~163.84MB)
		}),
		maxUniqueStreamRate: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_unique_stream_rate_bytes",
			Help:      "The maximum stream rate for any stream reported by ingesters during a sync operation. Sharded Streams are considered separate.",
		}, []string{"tenant"}),
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
