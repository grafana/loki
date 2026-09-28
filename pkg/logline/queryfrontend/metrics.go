package queryfrontend

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the logline query frontend.
type Metrics struct {
	// hintProviderDuration measures time spent in ProvideHints per query.
	hintProviderDuration prometheus.Histogram
	// hintRangesReturned measures the number of time ranges returned per query.
	hintRangesReturned prometheus.Histogram
	// hintPassthrough counts queries bypassed without hint narrowing.
	// Labels: reason — "timeout", "unsupported", "error".
	hintPassthrough *prometheus.CounterVec
	// hintSubRequests counts sub-requests dispatched for hint-narrowed queries.
	// Labels: status — "narrowed", "skipped".
	hintSubRequests *prometheus.CounterVec
	// passthroughSubRequests counts sub-requests dispatched for ingester-window
	// time ranges that bypass the logline index entirely.
	// Labels: reason — "ingester_window".
	passthroughSubRequests *prometheus.CounterVec
	// queryTermBatchesProcessed records QueryMultiple term-batch depth per index query.
	// Labels: reason — "term_miss", "empty_and", "positive".
	queryTermBatchesProcessed *prometheus.HistogramVec
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
			Help: "Total sub-requests dispatched for ingester-window time ranges",
		}, []string{"reason"}),
		queryTermBatchesProcessed: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "logline_query_frontend_query_term_batches_processed",
			Help:    "Term batches processed by QueryMultiple per index query",
			Buckets: []float64{1, 2, 3, 4, 5, 8, 10, 15, 20},
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

func (m *Metrics) ObserveQueryMultipleTermBatches(reason string, termBatchesProcessed int) {
	if m == nil || termBatchesProcessed <= 0 {
		return
	}
	m.queryTermBatchesProcessed.WithLabelValues(reason).Observe(float64(termBatchesProcessed))
}
