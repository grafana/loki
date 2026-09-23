package distributor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type metrics struct {
	// distributor metrics
	ingesterAppends                       *prometheus.CounterVec
	ingesterAppendTimeouts                *prometheus.CounterVec
	replicationFactor                     prometheus.Gauge
	streamShardCount                      prometheus.Counter
	zeroStreamCount                       *prometheus.CounterVec
	pushStatsCount                        *prometheus.CounterVec
	tenantPushSanitizedStructuredMetadata *prometheus.CounterVec

	// metrics for shard shadowing
	// so we can compare rateStore sharding with limit-service sharding
	limitsServiceShardShadowDivergence          *prometheus.CounterVec
	limitsServiceShardShadowDivergenceMagnitude *prometheus.HistogramVec
	limitsServiceShardShadowStreamRate          *prometheus.HistogramVec
	limitsServiceShardShadowFailed              *prometheus.CounterVec
	limitsServiceShardShadowRejected            *prometheus.CounterVec
	limitsServiceShardShadowCompared            *prometheus.CounterVec
	limitsServiceShardShadowCapped              *prometheus.CounterVec
	limitsServiceShardDuration                  prometheus.Histogram
	limitsServiceExceedsLimitsDuration          prometheus.Histogram

	// kafka metrics
	kafkaAppends           *prometheus.CounterVec
	kafkaWriteBytesTotal   prometheus.Counter
	kafkaWriteLatency      prometheus.Histogram
	kafkaRecordsPerRequest prometheus.Histogram

	// Track the max inflight bytes in the last 1 minute.
	maxInflightBytes           prometheus.Gauge
	inflightBytesHighWatermark prometheus.Summary
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		ingesterAppends: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_ingester_appends_total",
			Help:      "The total number of batch appends sent to ingesters.",
		}, []string{"ingester"}),
		ingesterAppendTimeouts: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_ingester_append_timeouts_total",
			Help:      "The total number of failed batch appends sent to ingesters due to timeouts.",
		}, []string{"ingester"}),
		replicationFactor: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "distributor_replication_factor",
			Help:      "The configured replication factor.",
		}),
		streamShardCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "stream_sharding_count",
			Help:      "Total number of times the distributor has sharded streams",
		}),
		zeroStreamCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_zero_streams_count",
			Help:      "Total number of push requests with 0 streams",
		}, []string{"tenant", "stage"}),
		pushStatsCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_stats_count",
			Help:      "Total number of successfully parsed push requests aggregated by tenant, content-type, encoding, version, format",
		}, []string{"tenant", "content_type", "content_encoding", "content_version", "format"}),
		tenantPushSanitizedStructuredMetadata: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_push_structured_metadata_sanitized_total",
			Help:      "The total number of times we've had to sanitize structured metadata (names or values) at ingestion time per tenant.",
		}, []string{"tenant", "format"}),

		limitsServiceShardShadowDivergence: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_divergence_total",
			Help:      "For tenants in shadow mode, the total number of times the ingest-limits service's shard count differed from the shard count actually used, which the local rate store decided. Only counted for comparable observations; see distributor_limits_service_shard_shadow_compared_total for the denominator. The sharding label says which side sharded the stream, meaning a shard count above one: both, limits_only, rate_store_only, or neither.",
		}, []string{"tenant", "sharding"}),

		limitsServiceShardShadowDivergenceMagnitude: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_divergence_magnitude",
			Help:      "For tenants in shadow mode, the distribution of the absolute gap between the ingest-limits service's shard count and the local rate store's, observed only when the two differ. The direction label splits it into over, the limits service asking for more shards, and under, fewer, so each is a clean distribution of positive magnitudes. The total count across both directions equals distributor_limits_service_shard_shadow_divergence_total.",
			// Native only, as the exponential schema covers the whole range
			// without hand-picked buckets.
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
		}, []string{"tenant", "direction"}),

		limitsServiceShardShadowStreamRate: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_stream_rate_bytes",
			Help:      "For tenants in shadow mode, the distribution of the per-stream byte rate that drove the shard decision, observed once per comparable stream. The source label splits it into rate_store, the distributor's local rate store, and limits, the ingest-limits service, so the two can be compared as heatmaps. Both are the sustained rate, before this push is amortized on top.",
			// Native only, as the byte rates span kilobytes to megabytes per
			// second.
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
		}, []string{"tenant", "source"}),

		limitsServiceShardShadowFailed: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_failed_total",
			Help:      "For tenants in shadow mode, the total number of observations that could not be compared because the ingest-limits service did not answer for the stream, reported that it could not check it, or answered from an instance that does not own the stream's partition.",
		}, []string{"tenant"}),

		limitsServiceShardShadowRejected: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_rejected_total",
			Help:      "For tenants in shadow mode, the total number of streams the ingest-limits service would have rejected, because a brand-new stream exhausted the tenant's stream count budget. The local rate store never rejects, so this is a difference in kind rather than in shard count.",
		}, []string{"tenant"}),

		limitsServiceShardShadowCompared: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_compared_total",
			Help:      "For tenants in shadow mode, the total number of observations with a comparable shard count from the ingest-limits service. The denominator for distributor_limits_service_shard_shadow_divergence_total. The sharding label says which side sharded the stream, meaning a shard count above one: both, limits_only, rate_store_only, or neither. Excluding neither restricts the divergence rate to comparisons where sharding was in play.",
		}, []string{"tenant", "sharding"}),

		limitsServiceShardShadowCapped: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_limits_service_shard_shadow_capped_total",
			Help:      "For tenants in shadow mode, the total number of comparable observations where the ingest-limits service capped the shard count below what the rate justified, to fit the tenant's remaining stream count budget. Capping is expected, and is not by itself a disagreement about the rate.",
		}, []string{"tenant"}),

		limitsServiceShardDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "distributor_limits_service_shard_duration_seconds",
			Help:                            "The time the distributor spends in the synchronous CheckLimitsAndShard call on the push path, which is the latency shadow mode adds. Bounded by the call timeout.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),

		limitsServiceExceedsLimitsDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "distributor_limits_service_exceeds_limits_duration_seconds",
			Help:                            "The time the distributor spends in the ExceedsLimits call on the push path. Reported alongside loki_distributor_limits_service_shard_duration_seconds so the two limits service calls can be compared: once CheckLimitsAndShard subsumes ExceedsLimits, the added latency is the difference between the two.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),

		kafkaAppends: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_kafka_appends_total",
			Help:      "The total number of appends sent to kafka ingest path.",
		}, []string{"partition", "status"}),
		kafkaWriteLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "distributor_kafka_latency_seconds",
			Help:                            "Latency to write an incoming request to the ingest storage.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),
		kafkaWriteBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "distributor_kafka_sent_bytes_total",
			Help:      "Total number of bytes sent to the ingest storage.",
		}),
		kafkaRecordsPerRequest: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "distributor_kafka_records_per_write_request",
			Help:      "The number of records a single per-partition write request has been split into.",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 8),
		}),

		maxInflightBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_distributor_max_inflight_bytes",
			Help: "The max permitted inflight bytes. 0 if disabled.",
		}),
		inflightBytesHighWatermark: promauto.With(reg).NewSummary(prometheus.SummaryOpts{
			Name:       "loki_distributor_inflight_bytes_high_watermark",
			Help:       "The most observed inflight bytes in the last 1 minute.",
			Objectives: map[float64]float64{1.0: 0.1},
			MaxAge:     time.Minute,
		}),
	}
}
