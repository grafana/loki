package compactor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Label names for metrics.
const (
	labelTenant  = "tenant"
	labelOutcome = "outcome"
)

// coordinatorMetrics holds every metric emitted from the coordinator's
// per-cycle loop.
type coordinatorMetrics struct {
	unconsolidatedBacklog      *prometheus.GaugeVec // tenant
	oldestBacklogLogAgeSeconds *prometheus.GaugeVec // tenant
	indexesPerTenantWindow     *prometheus.GaugeVec // tenant

	// cyclesTotal counts coordinator cycles by outcome.
	cyclesTotal *prometheus.CounterVec // outcome=toc_not_found|index_load_err|no_indexes|converged|compaction_failed|compacted_with_failures|compacted

	// tenantCyclesTotal counts per-tenant cycle outcomes.
	tenantCyclesTotal *prometheus.CounterVec // outcome=compacted|converged|failed, tenant

	// indexesRemovedTotal counts source indexes removed by compaction
	// (cumulative). Use with indexesAddedTotal to compute net reduction:
	// removed - added = net_reduction.
	indexesRemovedTotal *prometheus.CounterVec // tenant

	// indexesAddedTotal counts new compacted indexes written.
	indexesAddedTotal *prometheus.CounterVec // tenant

	// tasksTotal counts IndexMerge tasks dispatched per tenant (cumulative).
	tasksTotal *prometheus.CounterVec // tenant

	// cycleDurationSeconds measures full-cycle wall-clock duration.
	cycleDurationSeconds prometheus.Histogram

	// tenantCycleDurationSeconds measures per-tenant cycle wall-clock
	// duration. Excludes the converged-skip path.
	tenantCycleDurationSeconds *prometheus.HistogramVec // outcome=compacted|failed
}

func newCoordinatorMetrics(reg prometheus.Registerer) *coordinatorMetrics {
	f := promauto.With(reg)
	return &coordinatorMetrics{
		unconsolidatedBacklog: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_unconsolidated_index_backlog",
			Help: "Per-tenant count of indexes in the current + previous ToC windows whose max_timestamp is older than the consolidation SLO. Steady state: 0.",
		}, []string{labelTenant}),
		oldestBacklogLogAgeSeconds: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_oldest_backlog_log_age_seconds",
			Help: "Per-tenant age (now - min(max_timestamp)) of the oldest unconsolidated index in the current + previous ToC windows. Zero when there is no backlog.",
		}, []string{labelTenant}),
		indexesPerTenantWindow: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_indexes_per_tenant_window",
			Help: "Per-tenant raw index count for the current ToC window. Bounded cardinality (1 series per tenant).",
		}, []string{labelTenant}),

		// Operational counters / histograms.
		cyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_cycles_total",
			Help: "Total coordinator cycles by outcome.",
		}, []string{labelOutcome}),
		tenantCyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_tenant_cycles_total",
			Help: "Per-tenant cycle outcomes. compacted = ran compaction successfully, converged = had <= 1 index (no work), failed = compaction returned error.",
		}, []string{labelOutcome, labelTenant}),
		indexesRemovedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_indexes_removed_total",
			Help: "Cumulative count of source index pointers removed from the ToC by successful tenant cycles.",
		}, []string{labelTenant}),
		indexesAddedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_indexes_added_total",
			Help: "Cumulative count of compacted index pointers written to the ToC by successful tenant cycles.",
		}, []string{labelTenant}),
		tasksTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_tasks_total",
			Help: "Cumulative count of IndexMerge tasks dispatched per tenant.",
		}, []string{labelTenant}),
		cycleDurationSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_cycle_duration_seconds",
			Help:    "Full coordinator-cycle wall-clock duration (load ToC, per-tenant loop, all I/O).",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms .. ~80s
		}),
		tenantCycleDurationSeconds: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_tenant_cycle_duration_seconds",
			Help:    "Per-tenant cycle wall-clock duration. Excludes the converged-skip path.",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 14),
		}, []string{"outcome"}),
	}
}

