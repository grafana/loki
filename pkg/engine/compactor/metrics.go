package compactor

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

// Label names for metrics.
const (
	labelTenant  = "tenant"
	labelOutcome = "outcome"
)

// coordinatorMetrics holds every metric emitted from the coordinator's
// per-tenant worker loops.
type coordinatorMetrics struct {
	unconsolidatedBacklog      *prometheus.GaugeVec // tenant
	oldestBacklogLogAgeSeconds *prometheus.GaugeVec // tenant
	indexesPerTenantWindow     *prometheus.GaugeVec // tenant

	// cyclesTotal counts worker-loop phase iterations by outcome.
	// outcome=compacted|converged|failed
	cyclesTotal *prometheus.CounterVec

	// tenantCyclesTotal counts per-tenant index-compaction cycle outcomes.
	tenantCyclesTotal *prometheus.CounterVec // outcome=compacted|converged|failed, tenant

	// tenantLogCyclesTotal counts per-tenant log-compaction cycle outcomes.
	tenantLogCyclesTotal *prometheus.CounterVec // outcome=compacted|converged|failed, tenant

	// indexesRemovedTotal counts source indexes removed by compaction
	// (cumulative). Use with indexesAddedTotal to compute net reduction:
	// removed - added = net_reduction.
	indexesRemovedTotal *prometheus.CounterVec // tenant

	// indexesAddedTotal counts new compacted indexes written.
	indexesAddedTotal *prometheus.CounterVec // tenant

	// tasksTotal counts IndexMerge tasks dispatched per tenant (cumulative).
	tasksTotal *prometheus.CounterVec // tenant

	// cycleDurationSeconds measures wall-clock duration of one worker-loop phase
	// iteration (IndexMerge or LogMerge).
	cycleDurationSeconds prometheus.Histogram

	// tenantCycleDurationSeconds measures per-tenant cycle wall-clock
	// duration. Excludes the converged-skip path.
	tenantCycleDurationSeconds *prometheus.HistogramVec // outcome=compacted|failed

	// fileSizeStatDurationSeconds measures the latency of a single
	// bucket.Attributes call issued while filling in index FileSize. These
	// stats run concurrently before a ToC replace; the histogram surfaces
	// per-call latency early so it can be caught before it dominates a cycle.
	fileSizeStatDurationSeconds prometheus.Histogram
	indexInputRuns              prometheus.Histogram
}

func newCoordinatorMetrics(reg prometheus.Registerer) *coordinatorMetrics {
	f := promauto.With(reg)
	return &coordinatorMetrics{
		unconsolidatedBacklog: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_unconsolidated_index_backlog",
			Help: "Per-tenant count of nonterminal index runs beyond one. Steady state: 0.",
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
			Help: "Total worker-loop phase iterations by outcome (compacted = ToC swap applied, converged = no work, failed = phase error).",
		}, []string{labelOutcome}),
		tenantCyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_tenant_cycles_total",
			Help: "Per-tenant index-compaction cycle outcomes. compacted = ToC swapped, converged = no true section overlap, failed = cycle returned error.",
		}, []string{labelOutcome, labelTenant}),
		tenantLogCyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_tenant_log_cycles_total",
			Help: "Per-tenant log-compaction cycle outcomes. compacted = ToC swapped, converged = no worthwhile section overlap, failed = cycle returned error.",
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
			Help:    "Wall-clock duration of one worker-loop phase iteration (IndexMerge or LogMerge).",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms .. ~80s
		}),
		tenantCycleDurationSeconds: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_tenant_cycle_duration_seconds",
			Help:    "Per-tenant cycle wall-clock duration. Excludes the converged-skip path.",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 14),
		}, []string{"outcome"}),
		fileSizeStatDurationSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_file_size_stat_duration_seconds",
			Help:    "Latency of a single object-storage Attributes call issued to fill in index file size before a ToC replace.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 14), // 1ms .. ~8s
		}),
		indexInputRuns: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_index_input_runs",
			Help:    "Number of strict index runs offered to the task planner.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 12),
		}),
	}
}

func (m *coordinatorMetrics) observeIndexInputRuns(runs int) {
	if m != nil {
		m.indexInputRuns.Observe(float64(runs))
	}
}

