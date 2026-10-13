package compactor

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

// Label names for metrics.
const (
	labelTenant  = "tenant"
	labelOutcome = "outcome"
	labelThread  = "thread"
	labelAge     = "age"
	labelPhase   = "phase"
)

// Values of the outcome label of a phase cycle.
const (
	cycleCompacted = "compacted"
	cycleConverged = "converged"
	cycleFailed    = "failed"
)

// cycleOutcomeLabel returns the outcome label of a phase cycle. A cycle that
// applied a swap is compacted even when another window or index failed, so
// the label shows progress. The phase outcome still makes the loop retry.
func cycleOutcomeLabel(stats compactionStats, failed bool) string {
	switch {
	case stats.added > 0:
		return cycleCompacted
	case failed:
		return cycleFailed
	default:
		return cycleConverged
	}
}

// coordinatorMetrics holds every metric emitted from the coordinator's
// per-tenant worker loops.
//
// Every cycle metric has one observation per phase cycle. A phase cycle is one
// run of IndexMerge or LogMerge for a tenant over every compacted window.
type coordinatorMetrics struct {
	// unconsolidatedBacklog, oldestBacklogLogAgeSeconds, and tenantIndexes
	// describe the state that the last IndexMerge cycle without an error read.
	unconsolidatedBacklog      *prometheus.GaugeVec // tenant
	oldestBacklogLogAgeSeconds *prometheus.GaugeVec // tenant
	tenantIndexes              *prometheus.GaugeVec // tenant

	tenantCyclesTotal    *prometheus.CounterVec   // phase, outcome, tenant
	cycleDurationSeconds *prometheus.HistogramVec // phase, outcome

	// indexesRemovedTotal counts source indexes removed by compaction
	// (cumulative). Use with indexesAddedTotal to compute net reduction:
	// removed - added = net_reduction.
	indexesRemovedTotal *prometheus.CounterVec // tenant

	// indexesAddedTotal counts new compacted indexes written.
	indexesAddedTotal *prometheus.CounterVec // tenant

	// tasksTotal counts compaction tasks of every kind that compacted cycles
	// dispatched. The kinds are IndexMerge, LogMerge, SortObject, and
	// IndexFilter.
	tasksTotal *prometheus.CounterVec // tenant

	// cycleBackoffSeconds measures the wait a worker applies between phases. A
	// rising distribution means workers are increasingly idle (converged, empty,
	// or failing) and backing off rather than hammering object storage.
	cycleBackoffSeconds prometheus.Histogram

	// indexInputRuns has one observation each time the IndexMerge planner
	// reads one layout group of one window.
	indexInputRuns prometheus.Histogram

	// pendingLogBytes is the uncompressed log bytes that the current LogMerge
	// phase of a tenant has not rewritten yet, by the age of their oldest data.
	pendingLogBytes *prometheus.GaugeVec // tenant, age

	// lastPlanTimestampSeconds is when a LogMerge phase of a tenant last
	// planned every index in every window without an error.
	lastPlanTimestampSeconds *prometheus.GaugeVec // tenant
}

func newCoordinatorMetrics(reg prometheus.Registerer) *coordinatorMetrics {
	f := promauto.With(reg)
	return &coordinatorMetrics{
		unconsolidatedBacklog: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_unconsolidated_index_backlog",
			Help: "Per-tenant count of nonterminal index runs beyond one, summed over every layout group in every compacted window. Set by each IndexMerge cycle without an error. Steady state: 0.",
		}, []string{labelTenant}),
		oldestBacklogLogAgeSeconds: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_oldest_backlog_log_age_seconds",
			Help: "Per-tenant age (now - min(max_timestamp)) of the oldest unconsolidated index in every compacted window. Set by each IndexMerge cycle without an error. Zero when there is no backlog.",
		}, []string{labelTenant}),
		tenantIndexes: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_tenant_indexes",
			Help: "Per-tenant raw index count, summed over every compacted window. Set by each IndexMerge cycle without an error.",
		}, []string{labelTenant}),
		tenantCyclesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_tenant_cycles_total",
			Help: "Per-tenant phase cycles over every compacted window, by phase and outcome. compacted = at least one ToC swap applied, converged = no work, failed = an error and no swap.",
		}, []string{labelPhase, labelOutcome, labelTenant}),
		cycleDurationSeconds: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_cycle_duration_seconds",
			Help:    "Wall-clock duration of one phase cycle over every compacted window, by phase and outcome.",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 20), // 10ms .. ~87m
		}, []string{labelPhase, labelOutcome}),
		indexesRemovedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_indexes_removed_total",
			Help: "Cumulative count of source index pointers removed from the ToC by compacted cycles.",
		}, []string{labelTenant}),
		indexesAddedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_indexes_added_total",
			Help: "Cumulative count of compacted index pointers written to the ToC by compacted cycles.",
		}, []string{labelTenant}),
		tasksTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_tasks_total",
			Help: "Cumulative count of compaction tasks (IndexMerge, LogMerge, SortObject, IndexFilter) dispatched per tenant by compacted cycles.",
		}, []string{labelTenant}),
		cycleBackoffSeconds: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_cycle_backoff_seconds",
			Help:    "Wait a worker applies between phases. Grows exponentially for idle (converged/empty) or failing tenants up to max-backoff.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 12), // 1s .. ~68m
		}),
		indexInputRuns: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_index_input_runs",
			Help:    "Number of strict index runs offered to the task planner. One observation per layout group in each window of an IndexMerge cycle.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 12),
		}),
		pendingLogBytes: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_pending_log_bytes",
			Help: "Uncompressed log bytes that the current LogMerge phase of the tenant has not rewritten yet, summed over all compacted windows. The age label is the upper bound of the age of the oldest data in each task, measured when the phase plans. The buckets do not overlap: sum over age for the total. Set when the phase plans, and reduced after each index swap attempt that returns no error. Failed indexes stay counted until the next phase. Indexes that fail to plan are not counted. Data that the planner treats as converged counts as zero.",
		}, []string{labelTenant, labelAge}),
		lastPlanTimestampSeconds: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_compaction_last_plan_timestamp_seconds",
			Help: "Unix time when a LogMerge phase of the tenant last planned every index in every compacted window without an error. When it is old, loki_dataobj_compaction_pending_log_bytes is stale or incomplete.",
		}, []string{labelTenant}),
	}
}

