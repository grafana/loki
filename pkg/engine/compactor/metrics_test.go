package compactor

import (
	"testing"
	"time"

	"github.com/grafana/dskit/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

func TestCoordinatorMetrics_DeleteTenant(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())

	// Target tenant series across every per-tenant vector.
	m.unconsolidatedBacklog.WithLabelValues("acme").Set(5)
	m.oldestBacklogLogAgeSeconds.WithLabelValues("acme").Set(1)
	m.indexesPerTenantWindow.WithLabelValues("acme", "0").Set(2)
	m.indexesPerTenantWindow.WithLabelValues("acme", "1").Set(20)
	m.indexesRemovedTotal.WithLabelValues("acme").Add(1)
	m.indexesAddedTotal.WithLabelValues("acme").Add(3)
	m.tasksTotal.WithLabelValues("acme").Add(4)
	m.tenantCyclesTotal.WithLabelValues("compacted", "acme").Inc()
	m.tenantLogCyclesTotal.WithLabelValues("compacted", "acme").Inc()
	m.pendingLogBytes.WithLabelValues("acme", "1h").Set(100)
	m.pendingLogBytes.WithLabelValues("acme", "+Inf").Set(50)
	m.lastPlanTimestampSeconds.WithLabelValues("acme").Set(10)
	m.singleObjectLogRuns.WithLabelValues("acme").Set(3)
	m.logMergeFailedIndexesTotal.WithLabelValues("acme", failReasonPlan).Inc()
	m.logMergeSkippedMergesTotal.WithLabelValues("acme").Inc()
	m.indexMergeFailedGroupsTotal.WithLabelValues("acme").Inc()

	// A second tenant that must survive.
	m.unconsolidatedBacklog.WithLabelValues("other").Set(7)
	m.tenantCyclesTotal.WithLabelValues("compacted", "other").Inc()
	m.indexesPerTenantWindow.WithLabelValues("other", "0").Set(4)

	// A non-tenant-labeled metric that must survive.
	m.cyclesTotal.WithLabelValues("compacted").Inc()

	// Baseline: every series exists before the delete. Counting series (rather
	// than reading values via ToFloat64) avoids lazily re-creating a series and
	// keeps the post-delete assertions honest.
	require.Equal(t, 2, testutil.CollectAndCount(m.unconsolidatedBacklog), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 3, testutil.CollectAndCount(m.indexesPerTenantWindow), "two acme window lookbacks + other")
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 2, testutil.CollectAndCount(m.tenantCyclesTotal), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantLogCyclesTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.cyclesTotal))
	require.Equal(t, 2, testutil.CollectAndCount(m.pendingLogBytes))
	require.Equal(t, 1, testutil.CollectAndCount(m.lastPlanTimestampSeconds))
	require.Equal(t, 1, testutil.CollectAndCount(m.singleObjectLogRuns))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexMergeFailedGroupsTotal))

	m.deleteTenant("acme")

	// Every acme-only series is gone; series shared with "other" drop only acme.
	require.Equal(t, 0, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesPerTenantWindow), "only the other tenant's window series remains")
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tenantLogCyclesTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.pendingLogBytes))
	require.Equal(t, 0, testutil.CollectAndCount(m.lastPlanTimestampSeconds))
	require.Equal(t, 0, testutil.CollectAndCount(m.singleObjectLogRuns))
	require.Equal(t, 0, testutil.CollectAndCount(m.logMergeFailedIndexesTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.logMergeSkippedMergesTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexMergeFailedGroupsTotal))

	// The other tenant and the non-tenant metric are untouched.
	require.Equal(t, 1, testutil.CollectAndCount(m.unconsolidatedBacklog), "only the surviving tenant remains")
	require.Equal(t, 7.0, testutil.ToFloat64(m.unconsolidatedBacklog.WithLabelValues("other")))
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantCyclesTotal), "other tenant's cycle series survives")
	require.Equal(t, 1, testutil.CollectAndCount(m.cyclesTotal), "non-tenant-labeled metric survives")
}

func TestWorkerMetrics_LogMergeObserver(t *testing.T) {
	t.Run("adds input bytes to the series of the observer's thread only", func(t *testing.T) {
		m := newWorkerMetrics(prometheus.NewRegistry())
		thread0 := m.logMergeObserver(0)
		thread3 := m.logMergeObserver(3)

		thread0.ObserveLogMergeInputBytes(10)
		thread0.ObserveLogMergeInputBytes(5)
		thread3.ObserveLogMergeInputBytes(7)

		require.Equal(t, 2, testutil.CollectAndCount(m.logMergeTaskInputBytesTotal))
		require.Equal(t, 15.0, testutil.ToFloat64(m.logMergeTaskInputBytesTotal.WithLabelValues("0")))
		require.Equal(t, 7.0, testutil.ToFloat64(m.logMergeTaskInputBytesTotal.WithLabelValues("3")))
	})
}