// observeEntries records metrics regarding the age and number of entries per
// tenant and window.
//   - unconsolidated_index_backlog = max(0, len(entries)-1) — indexes beyond
//     the single converged target that still await consolidation.
//   - oldest_backlog_log_age_seconds = now - min(End) over all entries when a
//     backlog exists (len > 1), else 0.
//   - indexes_per_tenant_window = len(entries).
func (m *coordinatorMetrics) observeEntries(
	tenant string,
	entries []indexEntry,
	now time.Time,
) {
	if m == nil {
		return
	}
	backlog := max(0, len(entries)-1)
	m.unconsolidatedBacklog.WithLabelValues(tenant).Set(float64(backlog))

	var oldestAge float64
	if backlog > 0 {
		oldest := oldestEnd(entries)
		if !oldest.IsZero() {
			oldestAge = now.Sub(oldest).Seconds()
		}
	}
	m.oldestBacklogLogAgeSeconds.WithLabelValues(tenant).Set(oldestAge)

	m.indexesPerTenantWindow.WithLabelValues(tenant).Set(float64(len(entries)))
}

// oldestEnd returns the minimum End timestamp across entries, or the zero
// time when entries is empty.
func oldestEnd(entries []indexEntry) (oldest time.Time) {
	for _, e := range entries {
		if oldest.IsZero() || e.End.Before(oldest) {
			oldest = e.End
		}
	}
	return oldest
}

// observeCycle records the outcome and wall-clock duration of a full
// coordinator cycle.
func (m *coordinatorMetrics) observeCycle(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.cyclesTotal.WithLabelValues(outcome).Inc()
	m.cycleDurationSeconds.Observe(duration.Seconds())
}

// observeTenantCycle records per-tenant cycle outcomes and counts. stats is
// only consulted for the compacted outcome; pass the zero value otherwise.
func (m *coordinatorMetrics) observeTenantCycle(
	tenant string,
	outcome string,
	duration time.Duration,
	stats compactionStats,
) {
	if m == nil {
		return
	}
	// The converged outcome only bumps tenantCyclesTotal
	m.tenantCyclesTotal.WithLabelValues(outcome, tenant).Inc()
	if outcome != "converged" {
		m.tenantCycleDurationSeconds.WithLabelValues(outcome).Observe(duration.Seconds())
	}
	if outcome == "compacted" {
		m.indexesRemovedTotal.WithLabelValues(tenant).Add(float64(stats.removed))
		m.indexesAddedTotal.WithLabelValues(tenant).Add(float64(stats.added))
		m.tasksTotal.WithLabelValues(tenant).Add(float64(stats.dispatched))
	}
}

// workerMetrics holds the worker-side metrics that the IndexMerge executor
// emits.
type workerMetrics struct {
	outputBytesCompressed   *prometheus.HistogramVec // tenant
	outputBytesUncompressed *prometheus.HistogramVec // tenant
}

func newWorkerMetrics(reg prometheus.Registerer) *workerMetrics {
	f := promauto.With(reg)
	return &workerMetrics{
		outputBytesCompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_output_bytes_compressed",
			Help:    "Size of the bytes uploaded to object storage per successful IndexMerge task (post-encoding, what's stored at rest). One observation per task.",
			Buckets: prometheus.ExponentialBuckets(1024, 2, 21),
		}, []string{labelTenant}),
		outputBytesUncompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_output_bytes_uncompressed",
			Help:    "Estimated in-memory size of the IndexMerge builder (postings + stats sections, pre-encoding) at Flush. One observation per task.",
			Buckets: prometheus.ExponentialBuckets(1024, 2, 21),
		}, []string{labelTenant}),
	}
}

// ObserveIndexMergeOutput satisfies executor.IndexMergeObserver. Called by
// the IndexMerge executor on the worker after each successful task.
func (m *workerMetrics) ObserveIndexMergeOutput(tenant string, compressed, uncompressed int64) {
	if m == nil {
		return
	}
	if compressed > 0 {
		m.outputBytesCompressed.WithLabelValues(tenant).Observe(float64(compressed))
	}
	if uncompressed > 0 {
		m.outputBytesUncompressed.WithLabelValues(tenant).Observe(float64(uncompressed))
	}
}
