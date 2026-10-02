package indexgateway

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/timing"
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
	lookupWork            *prometheus.HistogramVec
	lookupScanDuration    *prometheus.HistogramVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		lookupWork: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_index_gateway_shard_planning_lookup_work",
			Help:                            "Work per bounded lookup: table visits, file scans, refs_before_dedup from file scans, and refs_returned after per-store deduplication. Errors include partial work; refs_returned is zero on error.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"kind", "outcome"}),
		lookupScanDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_index_gateway_shard_planning_lookup_scan_duration_seconds",
			Help:                            "Sum and maximum of file scan wall times per bounded lookup, including partial work on errors. Concurrent scans overlap; sum is not lookup wall time. Zero when no scans complete.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"stat", "outcome"}),
		shardPlanningDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_index_gateway_shard_planning_duration_seconds",
			Help:                            "Wall time per bounded shard planning phase invocation, including failures. Lookup includes waits and scans; concurrent invocations overlap.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
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

// observeLookup emits one observation per summary and lookup, including failures.
func (m *Metrics) observeLookup(stats timing.Stats, returned int, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
		returned = 0
	}
	for kind, value := range map[string]int64{
		"tables": stats.Tables, "files": stats.Files,
		"refs_before_dedup": stats.Refs, "refs_returned": int64(returned),
	} {
		m.lookupWork.WithLabelValues(kind, outcome).Observe(float64(value))
	}
	m.lookupScanDuration.WithLabelValues("sum", outcome).Observe(stats.ScanTotal.Seconds())
	m.lookupScanDuration.WithLabelValues("max", outcome).Observe(stats.ScanMax.Seconds())
}