// indexBacklog is the unconsolidated index state that one IndexMerge cycle
// read for a tenant. Merge combines the state of layout groups and windows.
type indexBacklog struct {
	// indexes is the raw index count.
	indexes int
	// runs is the count of nonterminal runs beyond one in each layout group.
	runs int
	// oldestEnd is the minimum End of the indexes in layout groups with
	// backlog. It is zero when runs is zero.
	oldestEnd time.Time
}

// Merge returns the combined backlog of b and other.
func (b indexBacklog) Merge(other indexBacklog) indexBacklog {
	oldest := b.oldestEnd
	if oldest.IsZero() || (!other.oldestEnd.IsZero() && other.oldestEnd.Before(oldest)) {
		oldest = other.oldestEnd
	}
	return indexBacklog{
		indexes:   b.indexes + other.indexes,
		runs:      b.runs + other.runs,
		oldestEnd: oldest,
	}
}

// groupBacklog returns the backlog of one layout group with the given run
// count. It does not count indexes, because the window counts them once.
func groupBacklog(converged bool, runs int, entries []indexEntry) indexBacklog {
	if converged || runs <= 1 {
		return indexBacklog{}
	}
	return indexBacklog{runs: runs - 1, oldestEnd: oldestEnd(entries)}
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

func (m *coordinatorMetrics) observeIndexBacklog(tenant string, b indexBacklog, now time.Time) {
	m.unconsolidatedBacklog.WithLabelValues(tenant).Set(float64(b.runs))
	m.tenantIndexes.WithLabelValues(tenant).Set(float64(b.indexes))

	var oldestAge float64
	if !b.oldestEnd.IsZero() {
		oldestAge = now.Sub(b.oldestEnd).Seconds()
	}
	m.oldestBacklogLogAgeSeconds.WithLabelValues(tenant).Set(oldestAge)
}

// observeCycle records one phase cycle of the tenant. stats holds the swaps
// that applied. failed reports an error in any window or index.
func (m *coordinatorMetrics) observeCycle(tenant string, p phase, stats compactionStats, failed bool, duration time.Duration) {
	outcome := cycleOutcomeLabel(stats, failed)
	m.tenantCyclesTotal.WithLabelValues(p.String(), outcome, tenant).Inc()
	m.cycleDurationSeconds.WithLabelValues(p.String(), outcome).Observe(duration.Seconds())
	if outcome == cycleCompacted {
		m.indexesRemovedTotal.WithLabelValues(tenant).Add(float64(stats.removed))
		m.indexesAddedTotal.WithLabelValues(tenant).Add(float64(stats.added))
		m.tasksTotal.WithLabelValues(tenant).Add(float64(stats.dispatched))
	}
}

// deleteTenant drops every per-tenant coordinator series for the tenant so a
// stopped worker's gauges do not linger and report a disabled tenant's
// last-known values. cycleDurationSeconds and cycleBackoffSeconds have no
// tenant label, so they keep their series.
func (m *coordinatorMetrics) deleteTenant(tenant string) {
	m.unconsolidatedBacklog.DeleteLabelValues(tenant)
	m.oldestBacklogLogAgeSeconds.DeleteLabelValues(tenant)
	m.tenantIndexes.DeleteLabelValues(tenant)
	m.pendingLogBytes.DeletePartialMatch(prometheus.Labels{labelTenant: tenant})
	m.lastPlanTimestampSeconds.DeleteLabelValues(tenant)
	m.indexesRemovedTotal.DeleteLabelValues(tenant)
	m.indexesAddedTotal.DeleteLabelValues(tenant)
	m.tasksTotal.DeleteLabelValues(tenant)
	m.tenantCyclesTotal.DeletePartialMatch(prometheus.Labels{labelTenant: tenant})
}

// workerMetrics holds the worker-side metrics that the IndexMerge and LogMerge
// executors emit.
type workerMetrics struct {
	outputBytesCompressed   *prometheus.HistogramVec // tenant
	outputBytesUncompressed *prometheus.HistogramVec // tenant

	logMergeTasksTotal            *prometheus.CounterVec   // tenant, outcome
	logMergeDurationSeconds       *prometheus.HistogramVec // tenant
	logMergeOutputBytesCompressed *prometheus.HistogramVec // tenant
	logMergeTaskInputBytes        *prometheus.HistogramVec // tenant
	logMergeTaskInputBytesTotal   *prometheus.CounterVec   // thread
	logMergeDuplicateRecordsTotal *prometheus.CounterVec   // tenant
}

func newWorkerMetrics(reg prometheus.Registerer) *workerMetrics {
	f := promauto.With(reg)
	byteBuckets := prometheus.ExponentialBuckets(512*1024, 2, 21) // 512KB - 0.5TB
	durationBuckets := prometheus.ExponentialBuckets(1, 2, 16)
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
			Help: "LogMerge tasks by outcome. success = compacted log objects uploaded, empty = no source data or records.",
		}, []string{labelTenant, labelOutcome}),
		logMergeDurationSeconds: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_duration_seconds",
			Help:    "Wall-clock duration of a LogMerge task on the worker.",
			Buckets: durationBuckets,

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{labelTenant}),
		logMergeOutputBytesCompressed: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_output_bytes_compressed",
			Help:    "Total encoded bytes uploaded across all compacted log objects for a successful LogMerge task.",
			Buckets: byteBuckets,
		}, []string{labelTenant}),
		logMergeTaskInputBytes: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_compaction_log_merge_task_input_bytes",
			Help:    "Log line and structured metadata value bytes merged by a LogMerge task. One observation per task, for the same tasks as loki_dataobj_compaction_log_merge_duration_seconds.",
			Buckets: byteBuckets,

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{labelTenant}),
		logMergeTaskInputBytesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_log_merge_task_input_bytes_total",
			Help: "Log line and structured metadata value bytes merged by LogMerge tasks, counted while tasks run. It tracks the same bytes as loki_dataobj_compaction_log_merge_task_input_bytes. Includes tasks that later fail. A thread runs one task at a time, so rate() per thread gives the throughput of its running task.",
		}, []string{labelThread}),
		logMergeDuplicateRecordsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_log_merge_duplicate_records_total",
			Help: "Records in successful LogMerge tasks that repeat an earlier record's stream, timestamp, line, and structured metadata. Duplicates are kept in the output.",
		}, []string{labelTenant}),
	}
}

