package compactor

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// observeEntries emits RAW indicators with no SLO threshold:
//   - unconsolidated_index_backlog = max(0, len(entries)-1)
//   - oldest_backlog_log_age_seconds = now - min(End) when len > 1, else 0
//   - indexes_per_tenant_window = len(entries)
var emitNow = time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC)

// TestObserveEntries_Converged: a single index is the converged target —
// backlog 0, age 0, count 1.
func TestObserveEntries_Converged(t *testing.T) {
	entries := []indexEntry{
		{Path: "i/cur", End: emitNow.Add(-30 * time.Minute)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeEntries("acme", entries, emitNow)

	require.InDelta(t, 0.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, 0.0, gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001)
	require.InDelta(t, 1.0, gaugeValue(m.indexesPerTenantWindow, "acme"), 0.001)
}

// TestObserveEntries_Empty: no entries — all gauges 0.
func TestObserveEntries_Empty(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeEntries("acme", nil, emitNow)

	require.InDelta(t, 0.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, 0.0, gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001)
	require.InDelta(t, 0.0, gaugeValue(m.indexesPerTenantWindow, "acme"), 0.001)
}

// TestObserveEntries_Backlog: multiple indexes await consolidation. backlog =
// len-1; oldest age = now - min(End).
func TestObserveEntries_Backlog(t *testing.T) {
	// 3 entries; oldest End is 4h ago. backlog = 3-1 = 2; age = 4h.
	entries := []indexEntry{
		{Path: "i/a", End: emitNow.Add(-4 * time.Hour)},
		{Path: "i/b", End: emitNow.Add(-2 * time.Hour)},
		{Path: "i/c", End: emitNow.Add(-1 * time.Hour)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeEntries("acme", entries, emitNow)

	require.InDelta(t, 2.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, (4 * time.Hour).Seconds(),
		gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001)
	require.InDelta(t, 3.0, gaugeValue(m.indexesPerTenantWindow, "acme"), 0.001)
}

// TestObserveEntries_NoThresholdApplied pins that the age reflects the raw
// oldest index regardless of how old it is — there is no SLO threshold
// filtering here. Even a very old index contributes its full age once a
// backlog exists.
func TestObserveEntries_NoThresholdApplied(t *testing.T) {
	entries := []indexEntry{
		{Path: "i/old", End: emitNow.Add(-50 * time.Hour)},
		{Path: "i/new", End: emitNow.Add(-1 * time.Minute)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeEntries("acme", entries, emitNow)

	require.InDelta(t, 1.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, (50 * time.Hour).Seconds(),
		gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001,
		"raw SLI: oldest age is unfiltered by any SLO threshold")
}

// TestObserveEntries_IndexesPerTenantWindow_OneSeriesPerTenant:
// indexes_per_tenant_window is labeled by tenant only, so it emits exactly one
// series per tenant.
func TestObserveEntries_IndexesPerTenantWindow_OneSeriesPerTenant(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newCoordinatorMetrics(reg)
	for _, tenant := range []string{"a", "b", "c"} {
		m.observeEntries(tenant, []indexEntry{{End: emitNow.Add(-time.Hour)}}, emitNow)
	}
	mfs, err := reg.Gather()
	require.NoError(t, err)
	var ipw int
	for _, mf := range mfs {
		if mf.GetName() == metricsNamespace+"_indexes_per_tenant_window" {
			ipw = len(mf.GetMetric())
		}
	}
	require.Equal(t, 3, ipw, "3 tenants × 1 series each = 3 series")
}

// TestObserveEntries_NilReceiver: a nil *coordinatorMetrics must not panic.
func TestObserveEntries_NilReceiver(t *testing.T) {
	var m *coordinatorMetrics
	require.NotPanics(t, func() {
		m.observeEntries("acme", []indexEntry{{End: emitNow}}, emitNow)
	})
}

// TestObserveCycle checks the full-cycle counter and duration histogram.
func TestObserveCycle(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeCycle("ok", 250*time.Millisecond)
	m.observeCycle("ok", 250*time.Millisecond)
	m.observeCycle("aborted", 10*time.Millisecond)

	require.InDelta(t, 2.0,
		testutil.ToFloat64(m.cyclesTotal.WithLabelValues("ok")), 0.001)
	require.InDelta(t, 1.0,
		testutil.ToFloat64(m.cyclesTotal.WithLabelValues("aborted")), 0.001)
	require.Equal(t, uint64(3), histogramSampleCount(t, m.cycleDurationSeconds))
}

// TestObserveCycle_NilReceiver: a nil *coordinatorMetrics must not panic.
func TestObserveCycle_NilReceiver(t *testing.T) {
	var m *coordinatorMetrics
	require.NotPanics(t, func() {
		m.observeCycle("ok", time.Second)
	})
}

// TestObserveTenantCycle_Compacted checks that a compacted outcome increments
// the per-tenant cycle counter, the removed/added/tasks counters, and records
// a duration observation.
func TestObserveTenantCycle_Compacted(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeTenantCycle("acme", "compacted", 500*time.Millisecond, 5, 1, 2)

	require.InDelta(t, 1.0,
		testutil.ToFloat64(m.tenantCyclesTotal.WithLabelValues("compacted", "acme")), 0.001)
	require.InDelta(t, 5.0,
		testutil.ToFloat64(m.indexesRemovedTotal.WithLabelValues("acme")), 0.001)
	require.InDelta(t, 1.0,
		testutil.ToFloat64(m.indexesAddedTotal.WithLabelValues("acme")), 0.001)
	require.InDelta(t, 2.0,
		testutil.ToFloat64(m.tasksTotal.WithLabelValues("acme")), 0.001)
	require.Equal(t, uint64(1),
		histogramVecSampleCount(t, m.tenantCycleDurationSeconds, "compacted"))
}

// TestObserveTenantCycle_Converged checks that the converged outcome bumps only
// the per-tenant cycle counter — no index/task counters and no duration
// observation (the converged-skip path does no work worth timing).
func TestObserveTenantCycle_Converged(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeTenantCycle("acme", "converged", 0, 0, 0, 0)

	require.InDelta(t, 1.0,
		testutil.ToFloat64(m.tenantCyclesTotal.WithLabelValues("converged", "acme")), 0.001)
	require.InDelta(t, 0.0,
		testutil.ToFloat64(m.indexesRemovedTotal.WithLabelValues("acme")), 0.001)
	require.InDelta(t, 0.0,
		testutil.ToFloat64(m.indexesAddedTotal.WithLabelValues("acme")), 0.001)
	require.InDelta(t, 0.0,
		testutil.ToFloat64(m.tasksTotal.WithLabelValues("acme")), 0.001)
	require.Equal(t, uint64(0),
		histogramVecSampleCount(t, m.tenantCycleDurationSeconds, "converged"))
}

// TestObserveTenantCycle_Failed checks that the failed outcome bumps the
// per-tenant counter and records a duration observation, but does NOT touch
// the removed/added/tasks counters (no successful work was done).
func TestObserveTenantCycle_Failed(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.observeTenantCycle("acme", "failed", 750*time.Millisecond, 0, 0, 0)

	require.InDelta(t, 1.0,
		testutil.ToFloat64(m.tenantCyclesTotal.WithLabelValues("failed", "acme")), 0.001)
	require.InDelta(t, 0.0,
		testutil.ToFloat64(m.indexesRemovedTotal.WithLabelValues("acme")), 0.001)
	require.Equal(t, uint64(1),
		histogramVecSampleCount(t, m.tenantCycleDurationSeconds, "failed"))
}

// TestObserveTenantCycle_NilReceiver: a nil *coordinatorMetrics must not panic.
func TestObserveTenantCycle_NilReceiver(t *testing.T) {
	var m *coordinatorMetrics
	require.NotPanics(t, func() {
		m.observeTenantCycle("acme", "compacted", time.Second, 1, 1, 1)
	})
}

// TestObserveIndexMergeOutput_RecordsBothHistograms checks that a successful
// task observation lands one sample in each output-size histogram with the
// expected magnitudes, scoped to the task's tenant.
func TestObserveIndexMergeOutput_RecordsBothHistograms(t *testing.T) {
	m := newWorkerMetrics(prometheus.NewRegistry())
	m.ObserveIndexMergeOutput("acme", 4096, 16384)

	require.Equal(t, uint64(1),
		histogramVecSampleCount(t, m.outputBytesCompressed, "acme"))
	require.Equal(t, uint64(1),
		histogramVecSampleCount(t, m.outputBytesUncompressed, "acme"))
	require.InDelta(t, 4096.0,
		histogramVecSampleSum(t, m.outputBytesCompressed, "acme"), 0.001)
	require.InDelta(t, 16384.0,
		histogramVecSampleSum(t, m.outputBytesUncompressed, "acme"), 0.001)
}

// TestObserveIndexMergeOutput_SkipsNonPositive checks that zero/negative sizes
// are not observed — they'd skew the histograms and represent the empty-merge
// sentinel path, not a real output.
func TestObserveIndexMergeOutput_SkipsNonPositive(t *testing.T) {
	m := newWorkerMetrics(prometheus.NewRegistry())
	// Compressed positive, uncompressed zero: only compressed is recorded.
	m.ObserveIndexMergeOutput("acme", 4096, 0)

	require.Equal(t, uint64(1),
		histogramVecSampleCount(t, m.outputBytesCompressed, "acme"))
	require.Equal(t, uint64(0),
		histogramVecSampleCount(t, m.outputBytesUncompressed, "acme"))
}

// TestObserveIndexMergeOutput_NilReceiver checks the nil-guard: a nil
// *workerMetrics (observation disabled) must not panic.
func TestObserveIndexMergeOutput_NilReceiver(t *testing.T) {
	var m *workerMetrics
	require.NotPanics(t, func() {
		m.ObserveIndexMergeOutput("acme", 4096, 16384)
	})
}

func gaugeValue(g *prometheus.GaugeVec, labels ...string) float64 {
	return testutil.ToFloat64(g.WithLabelValues(labels...))
}

// histogramSampleCount returns the cumulative sample count of a plain
// Histogram by gathering it through a throwaway registry.
func histogramSampleCount(t *testing.T, h prometheus.Histogram) uint64 {
	t.Helper()
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(h))
	mfs, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, mfs, 1)
	metrics := mfs[0].GetMetric()
	require.Len(t, metrics, 1)
	return metrics[0].GetHistogram().GetSampleCount()
}

// histogramVecSampleCount returns the cumulative sample count for a single
// label-value series of a HistogramVec.
func histogramVecSampleCount(t *testing.T, hv *prometheus.HistogramVec, labels ...string) uint64 {
	t.Helper()
	obs, err := hv.GetMetricWithLabelValues(labels...)
	require.NoError(t, err)
	m := &dto.Metric{}
	require.NoError(t, obs.(prometheus.Histogram).Write(m))
	return m.GetHistogram().GetSampleCount()
}

// histogramVecSampleSum returns the cumulative sample sum for a single
// label-value series of a HistogramVec.
func histogramVecSampleSum(t *testing.T, hv *prometheus.HistogramVec, labels ...string) float64 {
	t.Helper()
	obs, err := hv.GetMetricWithLabelValues(labels...)
	require.NoError(t, err)
	m := &dto.Metric{}
	require.NoError(t, obs.(prometheus.Histogram).Write(m))
	return m.GetHistogram().GetSampleSum()
}