func TestWorkerMetrics_ObserveLogMerge(t *testing.T) {
	t.Run("observes task input bytes for every outcome so the sum pairs with the duration sum", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m := newWorkerMetrics(reg)

		m.ObserveLogMerge("acme", executor.LogMergeObservedStats{Outcome: "success", InputBytes: 300}, 3*time.Second)
		m.ObserveLogMerge("acme", executor.LogMergeObservedStats{Outcome: "empty"}, time.Second)

		mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		inputBytes, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_compaction_log_merge_task_input_bytes", labelTenant, "acme")
		require.NoError(t, err)
		duration, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_compaction_log_merge_duration_seconds", labelTenant, "acme")
		require.NoError(t, err)
		require.Equal(t, uint64(2), inputBytes.GetSampleCount())
		require.Equal(t, duration.GetSampleCount(), inputBytes.GetSampleCount())
		require.Equal(t, 300.0, inputBytes.GetSampleSum())
		require.Equal(t, 4.0, duration.GetSampleSum())
	})

	t.Run("exposes task input bytes and duration as both classic and native histograms", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m := newWorkerMetrics(reg)

		m.ObserveLogMerge("acme", executor.LogMergeObservedStats{Outcome: "success", InputBytes: 1 << 30}, 20*time.Minute)

		mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		for _, name := range []string{
			"loki_dataobj_compaction_log_merge_task_input_bytes",
			"loki_dataobj_compaction_log_merge_duration_seconds",
		} {
			h, err := metrics.FindHistogramWithNameAndLabels(mfm, name, labelTenant, "acme")
			require.NoError(t, err, name)
			require.NotEmpty(t, h.GetBucket(), name)
			require.NotEmpty(t, h.GetPositiveSpan(), name)
		}
	})
}

func TestWorkerMetrics_IndexMergeObserver(t *testing.T) {
	t.Run("adds input bytes to the series of the observer's thread only", func(t *testing.T) {
		m := newWorkerMetrics(prometheus.NewRegistry())
		thread0 := m.indexMergeObserver(0)
		thread3 := m.indexMergeObserver(3)

		thread0.ObserveIndexMergeInputBytes(10)
		thread0.ObserveIndexMergeInputBytes(5)
		thread3.ObserveIndexMergeInputBytes(7)

		require.Equal(t, 2, testutil.CollectAndCount(m.indexMergeTaskInputBytesTotal))
		require.Equal(t, 15.0, testutil.ToFloat64(m.indexMergeTaskInputBytesTotal.WithLabelValues("0")))
		require.Equal(t, 7.0, testutil.ToFloat64(m.indexMergeTaskInputBytesTotal.WithLabelValues("3")))
	})
}

func TestWorkerMetrics_ObserveIndexMerge(t *testing.T) {
	t.Run("counts every outcome and observes input bytes and duration for each attempt", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m := newWorkerMetrics(reg)

		m.ObserveIndexMerge("acme", executor.IndexMergeObservedStats{Outcome: "success", PostingsInputBytes: 200, StatsInputBytes: 100}, 3*time.Second)
		m.ObserveIndexMerge("acme", executor.IndexMergeObservedStats{Outcome: "failed", PostingsInputBytes: 50}, time.Second)

		require.Equal(t, 1.0, testutil.ToFloat64(m.indexMergeTasksTotal.WithLabelValues("acme", "success")))
		require.Equal(t, 1.0, testutil.ToFloat64(m.indexMergeTasksTotal.WithLabelValues("acme", "failed")))

		mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		inputBytes, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_compaction_index_merge_task_input_bytes", labelTenant, "acme")
		require.NoError(t, err)
		duration, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_compaction_index_merge_duration_seconds", labelTenant, "acme")
		require.NoError(t, err)
		require.Equal(t, uint64(2), inputBytes.GetSampleCount())
		require.Equal(t, duration.GetSampleCount(), inputBytes.GetSampleCount())
		require.Equal(t, 350.0, inputBytes.GetSampleSum())
		require.Equal(t, 4.0, duration.GetSampleSum())
	})

	t.Run("observes output sizes only for attempts that report output", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m := newWorkerMetrics(reg)

		m.ObserveIndexMerge("acme", executor.IndexMergeObservedStats{Outcome: "success", OutputBytesCompressed: 40, OutputBytesUncompressed: 90}, time.Second)
		m.ObserveIndexMerge("acme", executor.IndexMergeObservedStats{Outcome: "failed"}, time.Second)

		mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		compressed, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_compaction_index_merge_output_bytes_compressed", labelTenant, "acme")
		require.NoError(t, err)
		uncompressed, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_compaction_index_merge_output_bytes_uncompressed", labelTenant, "acme")
		require.NoError(t, err)
		require.Equal(t, uint64(1), compressed.GetSampleCount())
		require.Equal(t, 40.0, compressed.GetSampleSum())
		require.Equal(t, uint64(1), uncompressed.GetSampleCount())
		require.Equal(t, 90.0, uncompressed.GetSampleSum())
	})
}
