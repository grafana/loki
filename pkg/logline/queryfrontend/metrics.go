package queryfrontend

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the logline query frontend.
type Metrics struct {
	// hintProviderDuration measures time spent in ProvideHints per query.
	// Lookups canceled before they finish are counted in hintLookupsCanceled
	// instead, so their truncated durations don't skew it.
	hintProviderDuration prometheus.Histogram
	// hintLookupsCanceled counts live hint lookups canceled before they
	// finished, usually because the query was answered first.
	hintLookupsCanceled prometheus.Counter
	// hintWaitDuration measures, once per live query that reaches the hint lookup,
	// the wall time during which at least one sub-request was held at the filter
	// waiting for hints.
	// Labels: status — the logline hintStatus when the query returns.
	hintWaitDuration *prometheus.HistogramVec
	// hintRangesReturned measures the number of time ranges returned per query.
	hintRangesReturned prometheus.Histogram
	// hintPassthrough counts sub-requests that waited for the hint lookup and
	// then passed through without hint narrowing. Ingester-window and
	// pre-min-date sub-requests don't wait, so they are counted in
	// passthroughSubRequests whatever the lookup returns.
	// Labels: reason — "timeout", "unsupported", "error".
	hintPassthrough *prometheus.CounterVec
	// hintSubRequests counts sub-requests dispatched for hint-narrowed queries.
	// Labels: status — "narrowed", "skipped".
	hintSubRequests *prometheus.CounterVec
	// passthroughSubRequests counts sub-requests dispatched for time ranges the
	// logline index doesn't cover, without waiting for the hint lookup.
	// Labels: reason — "ingester_window", "pre_min_date".
	passthroughSubRequests *prometheus.CounterVec
	// hintSkippedSmallQuery counts requests that skip hint prefetch because
	// query index-stats bytes are below the configured threshold.
	hintSkippedSmallQuery prometheus.Counter
	// dryRunTotal counts dry-run requests that pass initial eligibility gates.
	dryRunTotal prometheus.Counter
	// dryRunSkippedInflight counts dry-run requests skipped because only one
	// dry-run lookup is allowed in flight at a time.
	dryRunSkippedInflight prometheus.Counter
	// dryRunIncomplete counts dry-run requests where query execution completed
	// before hint lookup finished, forcing hint cancellation.
	dryRunIncomplete prometheus.Counter
	// dryRunVerified counts dry-run verification outcomes.
	// Labels: result — "correct", "false_negative".
	dryRunVerified *prometheus.CounterVec
	// shardPlanningTotal counts shard-planning cancel-then-rerun decisions and outcomes.
	// Labels: result — "provisional_query", "cancel_then_rerun", "fallback"; reason is low cardinality.
	shardPlanningTotal *prometheus.CounterVec
}

// NewMetrics creates a new Metrics instance registered with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		hintProviderDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_query_frontend_hint_provider_duration_seconds",
			Help:    "Time spent in ProvideHints per query in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		hintLookupsCanceled: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_query_frontend_hint_lookups_canceled_total",
			Help: "Total live hint lookups canceled before they finished, usually because the query was answered first",
		}),
		hintWaitDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "logline_query_frontend_hint_wait_seconds",
			Help: "Wall time per live query during which at least one sub-request waited for logline hints, in seconds",
			// The classic buckets extend past the default 15s hint timeout, which
			// a query pays more than once when its sub-requests wait in waves.
			Buckets:                         []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30, 60},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"status"}),
		hintRangesReturned: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_query_frontend_hint_ranges_returned",
			Help:    "Number of time ranges returned by ProvideHints per query",
			Buckets: []float64{0, 1, 2, 5, 10, 20, 50, 100, 200, 500},
		}),
		hintPassthrough: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_query_frontend_hint_passthrough_total",
			Help: "Total queries passed through without hint narrowing",
		}, []string{"reason"}),
		hintSubRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_query_frontend_hint_sub_requests_total",
			Help: "Total hint-narrowed sub-requests dispatched",
		}, []string{"status"}),
		passthroughSubRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_query_frontend_passthrough_sub_requests_total",
			Help: "Total sub-requests dispatched without hints for ingester-window or pre-min-date time ranges",
		}, []string{"reason"}),
		hintSkippedSmallQuery: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_query_frontend_hint_skipped_small_query_total",
			Help: "Total requests that skipped logline hint prefetch because query bytes were below threshold",
		}),
		dryRunTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_query_frontend_dry_run_total",
			Help: "Total dry-run requests that executed hint lookup verification",
		}),
		dryRunSkippedInflight: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_query_frontend_dry_run_skipped_inflight_total",
			Help: "Total dry-run requests skipped because another dry-run hint lookup was in flight",
		}),
		dryRunIncomplete: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_query_frontend_dry_run_incomplete_total",
			Help: "Total dry-run requests where hint lookup did not finish before query completion",
		}),
		dryRunVerified: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_query_frontend_dry_run_verified_total",
			Help: "Total dry-run verification outcomes",
		}, []string{"result"}),
		shardPlanningTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_query_frontend_shard_planning_total",
			Help: "Total shard-planning cancel-then-rerun decisions and outcomes",
		}, []string{"result", "reason"}),
	}
}