// ObserveIndexMergeOutput satisfies executor.IndexMergeObserver. Called by
// the IndexMerge executor on the worker after each successful task.
func (m *workerMetrics) ObserveIndexMergeOutput(tenant string, compressed, uncompressed int64) {
	if compressed > 0 {
		m.outputBytesCompressed.WithLabelValues(tenant).Observe(float64(compressed))
	}
	if uncompressed > 0 {
		m.outputBytesUncompressed.WithLabelValues(tenant).Observe(float64(uncompressed))
	}
}

// logMergeObserver returns the executor.LogMergeObserver for one worker
// thread. It satisfies executor.NewLogMergeObserverFunc.
func (m *workerMetrics) logMergeObserver(thread int) executor.LogMergeObserver {
	return &threadLogMergeMetrics{
		workerMetrics: m,
		inputBytes:    m.logMergeTaskInputBytesTotal.WithLabelValues(strconv.Itoa(thread)),
	}
}

// threadLogMergeMetrics is the executor.LogMergeObserver for one worker
// thread. It holds the thread's input bytes counter, so the per-record path
// does no label lookup.
type threadLogMergeMetrics struct {
	*workerMetrics
	inputBytes prometheus.Counter
}

// ObserveLogMergeInputBytes satisfies executor.LogMergeObserver.
func (m *threadLogMergeMetrics) ObserveLogMergeInputBytes(bytes int64) {
	m.inputBytes.Add(float64(bytes))
}

// ObserveLogMerge satisfies executor.LogMergeObserver. Called by the LogMerge
// executor on the worker after each task attempt.
func (m *workerMetrics) ObserveLogMerge(tenant string, stats executor.LogMergeObservedStats, duration time.Duration) {
	outcome := stats.Outcome
	if outcome == "" {
		outcome = "unknown"
	}
	m.logMergeTasksTotal.WithLabelValues(tenant, outcome).Inc()
	m.logMergeDurationSeconds.WithLabelValues(tenant).Observe(duration.Seconds())
	m.logMergeTaskInputBytes.WithLabelValues(tenant).Observe(float64(stats.InputBytes))
	if stats.Outcome != "success" {
		return
	}
	if stats.OutputBytesCompressed > 0 {
		m.logMergeOutputBytesCompressed.WithLabelValues(tenant).Observe(float64(stats.OutputBytesCompressed))
	}
	m.logMergeDuplicateRecordsTotal.WithLabelValues(tenant).Add(float64(stats.DuplicateRecords))
}