func (m *coordinatorMetrics) observeIndexConvergence(tenant string, converged bool, runs int, entries []indexEntry, now time.Time) {
	if m == nil {
		return
	}
	backlog := max(0, runs-1)
	if converged {
		backlog = 0
	}
	m.unconsolidatedBacklog.WithLabelValues(tenant).Set(float64(backlog))

	var oldestAge float64
	if backlog > 0 {
		oldest := oldestEnd(entries)
		if !oldest.IsZero() {
			oldestAge = now.Sub(oldest).Seconds()
		}
	}
	m.oldestBacklogLogAgeSeconds.WithLabelValues(tenant).Set(oldestAge)
}

// observeEntries records the raw ToC index count before section discovery.
func (m *coordinatorMetrics) observeEntries(tenant string, entries []indexEntry) {
	if m != nil {
		m.indexesPerTenantWindow.WithLabelValues(tenant).Set(float64(len(entries)))
	}
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

// observeCycle records the outcome and wall-clock duration of one worker-loop
// phase iteration.
func (m *coordinatorMetrics) observeCycle(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.cyclesTotal.WithLabelValues(outcome).Inc()
	m.cycleDurationSeconds.Observe(duration.Seconds())
}

// observeTenantCycle records per-tenant index-compaction cycle outcomes and
// counts. stats is consulted for the compacted outcome; pass the zero value for
// converged/failed.
func (m *coordinatorMetrics) observeTenantCycle(
	tenant string,
	outcome string,
	duration time.Duration,
	stats compactionStats,
) {
	if m == nil {
		return
	}
	m.tenantCyclesTotal.WithLabelValues(outcome, tenant).Inc()
	m.recordTenantCycle(tenant, outcome, duration, stats)
}

// observeTenantLogCycle records per-tenant log-compaction cycle outcomes and
// counts. stats is consulted for the compacted outcome; pass the zero value for
// converged/failed.
func (m *coordinatorMetrics) observeTenantLogCycle(
	tenant string,
	outcome string,
	duration time.Duration,
	stats compactionStats,
) {
	if m == nil {
		return
	}
	m.tenantLogCyclesTotal.WithLabelValues(outcome, tenant).Inc()
	m.recordTenantCycle(tenant, outcome, duration, stats)
}

// recordTenantCycle records the duration and index/task deltas shared by both
// index- and log-compaction cycles.
func (m *coordinatorMetrics) recordTenantCycle(
	tenant string,
	outcome string,
	duration time.Duration,
	stats compactionStats,
) {
	// The converged outcome skips the duration histogram.
	if outcome != "converged" {
		m.tenantCycleDurationSeconds.WithLabelValues(outcome).Observe(duration.Seconds())
	}
	// The compacted outcome adds/removes indexes and dispatches tasks, so
	// record its deltas.
	if outcome == "compacted" {
		m.indexesRemovedTotal.WithLabelValues(tenant).Add(float64(stats.removed))
		m.indexesAddedTotal.WithLabelValues(tenant).Add(float64(stats.added))
		m.tasksTotal.WithLabelValues(tenant).Add(float64(stats.dispatched))
	}
}

// deleteTenant drops every per-tenant coordinator series for the tenant so a
// stopped worker's gauges do not linger and report a disabled tenant's
// last-known values. cyclesTotal, cycleDurationSeconds, and
// tenantCycleDurationSeconds are intentionally omitted: cyclesTotal and
// cycleDurationSeconds carry no tenant label, and tenantCycleDurationSeconds
// carries only an outcome label (no tenant label).
func (m *coordinatorMetrics) deleteTenant(tenant string) {
	if m == nil {
		return
	}
	m.unconsolidatedBacklog.DeleteLabelValues(tenant)
	m.oldestBacklogLogAgeSeconds.DeleteLabelValues(tenant)
	m.indexesPerTenantWindow.DeleteLabelValues(tenant)
	m.indexesRemovedTotal.DeleteLabelValues(tenant)
	m.indexesAddedTotal.DeleteLabelValues(tenant)
	m.tasksTotal.DeleteLabelValues(tenant)
	m.tenantCyclesTotal.DeletePartialMatch(prometheus.Labels{labelTenant: tenant})
	m.tenantLogCyclesTotal.DeletePartialMatch(prometheus.Labels{labelTenant: tenant})
}

// observeFileSizeStat records the latency of a single bucket.Attributes call.
// Safe to call concurrently from the fillFileSizes goroutines.
func (m *coordinatorMetrics) observeFileSizeStat(duration time.Duration) {
	if m == nil {
		return
	}
	m.fileSizeStatDurationSeconds.Observe(duration.Seconds())
}

// workerMetrics holds the worker-side metrics that the IndexMerge and LogMerge
// executors emit.
type workerMetrics struct {
	outputBytesCompressed   *prometheus.HistogramVec // tenant
	outputBytesUncompressed *prometheus.HistogramVec // tenant

	logMergeTasksTotal              *prometheus.CounterVec   // tenant, outcome
	logMergeDurationSeconds         *prometheus.HistogramVec // tenant
	logMergeOutputRecords           *prometheus.HistogramVec // tenant
	logMergeOutputStreams           *prometheus.HistogramVec // tenant
	logMergeOutputBytesCompressed   *prometheus.HistogramVec // tenant
	logMergeOutputBytesUncompressed *prometheus.HistogramVec // tenant
}

func newWorkerMetrics(reg prometheus.Registerer) *workerMetrics {
	f := promauto.With(reg)
	byteBuckets := prometheus.ExponentialBuckets(1024, 2, 21)
	countBuckets := prometheus.ExponentialBuckets(1, 2, 16)
	durationBuckets := prometheus.ExponentialBuckets(0.01, 2, 14)
	return &workerMetrics{
		outputBytesCompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_output_bytes_compressed",
			Help:    "Size of the bytes uploaded to object storage per successful IndexMerge task (post-encoding, what's stored at rest). One observation per task.",
			Buckets: byteBuckets,
		}, []string{labelTenant}),
		outputBytesUncompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_output_bytes_uncompressed",
			Help:    "Estimated in-memory size of the IndexMerge builder (postings + stats sections, pre-encoding) at Flush. One observation per task.",
			Buckets: byteBuckets,
		}, []string{labelTenant}),
		logMergeTasksTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_log_merge_tasks_total",
			Help: "LogMerge tasks by outcome. success = compacted log objects uploaded, short_circuit = output index already present, empty = no source data or records.",
		}, []string{labelTenant, labelOutcome}),
		logMergeDurationSeconds: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_duration_seconds",
			Help:    "Wall-clock duration of a LogMerge task on the worker.",
			Buckets: durationBuckets,
		}, []string{labelTenant}),
		logMergeOutputRecords: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_output_records",
			Help:    "Number of log records written by a successful LogMerge task.",
			Buckets: countBuckets,
		}, []string{labelTenant}),
		logMergeOutputStreams: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_output_streams",
			Help:    "Number of output streams written by a successful LogMerge task.",
			Buckets: countBuckets,
		}, []string{labelTenant}),
		logMergeOutputBytesCompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_output_bytes_compressed",
			Help:    "Total encoded bytes uploaded across all compacted log objects for a successful LogMerge task.",
			Buckets: byteBuckets,
		}, []string{labelTenant}),
		logMergeOutputBytesUncompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_output_bytes_uncompressed",
			Help:    "Estimated uncompressed bytes of log records written by a successful LogMerge task.",
			Buckets: byteBuckets,
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

