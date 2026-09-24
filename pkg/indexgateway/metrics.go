package indexgateway

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	routeChunkRefs = "chunk_refs"
	routeShards    = "shards"
)

type Metrics struct {
	preFilterChunks       *prometheus.HistogramVec
	postFilterChunks      *prometheus.HistogramVec
	shardPlanningDuration *prometheus.HistogramVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		shardPlanningDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_index_gateway_shard_planning_duration_seconds",
			Help:    "Wall time per bounded shard planning phase invocation, including failures. Lookup includes waits and scans; concurrent invocations overlap.",
			Buckets: []float64{.001, .01, .1, 1, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"phase"}),
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
	}
}
