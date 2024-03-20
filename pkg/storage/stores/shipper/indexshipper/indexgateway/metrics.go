package indexgateway

import (
	"github.com/grafana/loki/pkg/util/constants"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	routeChunkRefs = "chunk_refs"
	routeShards    = "shards"
)

type Metrics struct {
	preFilterChunks  *prometheus.HistogramVec
	postFilterChunks *prometheus.HistogramVec
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		preFilterChunks: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "prefilter_chunks",
			Help:      "Number of chunks before filtering",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 10),
		}, []string{"route"}),
		postFilterChunks: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "index_gateway",
			Name:      "postfilter_chunks",
			Help:      "Number of chunks after filtering",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 10),
		}, []string{"route"}),
	}
}
