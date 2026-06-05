package compactor

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// With 12h windows: current = [12:00, 24:00), previous = [00:00, 12:00).
// "Now" pinned at 13:00; SLO = 3h ⇒ threshold = 10:00.
var (
	emitSLONow       = time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC)
	emitSLOCurStart  = time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	emitSLOPrevStart = time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	emitSLOSlo       = 3 * time.Hour
)

func TestEmitSLO_Converged(t *testing.T) {
	current := []indexEntry{
		{Path: "i/cur", End: emitSLOCurStart.Add(30 * time.Minute)},
	}
	previous := []indexEntry{
		{Path: "i/prev", End: emitSLOPrevStart.Add(11 * time.Hour)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.emitSLO("acme", current, previous, emitSLONow, emitSLOSlo)

	require.InDelta(t, 0.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, 0.0, gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001)
	require.InDelta(t, 0.0, gaugeValue(m.prePreviousUnconsolidated, "acme"), 0.001)
	require.InDelta(t, 1.0, gaugeValue2(m.indexesPerTenantWindow, "acme", "current"), 0.001)
	require.InDelta(t, 1.0, gaugeValue2(m.indexesPerTenantWindow, "acme", "previous"), 0.001)
}

func TestEmitSLO_BacklogInPreviousWindow(t *testing.T) {
	current := []indexEntry{
		{Path: "i/cur", End: emitSLOCurStart.Add(30 * time.Minute)},
	}
	// threshold = 10:00. prev-a (09:00) old, prev-b (10:00) NOT old (strict <),
	// prev-c (09:30) old. Old count = 2; backlog contribution = max(0, 2-1) = 1.
	// Oldest old End = 09:00; age = 13:00 - 09:00 = 4h.
	previous := []indexEntry{
		{Path: "i/prev-a", End: emitSLOPrevStart.Add(9 * time.Hour)},
		{Path: "i/prev-b", End: emitSLOPrevStart.Add(10 * time.Hour)},
		{Path: "i/prev-c", End: emitSLOPrevStart.Add(9*time.Hour + 30*time.Minute)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.emitSLO("acme", current, previous, emitSLONow, emitSLOSlo)

	require.InDelta(t, 1.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, (4 * time.Hour).Seconds(),
		gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001)
	require.InDelta(t, 1.0, gaugeValue2(m.indexesPerTenantWindow, "acme", "current"), 0.001)
	require.InDelta(t, 3.0, gaugeValue2(m.indexesPerTenantWindow, "acme", "previous"), 0.001)
}

func TestEmitSLO_BacklogInCurrentWindow(t *testing.T) {
	now := time.Date(2026, 5, 14, 18, 0, 0, 0, time.UTC)
	cur := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	// threshold = 15:00. cur-a (13:00) old & oldest, cur-b (14:00) old, cur-c (17:30) fresh.
	current := []indexEntry{
		{Path: "i/cur-a", End: cur.Add(1 * time.Hour)},
		{Path: "i/cur-b", End: cur.Add(2 * time.Hour)},
		{Path: "i/cur-c", End: cur.Add(5*time.Hour + 30*time.Minute)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.emitSLO("acme", current, nil, now, 3*time.Hour)

	require.InDelta(t, 1.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	require.InDelta(t, (5 * time.Hour).Seconds(),
		gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001)
	require.InDelta(t, 3.0, gaugeValue2(m.indexesPerTenantWindow, "acme", "current"), 0.001)
	require.InDelta(t, 0.0, gaugeValue2(m.indexesPerTenantWindow, "acme", "previous"), 0.001)
}

// TestEmitSLO_BaselineEntryDoesNotInflateOldestAge pins that a window holding
// a single old entry (the converged covering index) does not contribute to
// the oldest_backlog_log_age_seconds metric, even when the other window has
// a real backlog. The baseline entry is discounted from the backlog formula
// (`max(0, oldCount-1) == 0`), so it must also be excluded from the age
// calculation.
func TestEmitSLO_BaselineEntryDoesNotInflateOldestAge(t *testing.T) {
	// Current window's single entry is OLD (e.g., a long-sealed previous
	// cycle's covering index). It's the baseline; backlog contribution = 0.
	// Threshold at 10:00 (now=13:00, slo=3h).
	current := []indexEntry{
		// End = -5h ⇒ 5h before threshold (very old, but baseline).
		{Path: "i/cur-baseline", End: emitSLONow.Add(-5 * time.Hour)},
	}
	// Previous window has a real backlog: 3 old entries, oldest at 4h ago
	// (1h before threshold).
	previous := []indexEntry{
		{Path: "i/prev-a", End: emitSLONow.Add(-4 * time.Hour)},
		{Path: "i/prev-b", End: emitSLONow.Add(-3*time.Hour - 30*time.Minute)},
		{Path: "i/prev-c", End: emitSLONow.Add(-3*time.Hour - 15*time.Minute)},
	}
	m := newCoordinatorMetrics(prometheus.NewRegistry())
	m.emitSLO("acme", current, previous, emitSLONow, emitSLOSlo)

	// backlog = max(0,1-1) + max(0,3-1) = 0 + 2 = 2 (all from previous).
	require.InDelta(t, 2.0, gaugeValue(m.unconsolidatedBacklog, "acme"), 0.001)
	// oldestAge must reflect the previous window's oldest (4h), NOT the
	// current window's baseline (5h).
	require.InDelta(t, (4 * time.Hour).Seconds(),
		gaugeValue(m.oldestBacklogLogAgeSeconds, "acme"), 0.001,
		"current-window single old entry is the baseline; it must NOT inflate oldestAge")
}

func TestEmitSLO_IndexesPerTenantWindow_AlwaysExactlyTwoSeriesPerTenant(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newCoordinatorMetrics(reg)
	for _, tenant := range []string{"a", "b", "c"} {
		m.emitSLO(tenant,
			[]indexEntry{{End: emitSLOCurStart.Add(time.Hour)}},
			[]indexEntry{{End: emitSLOPrevStart.Add(time.Hour)}},
			emitSLONow, emitSLOSlo)
	}
	mfs, err := reg.Gather()
	require.NoError(t, err)
	var ipw int
	for _, mf := range mfs {
		if mf.GetName() == metricsNamespace+"_indexes_per_tenant_window" {
			ipw = len(mf.GetMetric())
		}
	}
	require.Equal(t, 6, ipw, "3 tenants × 2 window_roles = 6 series; no extra roles allowed")
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

func gaugeValue(g *prometheus.GaugeVec, labels ...string) float64 {
	return testutil.ToFloat64(g.WithLabelValues(labels...))
}
func gaugeValue2(g *prometheus.GaugeVec, l1, l2 string) float64 {
	return testutil.ToFloat64(g.WithLabelValues(l1, l2))
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
