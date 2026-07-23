package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/queue/fair"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var errUnassignable = fmt.Errorf("task has a terminal result or cancellation has been requested")

// task wraps a [workflow.Task] with its handler and scheduler-side facts.
type task struct {
	createTime time.Time // Time when task was created.
	inner      *workflow.Task
	handler    workflow.TaskResultHandler
	scope      fair.Scope // Queue scope this task belongs to.

	mut           sync.RWMutex
	result        *workflow.TaskResult
	queued        bool      // Task was submitted to the assignment queue.
	queueTime     time.Time // Time when task was enqueued.
	assignTime    time.Time // Time when assignment was accepted by a worker.
	pendingCancel bool      // Cancellation requested but not yet confirmed.
	requeues      int       // Number of times the task was requeued after a failed assignment.

	// capture holds individual task information from any source:
	//
	//   - The scheduler records its own per-task observations (e.g., queue and
	//     execution timing) into capture via region.
	//   - Worker-supplied captures are merged into this capture when their
	//     TaskResultMessages arrive, so workflow consumers see a single
	//     unified capture per task.
	//
	// capture is distinct from wfRegion: wfRegion is used for workflow-level
	// aggregate counters (e.g., StatPlannedTasks), while capture is the
	// container for per-task data.
	capture *xcap.Capture

	// region is the scheduler-owned region within capture. All scheduler-side
	// per-task observations are recorded against this region. Worker-supplied
	// regions are merged into capture but kept distinct from this one.
	region *xcap.Region

	// Set once by [Scheduler.Start] and read thereafter.
	metadata        http.Header
	wfRegion        *xcap.Region
	runtimeTraceCtx context.Context

	// Protected by [Scheduler.resourcesMut].
	owner *workerConn
}

// Result returns the task's terminal result, if one has been recorded. The
// returned result's Capture field is shared by reference; callers should not
// mutate it.
func (t *task) Result() (workflow.TaskResult, bool) {
	t.mut.RLock()
	defer t.mut.RUnlock()
	if t.result == nil {
		return workflow.TaskResult{}, false
	}
	return *t.result, true
}

// HasResult reports whether the task has a terminal result.
func (t *task) HasResult() bool {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.result != nil
}

// SetResult records the first terminal result for the task. Later duplicate or
// conflicting results do not overwrite the first result.
func (t *task) SetResult(m *metrics, result workflow.TaskResult) (bool, error) {
	if !result.Outcome.Valid() {
		return false, fmt.Errorf("invalid task outcome %s", result.Outcome)
	}

	t.mut.Lock()
	defer t.mut.Unlock()
	if t.result != nil {
		return false, nil
	}

	t.result = &result
	m.taskResultsTotal.WithLabelValues(result.Outcome.String()).Inc()
	return true, nil
}

// PendingCancel reports whether [Scheduler.Cancel] has requested cancellation
// of this task while it was assigned to a worker.
func (t *task) PendingCancel() bool {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.pendingCancel
}

// Queued reports whether the task has been submitted to the assignment queue.
func (t *task) Queued() bool {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.queued
}

// AssignTime returns the time at which [task.TryAssign] successfully sent
// the task's assignment to a worker, or the zero time if no successful send
// has occurred.
func (t *task) AssignTime() time.Time {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.assignTime
}

// QueueTime returns the time at which [task.MarkQueued] was called, or the
// zero time if the task was never queued.
func (t *task) QueueTime() time.Time {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.queueTime
}

// MarkQueued records the first submission of a task to the assignment queue.
// It returns false if the task was already queued or already has a result.
func (t *task) MarkQueued() bool {
	t.mut.Lock()
	defer t.mut.Unlock()
	if t.queued || t.result != nil {
		return false
	}

	t.queued = true
	t.queueTime = time.Now()
	return true
}

// MarkRequeued records that the task was requeued after a failed assignment
// attempt, incrementing its assignment retry count.
func (t *task) MarkRequeued() {
	t.mut.Lock()
	defer t.mut.Unlock()
	t.requeues++
}

