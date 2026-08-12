package worker

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestSlotPhaseTrackerComputeIsLifetimeMinusComm is the utilization invariant:
// comm-blocked is the plain sum of the main goroutine's communication waits, and
// compute is the slot lifetime minus that sum.
func TestSlotPhaseTrackerComputeIsLifetimeMinusComm(t *testing.T) {
	base := time.Unix(0, 0)
	m := newMetrics()
	tracker := newSlotPhaseTracker(m, taskTypeLeaf, base)

	tracker.AddComm(2 * time.Second)
	tracker.AddComm(4 * time.Second)

	tracker.Observe(base.Add(10*time.Second), outcomeSuccess)

	comm := testutil.ToFloat64(m.slotPhaseSeconds.WithLabelValues(slotPhaseComm.String(), taskTypeLeaf.String(), outcomeSuccess.String()))
	compute := testutil.ToFloat64(m.slotPhaseSeconds.WithLabelValues(slotPhaseCompute.String(), taskTypeLeaf.String(), outcomeSuccess.String()))

	require.InDelta(t, 6.0, comm, 1e-9)
	require.InDelta(t, 4.0, compute, 1e-9)
	require.InDelta(t, 10.0, comm+compute, 1e-9)
}

// TestSlotPhaseTrackerComputeNeverNegative ensures comm is clamped to the slot
// lifetime so compute never goes negative even if accumulated comm exceeds it.
func TestSlotPhaseTrackerComputeNeverNegative(t *testing.T) {
	base := time.Unix(0, 0)
	m := newMetrics()
	tracker := newSlotPhaseTracker(m, taskTypeLeaf, base)

	tracker.AddComm(20 * time.Second)

	tracker.Observe(base.Add(5*time.Second), outcomeSuccess)

	comm := testutil.ToFloat64(m.slotPhaseSeconds.WithLabelValues(slotPhaseComm.String(), taskTypeLeaf.String(), outcomeSuccess.String()))
	compute := testutil.ToFloat64(m.slotPhaseSeconds.WithLabelValues(slotPhaseCompute.String(), taskTypeLeaf.String(), outcomeSuccess.String()))

	require.InDelta(t, 5.0, comm, 1e-9)
	require.InDelta(t, 0.0, compute, 1e-9)
}
