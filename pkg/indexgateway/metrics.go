package indexgateway

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	routeChunkRefs = "chunk_refs"
	routeShards    = "shards"
)

// Values of the operation label on indexRequestDuration.
const (
	opChunkRefs   = "chunk_refs"
	opSeries      = "series"
	opLabelNames  = "label_names"
	opLabelValues = "label_values"
	opStats       = "stats"
	opVolume      = "volume"
	opShards      = "shards"
)

type Metrics struct {
	preFilterChunks  *prometheus.HistogramVec
	postFilterChunks *prometheus.HistogramVec

	indexRequestDuration *prometheus.HistogramVec
	indexFileAccesses    *prometheus.CounterVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		preFilterChunks: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "prefilter_chunks",
			Help:      "Number of chunks before filtering",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 10),
		}, []string{"route"}),
		postFilterChunks: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "postfilter_chunks",
			Help:      "Number of chunks after filtering",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 10),
		}, []string{"route"}),
		indexRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Subsystem:                       "index_gateway",
			Name:                            "index_request_duration_seconds",
			Help:                            "Time taken to serve successful index requests, after admission by the query gate, by operation and the slowest index tier touched (memory, disk, on_demand, or none).",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"operation", "tier"}),
		indexFileAccesses: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: "tsdb_shipper",
			Name:      "index_file_accesses_total",
			Help:      "Number of times index gateway requests read a downloaded TSDB index file, by the tier it was served from (memory or disk).",
		}, []string{"tier"}),
	}
}
