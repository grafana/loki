package compactor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metricsNamespace prefixes every coordinator-side metric.
const metricsNamespace = "loki_dataobj_compaction"

// Label names for metrics.
const (
	labelTenant     = "tenant"
	labelOutcome    = "outcome"
	labelWindowRole = "window_role"
)

// coordinatorMetrics holds every metric emitted from the coordinator's
// per-cycle loop. Construction goes through newCoordinatorMetrics; a nil
// Registerer is supported (promauto.With(nil) creates metrics that are
// never registered — see promauto/auto.go's `if f.r != nil` guard).
type coordinatorMetrics struct {
	// SLO gauges. Scope: current + previous ToC windows only.
	unconsolidatedBacklog      *prometheus.GaugeVec // tenant
	oldestBacklogLogAgeSeconds *prometheus.GaugeVec // tenant
	prePreviousUnconsolidated  *prometheus.GaugeVec // tenant
	indexesPerTenantWindow     *prometheus.GaugeVec // tenant, window_role

	// Orphan-rate proxy. Incremented next to the inline ReplaceIndexPointers
	// call when it returns metastore.ErrReplaceRetriesExhausted.
	tocSwapRetryExhaustedTotal *prometheus.CounterVec // tenant

	// ── Operational counters / histograms ─────────────────────────────
	// These describe coordinator throughput and latency rather than the
	// SLO itself. They drive the operational rows of the
	// dataobj-compaction-overview dashboard.

	// cyclesTotal counts coordinator cycles by outcome.
	cyclesTotal *prometheus.CounterVec // outcome=ok|aborted

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
			Namespace: metricsNamespace,
			Name:      "unconsolidated_index_backlog",
			Help:      "Per-tenant count of indexes in the current + previous ToC windows whose max_timestamp is older than the consolidation SLO. Steady state: 0.",
		}, []string{labelTenant}),
		oldestBacklogLogAgeSeconds: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "oldest_backlog_log_age_seconds",
			Help:      "Per-tenant age (now - min(max_timestamp)) of the oldest unconsolidated index in the current + previous ToC windows. Zero when there is no backlog.",
		}, []string{labelTenant}),
		prePreviousUnconsolidated: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "pre_previous_unconsolidated_windows",
			Help:      "Per-tenant count of ToC windows older than previous_window that still have more than one index. Steady state: 0. Hard alert: > 0. Currently emits 0 until older-window scan is implemented.",
		}, []string{labelTenant}),
		indexesPerTenantWindow: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "indexes_per_tenant_window",
			Help:      "Per-tenant raw index count for the current and previous ToC windows. Bounded cardinality (2 series per tenant).",
		}, []string{labelTenant, labelWindowRole}),
		tocSwapRetryExhaustedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "toc_swap_retry_exhausted_total",
			Help:      "Per-tenant count of ReplaceIndexPointers invocations that exhausted the retry budget. Orphan-rate proxy.",
		}, []string{labelTenant}),

		// Operational counters / histograms.
		cyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "cycles_total",
			Help:      "Total coordinator cycles by outcome. ok = cycle completed (any per-tenant result), aborted = cycle bailed before per-tenant loop.",
		}, []string{"outcome"}),
		tenantCyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "tenant_cycles_total",
			Help:      "Per-tenant cycle outcomes. compacted = ran compaction successfully, converged = had <= 1 index (no work), failed = compaction returned error.",
		}, []string{labelOutcome, labelTenant}),
		indexesRemovedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "indexes_removed_total",
			Help:      "Cumulative count of source index pointers removed from the ToC by successful tenant cycles.",
		}, []string{labelTenant}),
		indexesAddedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "indexes_added_total",
			Help:      "Cumulative count of compacted index pointers written to the ToC by successful tenant cycles.",
		}, []string{labelTenant}),
		tasksTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "tasks_total",
			Help:      "Cumulative count of IndexMerge tasks dispatched per tenant.",
		}, []string{labelTenant}),
		cycleDurationSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "cycle_duration_seconds",
			Help:      "Full coordinator-cycle wall-clock duration (load ToC, per-tenant loop, all I/O).",
			Buckets:   prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms .. ~80s
		}),
		tenantCycleDurationSeconds: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "tenant_cycle_duration_seconds",
			Help:      "Per-tenant cycle wall-clock duration. Excludes the converged-skip path.",
			Buckets:   prometheus.ExponentialBuckets(0.01, 2, 14),
		}, []string{"outcome"}),
	}
}

