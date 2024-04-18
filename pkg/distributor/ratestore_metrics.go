package distributor

import (
	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type ratestoreMetrics struct {
	rateRefreshFailures *prometheus.CounterVec
	streamCount         prometheus.Gauge
	expiredCount        prometheus.Counter
	maxStreamShardCount prometheus.Gauge
	streamShardCount    prometheus.Histogram
	maxStreamRate       prometheus.Gauge
	streamRate          prometheus.Histogram
	maxUniqueStreamRate prometheus.Gauge
	refreshDuration     *instrument.HistogramCollector
}

func newRateStoreMetrics(reg prometheus.Registerer) *ratestoreMetrics {
	return &ratestoreMetrics{
		rateRefreshFailures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_refresh_failures_total",
			Help:      "The total number of failed attempts to refresh the distributor's view of stream rates",
		}, []string{"source"}),
		streamCount: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_streams",
			Help:      "The number of unique streams reported by all ingesters. Sharded streams are combined",
		}),
		expiredCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_expired_streams_total",
			Help:      "The number of streams that have been expired by the ratestore",
		}),
		maxStreamShardCount: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_max_stream_shards",
			Help:      "The number of shards for a single stream reported by ingesters during a sync operation.",
		}),
		streamShardCount: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_stream_shards",
			Help:      "The distribution of number of shards for a single stream reported by ingesters during a sync operation.",
			Buckets:   []float64{0, 1, 2, 4, 8, 16, 32, 64, 128},
		}),
		maxStreamRate: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_max_stream_rate_bytes",
			Help:      "The maximum stream rate for any stream reported by ingesters during a sync operation. Sharded Streams are combined.",
		}),
		streamRate: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_stream_rate_bytes",
			Help:      "The distribution of stream rates for any stream reported by ingesters during a sync operation. Sharded Streams are combined.",
			Buckets:   prometheus.ExponentialBuckets(20000, 2, 14), // biggest bucket is 20000*2^(14-1) = 163,840,000 (~163.84MB)
		}),
		maxUniqueStreamRate: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "rate_store_max_unique_stream_rate_bytes",
			Help:      "The maximum stream rate for any stream reported by ingesters during a sync operation. Sharded Streams are considered separate.",
		}),
		refreshDuration: instrument.NewHistogramCollector(
			promauto.With(reg).NewHistogramVec(
				prometheus.HistogramOpts{
					Namespace: constants.Loki,
					Name:      "rate_store_refresh_duration_seconds",
					Help:      "Time spent refreshing the rate store",
					Buckets:   prometheus.DefBuckets,
				}, instrument.HistogramCollectorBuckets,
			),
		),
	}
}
