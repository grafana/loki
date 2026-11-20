package indexgateway

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	routeChunkRefs = "chunk_refs"
	routeShards    = "shards"

	fallbackUnsupported = "unsupported"
	fallbackOutOfOrder  = "out_of_order"
)

type Metrics struct {
	preFilterChunks        *prometheus.HistogramVec
	postFilterChunks       *prometheus.HistogramVec
	shardPlanningFiles     prometheus.Histogram
	shardPlanningFallbacks *prometheus.CounterVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	metrics := promauto.With(r)
	metrics.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Subsystem: "tsdb_streaming",
		Name:      "pinned_files",
		Help:      "Open file handles shared by ordered streaming TSDB scans.",
	}, func() float64 { return float64(streamenc.PinnedFileCount()) })
	fallbacks := metrics.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "index_gateway",
		Name:      "shard_planning_fallbacks_total",
		Help:      "Number of shard plans that fall back to the full chunk reference lookup.",
	}, []string{"reason"})
	fallbacks.WithLabelValues(fallbackUnsupported)
	fallbacks.WithLabelValues(fallbackOutOfOrder)
	return &Metrics{
		preFilterChunks: metrics.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "prefilter_chunks",
			Help:      "Number of chunks before filtering",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 10),
		}, []string{"route"}),
		postFilterChunks: metrics.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "postfilter_chunks",
			Help:      "Number of chunks after filtering",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 10),
		}, []string{"route"}),
		shardPlanningFiles: metrics.NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Subsystem:                       "index_gateway",
			Name:                            "shard_planning_files",
			Help:                            "Number of index file cursors per ordered shard lookup that opened successfully.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		shardPlanningFallbacks: fallbacks,
	}
}