// RequestCancel records that cancellation has been requested for this task.
func (t *task) RequestCancel() bool {
	t.mut.Lock()
	defer t.mut.Unlock()

	if t.pendingCancel {
		return false
	}
	t.pendingCancel = true
	return true
}

// TryAssign calls doAssign, holding a mutex on the task to prevent concurrent
// modification to the task's assignment and result facts.
//
// If doAssign returns nil, the assign time is recorded and TryAssign returns
// nil. Otherwise, TryAssign returns the error from doAssign without making any
// changes.
//
// If the task has a terminal result or cancellation has been requested,
// TryAssign returns [errUnassignable] without calling doAssign.
func (t *task) TryAssign(doAssign func() error) error {
	t.mut.Lock()
	defer t.mut.Unlock()

	if t.result != nil || t.pendingCancel {
		return errUnassignable
	}

	if err := doAssign(); err != nil {
		return err
	}

	t.assignTime = time.Now()
	return nil
}

// RecordTerminalObservations records the scheduler-side per-task duration
// observations for a task that has just produced a terminal result and ends
// the scheduler region within the task's capture.
//
// The recorded durations partition the task's total lifetime:
//
//   - [schedulerstat.TaskQueueDuration] is recorded for any task that was
//     enqueued. It spans from enqueue until assignment, or until the terminal
//     result if the task was never assigned. Recording it here, rather than in
//     [finalizeAssignment], ensures it is recorded before the region ends for
//     tasks that produce a terminal result before assignment finalization.
//
//   - [schedulerstat.TaskExecutionDuration] is recorded for any task that
//     was assigned to a worker.
//
//   - [schedulerstat.TaskTotalDuration] is always recorded.
//
// It also records [schedulerstat.TaskFinishTime], the absolute terminal
// timestamp, which the workflow uses to approximate the query's critical path.
//
// RecordTerminalObservations must only be called once per task and only after
// the task has produced a terminal result.
func (t *task) RecordTerminalObservations(now time.Time) {
	t.mut.RLock()
	queueTime, assignTime, requeues := t.queueTime, t.assignTime, t.requeues
	t.mut.RUnlock()

	t.region.Record(schedulerstat.TaskAssignmentRetries.Observe(int64(requeues)))

	if !queueTime.IsZero() {
		t.region.Record(schedulerstat.TaskStagingDuration.Observe(queueTime.Sub(t.createTime).Nanoseconds()))

		// Queue time ends at assignment, or at the terminal result if the task
		// was never assigned.
		queueEnd := now
		if !assignTime.IsZero() {
			queueEnd = assignTime
		}
		t.region.Record(schedulerstat.TaskQueueDuration.Observe(queueEnd.Sub(queueTime).Nanoseconds()))
	} else {
		// For a task that was never enqueued, record the entire duration
		// from creation to terminal result as staging duration.
		t.region.Record(schedulerstat.TaskStagingDuration.Observe(now.Sub(t.createTime).Nanoseconds()))
	}

	if !assignTime.IsZero() {
		t.region.Record(schedulerstat.TaskExecutionDuration.Observe(now.Sub(assignTime).Nanoseconds()))
	}
	t.region.Record(schedulerstat.TaskTotalDuration.Observe(now.Sub(t.createTime).Nanoseconds()))
	t.region.Record(schedulerstat.TaskFinishTime.Observe(now.UnixNano()))
	t.region.End()
}

// releaseTerminalResources drops the scheduler's references to the task's
// large, no-longer-needed state after it has produced a terminal result and
// its result notification has been dispatched.
//
// releaseTerminalResources must only be called with the task already holding a
// terminal result, after the result notification has been queued, while
// [Scheduler.resourcesMut] is held.
func (t *task) releaseTerminalResources() {
	// capture and region are only ever accessed under [Scheduler.resourcesMut],
	// which the caller holds.
	t.capture = nil
	t.region = nil

	// result is guarded by t.mut.
	t.mut.Lock()
	if t.result != nil {
		t.result.Capture = nil
	}
	t.mut.Unlock()
}
