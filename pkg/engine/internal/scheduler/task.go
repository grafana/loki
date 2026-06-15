package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/queue/fair"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var errUnassignable = fmt.Errorf("task is in a terminal state or has been interrupted")

// task wraps a [workflow.Task] with its handler and scheduler-side state.
type task struct {
	createTime time.Time // Time when task was created.
	inner      *workflow.Task
	handler    workflow.TaskEventHandler
	scope      fair.Scope // Queue scope this task belongs to.

	mut         sync.RWMutex
	status      workflow.TaskStatus
	queueTime   time.Time // Time when task was enqueued.
	assignTime  time.Time // Time when task was assigned to a worker.
	interrupted bool      // Cancellation requested but not yet confirmed.
	requeues    int       // Number of times the task was requeued after a failed assignment.

	// capture holds individual task information from any source:
	//
	//   - The scheduler records its own per-task observations (e.g., timing
	//     between state transitions) into capture via region.
	//   - Worker-supplied captures are merged into this capture when their
	//     TaskStatusMessages arrive, so workflow consumers see a single
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

var validTaskTransitions = map[workflow.TaskState][]workflow.TaskState{
	workflow.TaskStateCreated: {workflow.TaskStatePending, workflow.TaskStateRunning, workflow.TaskStateCancelled},
	workflow.TaskStatePending: {workflow.TaskStateRunning, workflow.TaskStateCancelled, workflow.TaskStateFailed},
	workflow.TaskStateRunning: {workflow.TaskStateCompleted, workflow.TaskStateCancelled, workflow.TaskStateFailed},

	workflow.TaskStateCompleted: {}, // Terminal state, can't transition
	workflow.TaskStateCancelled: {}, // Terminal state, can't transition
	workflow.TaskStateFailed:    {}, // Terminal state, can't transition
}

// State returns the task's current [workflow.TaskState].
func (t *task) State() workflow.TaskState {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.status.State
}

// Status returns a snapshot of the task's current [workflow.TaskStatus].
// The returned status's Capture field is shared by reference; callers should
// not mutate it.
func (t *task) Status() workflow.TaskStatus {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.status
}

// Interrupted reports whether [Scheduler.Cancel] has requested cancellation
// of this task while it was assigned to a worker. See [task.MarkInterrupted].
func (t *task) Interrupted() bool {
	t.mut.RLock()
	defer t.mut.RUnlock()
	return t.interrupted
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

// SetState updates the state of the task. SetState returns an error if the
// transition is invalid.
//
// Returns true if the state was updated, false otherwise (such as if the task
// is already in the desired state).
func (t *task) SetState(m *metrics, newStatus workflow.TaskStatus) (bool, error) {
	t.mut.Lock()
	defer t.mut.Unlock()
	return t.setStateLocked(m, newStatus)
}

// setStateLocked implements the logic of [SetState]. Callers must hold t.mut.
func (t *task) setStateLocked(m *metrics, newStatus workflow.TaskStatus) (bool, error) {
	oldState, newState := t.status.State, newStatus.State

	switch {
	case newStatus != t.status && newState == oldState:
		// State is the same (so we don't have to validate transitions), but
		// there's a new payload about the status, so we should store it.
		t.status = newStatus
		return true, nil

	case newState == oldState:
		// Status is the exact same, no need to update.
		return false, nil

	default:
		validStates := validTaskTransitions[oldState]
		if !slices.Contains(validStates, newState) {
			return false, fmt.Errorf("invalid state transition from %s to %s", oldState, newState)
		}

		t.status = newStatus
		m.tasksTotal.WithLabelValues(newState.String()).Inc()
		return true, nil
	}
}

// MarkQueued records that the task has been enqueued for assignment by
// setting queueTime to the current time.
func (t *task) MarkQueued() {
	t.mut.Lock()
	defer t.mut.Unlock()
	t.queueTime = time.Now()
}

// MarkRequeued records that the task was requeued after a failed assignment
// attempt, incrementing its assignment retry count.
func (t *task) MarkRequeued() {
	t.mut.Lock()
	defer t.mut.Unlock()
	t.requeues++
}

// MarkInterrupted records that cancellation has been requested for this task.
func (t *task) MarkInterrupted() bool {
	t.mut.Lock()
	defer t.mut.Unlock()

	if t.interrupted {
		return false
	}
	t.interrupted = true
	return true
}

// TryAssign calls doAssign, holding a mutex on the task to prevent concurrent
// modification to t's state.
//
// If doAssign returns nil, the assign time is recorded and TryAssign returns
// nil. Otherwise, TryAssign returns the error from doAssign without making any
// changes.
//
// If the task is in a terminal state or has been interrupted, TryAssign
// returns [errUnassignable] without calling doAssign.
func (t *task) TryAssign(doAssign func() error) error {
	t.mut.Lock()
	defer t.mut.Unlock()

	if t.status.State.Terminal() || t.interrupted {
		return errUnassignable
	}

	if err := doAssign(); err != nil {
		return err
	}

	t.assignTime = time.Now()
	return nil
}

// RecordTerminalObservations records the scheduler-side per-task duration
// observations for a task that has just reached a terminal state and ends
// the scheduler region within the task's capture.
//
// The recorded durations partition the task's total lifetime:
//
//   - [schedulerstat.TaskQueueDuration] is recorded for any task that was
//     enqueued. It spans from enqueue until assignment, or until the terminal
//     state if the task was never assigned. Recording it here, rather than in
//     [finalizeAssignment] to ensure we record it before the region ends for
//     tasks that reach terminal state even before assignment finalization.
//
//   - [schedulerstat.TaskExecutionDuration] is recorded for any task that
//     was assigned to a worker.
//
//   - [schedulerstat.TaskTotalDuration] is always recorded.
//
// It also records [schedulerstat.TaskFinishTime], the absolute terminal
// timestamp, which the workflow uses to approximate the query's critical path.
//
// RecordTerminalObservations must only be called once per task and only when
// the task has reached a terminal state.
func (t *task) RecordTerminalObservations(now time.Time) {
	t.mut.RLock()
	queueTime, assignTime, requeues := t.queueTime, t.assignTime, t.requeues
	t.mut.RUnlock()

	t.region.Record(schedulerstat.TaskAssignmentRetries.Observe(int64(requeues)))

	if !queueTime.IsZero() {
		// Queue time ends at assignment, or at the terminal state if the task
		// was never assigned.
		queueEnd := now
		if !assignTime.IsZero() {
			queueEnd = assignTime
		}
		t.region.Record(schedulerstat.TaskQueueDuration.Observe(queueEnd.Sub(queueTime).Nanoseconds()))
	}
	if !assignTime.IsZero() {
		t.region.Record(schedulerstat.TaskExecutionDuration.Observe(now.Sub(assignTime).Nanoseconds()))
	}
	t.region.Record(schedulerstat.TaskTotalDuration.Observe(now.Sub(t.createTime).Nanoseconds()))
	t.region.Record(schedulerstat.TaskFinishTime.Observe(now.UnixNano()))
	t.region.End()
}
