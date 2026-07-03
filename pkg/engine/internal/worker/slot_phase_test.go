package worker

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
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

type slowClosingSink struct{ delay time.Duration }

func (s slowClosingSink) Close(context.Context) error {
	time.Sleep(s.delay)
	return nil
}

// TestCloseSinksCountsCloseSendAsComm is a contract test for the main-goroutine
// sink-close status send: its wait must land in the slot's comm-blocked total,
// not in compute. It fails if closeSinks stops adding the close duration to the
// slot.
func TestCloseSinksCountsCloseSendAsComm(t *testing.T) {
	base := time.Now()
	m := newMetrics()
	tracker := newSlotPhaseTracker(m, taskTypeLeaf, base)

	const closeDelay = 20 * time.Millisecond
	closeSinks(context.Background(), []closableSink{slowClosingSink{delay: closeDelay}}, tracker, log.NewNopLogger())

	tracker.Observe(time.Now(), outcomeSuccess)

	comm := testutil.ToFloat64(m.slotPhaseSeconds.WithLabelValues(slotPhaseComm.String(), taskTypeLeaf.String(), outcomeSuccess.String()))
	// The close send blocked the main goroutine, so comm must include it.
	require.GreaterOrEqual(t, comm, closeDelay.Seconds())
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