// emitSLO updates the SLO gauges for a single tenant given the tenant's
// per-window entries, the wall clock, and the configured SLO duration.
//
// currentEntries and previousEntries are the entries from the current and
// previous 12h ToC windows respectively. Either may be nil/empty. Window
// classification is determined by data source (which ToC file the entry
// came from), not by deriving the window from the entry's End timestamp.
//
// The pre_previous gauge is set to 0 in v1.0 — populating it requires
// walking older ToCs every cycle and is deferred. The series is still
// emitted (via WithLabelValues) so dashboards have a stable shape.
func (m *coordinatorMetrics) emitSLO(
	tenant string,
	currentEntries, previousEntries []indexEntry,
	now time.Time,
	slo time.Duration,
) {
	threshold := now.Add(-slo)
	curOld, curOldest := tallyOld(currentEntries, threshold)
	prevOld, prevOldest := tallyOld(previousEntries, threshold)

	backlog := max(0, curOld-1) + max(0, prevOld-1)
	m.unconsolidatedBacklog.WithLabelValues(tenant).Set(float64(backlog))

	var oldestAge float64
	if backlog > 0 {
		// Only windows that contribute >0 to backlog contribute to oldestAge.
		// A window with exactly one old entry holds the converged-target
		// baseline; its End is the covering index's End, not a backlog entry.
		if curOld >= 2 && !curOldest.IsZero() {
			oldestAge = now.Sub(curOldest).Seconds()
		}
		if prevOld >= 2 && !prevOldest.IsZero() {
			if age := now.Sub(prevOldest).Seconds(); age > oldestAge {
				oldestAge = age
			}
		}
	}
	m.oldestBacklogLogAgeSeconds.WithLabelValues(tenant).Set(oldestAge)

	m.prePreviousUnconsolidated.WithLabelValues(tenant).Set(0)

	m.indexesPerTenantWindow.WithLabelValues(tenant, "current").Set(float64(len(currentEntries)))
	m.indexesPerTenantWindow.WithLabelValues(tenant, "previous").Set(float64(len(previousEntries)))
}

// tallyOld counts entries whose End < threshold and tracks the oldest
// (minimum) such End. Uses strict less-than per the SLO definition.
func tallyOld(entries []indexEntry, threshold time.Time) (oldCount int, oldestEnd time.Time) {
	for _, e := range entries {
		if !e.End.Before(threshold) {
			continue
		}
		oldCount++
		if oldestEnd.IsZero() || e.End.Before(oldestEnd) {
			oldestEnd = e.End
		}
	}
	return oldCount, oldestEnd
}

// incRetryExhausted records one retry-budget-exhaustion observation.
// nolint:unused // will be called from coordinator.runTenantCycle
func (m *coordinatorMetrics) incRetryExhausted(tenant string) {
	m.tocSwapRetryExhaustedTotal.WithLabelValues(tenant).Inc()
}

// observeCycle records the outcome and wall-clock duration of a full
// coordinator cycle.
func (m *coordinatorMetrics) observeCycle(outcome string, duration time.Duration) {
	m.cyclesTotal.WithLabelValues(outcome).Inc()
	m.cycleDurationSeconds.Observe(duration.Seconds())
}

// observeTenantCycle records per-tenant cycle outcomes and counts. The
// converged outcome only bumps tenantCyclesTotal — it does no work worth
// timing and produces no index/task deltas. The compacted outcome adds the
// removed/added/tasks deltas; both compacted and failed record a duration.
func (m *coordinatorMetrics) observeTenantCycle(
	tenant string,
	outcome string,
	duration time.Duration,
	removedIndexes int,
	addedIndexes int,
	tasks int,
) {
	m.tenantCyclesTotal.WithLabelValues(outcome, tenant).Inc()
	if outcome != "converged" {
		m.tenantCycleDurationSeconds.WithLabelValues(outcome).Observe(duration.Seconds())
	}
	if outcome == "compacted" {
		m.indexesRemovedTotal.WithLabelValues(tenant).Add(float64(removedIndexes))
		m.indexesAddedTotal.WithLabelValues(tenant).Add(float64(addedIndexes))
		m.tasksTotal.WithLabelValues(tenant).Add(float64(tasks))
	}
}