// ObserveLogMerge satisfies executor.LogMergeObserver. Called by the LogMerge
// executor on the worker after each task attempt.
func (m *workerMetrics) ObserveLogMerge(tenant string, stats executor.LogMergeObservedStats, duration time.Duration) {
	if m == nil {
		return
	}
	outcome := stats.Outcome
	if outcome == "" {
		outcome = "unknown"
	}
	m.logMergeTasksTotal.WithLabelValues(tenant, outcome).Inc()
	m.logMergeDurationSeconds.WithLabelValues(tenant).Observe(duration.Seconds())
	if stats.Outcome != "success" {
		return
	}
	if stats.OutputRecords > 0 {
		m.logMergeOutputRecords.WithLabelValues(tenant).Observe(float64(stats.OutputRecords))
	}
	if stats.OutputStreams > 0 {
		m.logMergeOutputStreams.WithLabelValues(tenant).Observe(float64(stats.OutputStreams))
	}
	if stats.OutputBytesCompressed > 0 {
		m.logMergeOutputBytesCompressed.WithLabelValues(tenant).Observe(float64(stats.OutputBytesCompressed))
	}
	if stats.OutputBytesUncompressed > 0 {
		m.logMergeOutputBytesUncompressed.WithLabelValues(tenant).Observe(float64(stats.OutputBytesUncompressed))
	}
}
