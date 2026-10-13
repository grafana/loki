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
	m.tenantIndexes.WithLabelValues("acme").Set(2)
	m.indexesRemovedTotal.WithLabelValues("acme").Add(1)
	m.indexesAddedTotal.WithLabelValues("acme").Add(3)
	m.tasksTotal.WithLabelValues("acme").Add(4)
	m.tenantCyclesTotal.WithLabelValues("index_merge", "compacted", "acme").Inc()
	m.tenantCyclesTotal.WithLabelValues("log_merge", "compacted", "acme").Inc()
	m.pendingLogBytes.WithLabelValues("acme", "1h").Set(100)
	m.pendingLogBytes.WithLabelValues("acme", "+Inf").Set(50)
	m.lastPlanTimestampSeconds.WithLabelValues("acme").Set(10)

	// A second tenant that must survive.
	m.unconsolidatedBacklog.WithLabelValues("other").Set(7)
	m.tenantCyclesTotal.WithLabelValues("index_merge", "compacted", "other").Inc()

	// A non-tenant-labeled metric that must survive.
	m.cycleDurationSeconds.WithLabelValues("index_merge", "compacted").Observe(1)

	// Baseline: every series exists before the delete. Counting series (rather
	// than reading values via ToFloat64) avoids lazily re-creating a series and
	// keeps the post-delete assertions honest.
	require.Equal(t, 2, testutil.CollectAndCount(m.unconsolidatedBacklog), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantIndexes))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 3, testutil.CollectAndCount(m.tenantCyclesTotal), "acme in both phases + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.cycleDurationSeconds))
	require.Equal(t, 2, testutil.CollectAndCount(m.pendingLogBytes))
	require.Equal(t, 1, testutil.CollectAndCount(m.lastPlanTimestampSeconds))

	m.deleteTenant("acme")

	// Every acme-only series is gone; series shared with "other" drop only acme.
	require.Equal(t, 0, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 0, testutil.CollectAndCount(m.tenantIndexes))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.pendingLogBytes))
	require.Equal(t, 0, testutil.CollectAndCount(m.lastPlanTimestampSeconds))

	// The other tenant and the non-tenant metric are untouched.
	require.Equal(t, 1, testutil.CollectAndCount(m.unconsolidatedBacklog), "only the surviving tenant remains")
	require.Equal(t, 7.0, testutil.ToFloat64(m.unconsolidatedBacklog.WithLabelValues("other")))
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantCyclesTotal), "other tenant's cycle series survives")
	require.Equal(t, 1, testutil.CollectAndCount(m.cycleDurationSeconds), "non-tenant-labeled metric survives")
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

func TestIndexBacklog_Merge(t *testing.T) {
	older := time.Unix(100, 0)
	newer := time.Unix(200, 0)

	t.Run("sums indexes and runs and keeps the oldest end", func(t *testing.T) {
		a := indexBacklog{indexes: 3, runs: 1, oldestEnd: newer}
		b := indexBacklog{indexes: 2, runs: 4, oldestEnd: older}

		require.Equal(t, indexBacklog{indexes: 5, runs: 5, oldestEnd: older}, a.Merge(b))
		require.Equal(t, indexBacklog{indexes: 5, runs: 5, oldestEnd: older}, b.Merge(a))
	})

	t.Run("ignores the zero end of a backlog without runs", func(t *testing.T) {
		a := indexBacklog{indexes: 1}
		b := indexBacklog{indexes: 2, runs: 1, oldestEnd: newer}

		require.Equal(t, newer, a.Merge(b).oldestEnd)
		require.Equal(t, newer, b.Merge(a).oldestEnd)
	})
}

func TestCoordinatorMetrics_ObserveCycle(t *testing.T) {
	t.Run("records a cycle with a swap and an error as compacted and adds its deltas", func(t *testing.T) {
		m := newCoordinatorMetrics(prometheus.NewRegistry())

		m.observeCycle("acme", phaseIndexMerge, compactionStats{removed: 4, added: 1, dispatched: 1}, true, time.Second)

		require.Equal(t, 1.0, testutil.ToFloat64(m.tenantCyclesTotal.WithLabelValues("index_merge", cycleCompacted, "acme")))
		require.Equal(t, 4.0, testutil.ToFloat64(m.indexesRemovedTotal.WithLabelValues("acme")))
		require.Equal(t, 1.0, testutil.ToFloat64(m.indexesAddedTotal.WithLabelValues("acme")))
	})

	t.Run("records a converged cycle in the duration histogram and adds no deltas", func(t *testing.T) {
		m := newCoordinatorMetrics(prometheus.NewRegistry())

		m.observeCycle("acme", phaseLogMerge, compactionStats{}, false, time.Second)

		require.Equal(t, 1.0, testutil.ToFloat64(m.tenantCyclesTotal.WithLabelValues("log_merge", cycleConverged, "acme")))
		require.Equal(t, 1, testutil.CollectAndCount(m.cycleDurationSeconds))
		require.Zero(t, testutil.CollectAndCount(m.indexesAddedTotal))
	})

	t.Run("records a cycle with an error and no swap as failed", func(t *testing.T) {
		m := newCoordinatorMetrics(prometheus.NewRegistry())

		m.observeCycle("acme", phaseLogMerge, compactionStats{}, true, time.Second)

		require.Equal(t, 1.0, testutil.ToFloat64(m.tenantCyclesTotal.WithLabelValues("log_merge", cycleFailed, "acme")))
	})
}
