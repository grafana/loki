package logql

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// expression type used in metrics
const (
	StreamsKey = "streams"
	MetricsKey = "metrics"
)

// parsing evaluation result used in metrics
const (
	SuccessKey = "success"
	FailureKey = "failure"
	NoopKey    = "noop"
)

// MapperMetrics is the metrics wrapper used in logql mapping (shard and range)
type MapperMetrics struct {
	DownstreamQueries *prometheus.CounterVec // downstream queries total, partitioned by streams/metrics
	ParsedQueries     *prometheus.CounterVec // parsed ASTs total, partitioned by success/failure/noop
	DownstreamFactor  prometheus.Histogram   // per request downstream factor
}

func newMapperMetrics(registerer prometheus.Registerer, mapper string) *MapperMetrics {
	return &MapperMetrics{
		DownstreamQueries: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "query_frontend_shards_total",
			Help:        "Number of downstream queries by expression type",
			ConstLabels: prometheus.Labels{"mapper": mapper},
		}, []string{"type"}),
		ParsedQueries: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "query_frontend_sharding_parsed_queries_total",
			Help:        "Number of parsed queries by evaluation type",
			ConstLabels: prometheus.Labels{"mapper": mapper},
		}, []string{"type"}),
		DownstreamFactor: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "query_frontend_shard_factor",
			Help:      "Number of downstream queries per request",
			// 1 -> 65k shards
			Buckets:     prometheus.ExponentialBuckets(1, 4, 8),
			ConstLabels: prometheus.Labels{"mapper": mapper},
		}),
	}
}

// downstreamRecorder wraps a vector & histogram, providing an easy way to increment downstream counts.
// and unify them into histogram entries.
// NOT SAFE FOR CONCURRENT USE! We avoid introducing mutex locking here
// because AST mapping is single threaded.
type downstreamRecorder struct {
	done  bool
	total int
	*MapperMetrics
}

// downstreamRecorder constructs a recorder using the underlying metrics.
func (m *MapperMetrics) downstreamRecorder() *downstreamRecorder {
	return &downstreamRecorder{
		MapperMetrics: m,
	}
}

// Add increments both the downstream count and tracks it for the eventual histogram entry.
func (r *downstreamRecorder) Add(x int, key string) {
	r.total += x
	r.DownstreamQueries.WithLabelValues(key).Add(float64(x))
}

// Finish idemptotently records a histogram entry with the total downstream factor.
func (r *downstreamRecorder) Finish() {
	if !r.done {
		r.done = true
		r.DownstreamFactor.Observe(float64(r.total))
	}
}
