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
			Name:      "rate_store_stream_count",
			Help:      "The last seen number of streams",
		}),
		maxStreamShardCount: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_stream_shard_count",
			Help:      "The largest number of shards seen for any stream",
		}),
		maxStreamRate: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "rate_store_max_stream_rate_bytes",
			Help:      "The maximum stream rate",
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
