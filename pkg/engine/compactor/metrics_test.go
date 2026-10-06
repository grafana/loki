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
	m.indexesPerTenantWindow.WithLabelValues("acme").Set(2)
	m.indexesRemovedTotal.WithLabelValues("acme").Add(1)
	m.indexesAddedTotal.WithLabelValues("acme").Add(3)
	m.tasksTotal.WithLabelValues("acme").Add(4)
	m.tenantCyclesTotal.WithLabelValues("compacted", "acme").Inc()
	m.tenantLogCyclesTotal.WithLabelValues("compacted", "acme").Inc()

	// A second tenant that must survive.
	m.unconsolidatedBacklog.WithLabelValues("other").Set(7)
	m.tenantCyclesTotal.WithLabelValues("compacted", "other").Inc()

	// A non-tenant-labeled metric that must survive.
	m.cyclesTotal.WithLabelValues("compacted").Inc()

	// Baseline: every series exists before the delete. Counting series (rather
	// than reading values via ToFloat64) avoids lazily re-creating a series and
	// keeps the post-delete assertions honest.
	require.Equal(t, 2, testutil.CollectAndCount(m.unconsolidatedBacklog), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesPerTenantWindow))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 2, testutil.CollectAndCount(m.tenantCyclesTotal), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantLogCyclesTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.cyclesTotal))

	m.deleteTenant("acme")

	// Every acme-only series is gone; series shared with "other" drop only acme.
	require.Equal(t, 0, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesPerTenantWindow))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tenantLogCyclesTotal))

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
