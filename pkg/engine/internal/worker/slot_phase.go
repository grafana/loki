package worker

import (
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
)

// slotPhaseTracker accumulates, over one worker slot's lifetime, how long the
// main runJob goroutine spent blocked on communication. Because a slot has a
// single serial thread of execution, its communication waits never overlap, so
// a plain sum is exact. At slot end it emits the comm-blocked seconds and the
// remaining compute seconds (total - comm) to the slot_phase metric.
//
// Only the main runJob goroutine mutates a tracker, so it needs no locking.
// Communication that runs on other goroutines (Merge prefetch reads, the async
// bind enqueue on a handler goroutine) is intentionally not counted: while it
// runs, the main goroutine is genuinely computing.
type slotPhaseTracker struct {
	metrics  *metrics
	taskType taskType
	start    time.Time

	commBlocked time.Duration
}

func newSlotPhaseTracker(metrics *metrics, taskType taskType, start time.Time) *slotPhaseTracker {
	return &slotPhaseTracker{metrics: metrics, taskType: taskType, start: start}
}

// AddComm adds d to the slot's comm-blocked total. It is called from the main
// runJob goroutine at each communication wait it incurs: the pipeline reads for
// input and the sends it issues for output and status.
func (t *slotPhaseTracker) AddComm(d time.Duration) {
	if t == nil || d <= 0 {
		return
	}
	t.commBlocked += d
}

// Observe emits the slot's comm-blocked and compute seconds. compute is the slot
// lifetime minus the comm-blocked total and is never negative.
func (t *slotPhaseTracker) Observe(end time.Time, outcome metrictimer.Outcome) {
	if t == nil || t.metrics == nil || end.Before(t.start) {
		return
	}

	total := end.Sub(t.start)
	comm := t.commBlocked
	if comm > total {
		comm = total
	}
	compute := total - comm

	t.metrics.addSlotPhase(slotPhaseComm, t.taskType, outcome, comm)
	t.metrics.addSlotPhase(slotPhaseCompute, t.taskType, outcome, compute)
}
