package querytee

import (
	"math"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v4/pkg/logql/model/stats"
)

const (
	comparisonMatch    = "match"
	comparisonMismatch = "mismatch"
	comparisonFailed   = "fail"
	comparisonSkipped  = "skipped"

	unknownIssuer = "unknown"
	canaryIssuer  = "loki-canary"

	// unknownTenant is the tenant label of a request whose tenants cannot be read.
	unknownTenant = "unknown"

	// idleQueueTime is the longest queue time that still counts as an idle backend.
	idleQueueTime = 100 * time.Millisecond
)

// causeCompareError is the cause of a failed comparison when the comparator fails. The other causes
// say how the bodies differ, or why a backend failed.
const causeCompareError = "compare_error"

// statBucket is a range of values of a stat. It holds the values above the upper bound of the
// previous bucket, up to and including its own upper bound.
type statBucket struct {
	upperBound float64
	label      string
}

// bytesProcessedBuckets are the buckets for the bytes that the primary backend processed. The sizes
// are decimal: 1 GB is 10^9 bytes.
var bytesProcessedBuckets = []statBucket{
	{upperBound: humanize.GByte, label: "less_than_1gb"},
	{upperBound: 10 * humanize.GByte, label: "1gb_to_10gb"},
	{upperBound: 100 * humanize.GByte, label: "10gb_to_100gb"},
	{upperBound: humanize.TByte, label: "100gb_to_1tb"},
	{upperBound: math.Inf(1), label: "more_than_1tb"},
}

// execTimeBuckets are the buckets for the execution time of the primary backend, in seconds.
var execTimeBuckets = []statBucket{
	{upperBound: 0.1, label: "less_than_100ms"},
	{upperBound: 0.5, label: "100ms_to_500ms"},
	{upperBound: 1, label: "500ms_to_1s"},
	{upperBound: 5, label: "1s_to_5s"},
	{upperBound: 10, label: "5s_to_10s"},
	{upperBound: 30, label: "10s_to_30s"},
	{upperBound: 60, label: "30s_to_60s"},
	{upperBound: math.Inf(1), label: "more_than_60s"},
}

// bucketLabel returns the label of the first bucket that holds value. The buckets must be in
// ascending order of upper bound.
func bucketLabel(buckets []statBucket, value float64) string {
	for _, bucket := range buckets {
		if value <= bucket.upperBound {
			return bucket.label
		}
	}
	return buckets[len(buckets)-1].label
}

type ProxyMetrics struct {
	requestsTotal              *prometheus.CounterVec
	requestDuration            *prometheus.HistogramVec
	responsesTotal             *prometheus.CounterVec
	responsesComparedTotal     *prometheus.CounterVec
	missingMetrics             *prometheus.HistogramVec
	secondaryBackendDecisions  *prometheus.CounterVec
	secondaryBackendStatsRatio *prometheus.HistogramVec
}

func NewProxyMetrics(registerer prometheus.Registerer) *ProxyMetrics {
	m := &ProxyMetrics{
		requestsTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_requests_total",
			Help: "Total number of HTTP requests received by query-tee.",
		}, []string{"method", "route"}),
		requestDuration: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_querytee_request_duration_seconds",
			Help:                            "Time (in seconds) spent serving HTTP requests.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"backend", "backend_alias", "method", "route", "status_code", "issuer"}),
		responsesTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_responses_total",
			Help: "Total number of responses sent back to the client by the selected backend.",
		}, []string{"backend", "backend_alias", "method", "route", "issuer"}),
		responsesComparedTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_responses_compared_total",
			Help: "Total number of responses compared per route and backend name by result and cause.",
		}, []string{"backend", "backend_alias", "route", "result", "cause", "issuer", "tenant"}),
		missingMetrics: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_querytee_missing_metrics_series",
			Help:                            "Number of missing metrics (series) in a vector response.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"backend", "backend_alias", "route", "status_code", "issuer"}),
		secondaryBackendDecisions: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_secondary_backend_decisions_total",
			Help: "Total number of requests by the decision to send them to the secondary backend, or the reason not to.",
		}, []string{"route", "decision"}),
		secondaryBackendStatsRatio: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_querytee_secondary_backend_stats_ratio",
			Help:                            "Ratio of a query stat of the secondary backend over the same stat of the primary backend, by the bucket of the primary value.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"route", "stat", "primary_bucket", "queriers_load_status", "result_cache_status"}),
	}

	return m
}

// observeSecondaryBackendStats observes the ratio of each stat of the secondary over the same stat
// of the primary. It labels the ratio with the bucket of the primary value, and it skips a stat when
// the primary value is zero.
func (m *ProxyMetrics) observeSecondaryBackendStats(route string, primary, secondary stats.Result) {
	// comparisonStatus returns the label value for the status of the two backends. The arguments
	// tell whether the primary and the secondary are in the state named by yes. The value is
	// both_<no> when neither is, primary_<yes> or secondary_<yes> when one is, and both_<yes> when
	// both are.
	comparisonStatus := func(primaryYes, secondaryYes bool, no, yes string) string {
		switch {
		case primaryYes && secondaryYes:
			return "both_" + yes
		case primaryYes:
			return "primary_" + yes
		case secondaryYes:
			return "secondary_" + yes
		default:
			return "both_" + no
		}
	}

	queriersLoadStatus := comparisonStatus(
		primary.Summary.QueueTime > idleQueueTime.Seconds(),
		secondary.Summary.QueueTime > idleQueueTime.Seconds(),
		"idle", "busy",
	)
	primaryEntriesFound, _ := resultCacheEntries(primary.Caches)
	secondaryEntriesFound, _ := resultCacheEntries(secondary.Caches)
	resultCacheStatus := comparisonStatus(primaryEntriesFound > 0, secondaryEntriesFound > 0, "miss", "hit")

	for _, stat := range []struct {
		name               string
		buckets            []statBucket
		primary, secondary float64
	}{
		{"bytes_processed", bytesProcessedBuckets, float64(primary.Summary.TotalBytesProcessed), float64(secondary.Summary.TotalBytesProcessed)},
		{"exec_time", execTimeBuckets, primary.Summary.ExecTime, secondary.Summary.ExecTime},
	} {
		if stat.primary == 0 {
			continue
		}
		m.secondaryBackendStatsRatio.WithLabelValues(
			route, stat.name, bucketLabel(stat.buckets, stat.primary), queriersLoadStatus, resultCacheStatus,
		).Observe(stat.secondary / stat.primary)
	}
}

// resultCacheEntries returns the entries that the query results caches of the query-frontend found
// and requested.
func resultCacheEntries(caches stats.Caches) (found, requested int32) {
	// The query-frontend keeps one results cache for each query type: range query, instant metric
	// and log. A query uses one of them, so the sum is the count of that cache. The other caches in
	// stats.Caches serve other types of request, so they do not count.
	for _, cache := range []stats.Cache{caches.Result, caches.InstantMetricResult, caches.LogResult} {
		found += cache.EntriesFound
		requested += cache.EntriesRequested
	}
	return found, requested
}
