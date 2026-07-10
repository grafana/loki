package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"go.uber.org/atomic"

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

	// owner is the worker the task is currently assigned to (nil if unassigned).
	// It is an atomic pointer rather than being guarded by resourcesMut so that
	// the per-dispatch finalizeAssignment path — which sets it — does not need
	// the global write lock and can run concurrently with other readers.
	owner atomic.Pointer[workerConn]
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

// TryAssign calls doAssign to send the task's assignment to a worker.
//
// If doAssign returns nil, the assign time is recorded and TryAssign returns
// nil. Otherwise, TryAssign returns the error from doAssign without recording
// the assign time.
//
// If the task is in a terminal state or has been interrupted, TryAssign
// returns [errUnassignable] without calling doAssign.
//
// TryAssign deliberately does NOT hold t.mut for the duration of doAssign.
// doAssign performs a blocking, network-round-trip send to the worker (up to
// several seconds under load), and t.mut is also acquired by hot scheduler
// paths — handleTaskStatus, finalizeAssignment, and Cancel — several of which
// hold the global resourcesMut while calling t.SetState. Holding t.mut across
// the send would therefore pin resourcesMut behind a single worker's send
// latency and starve the dispatcher (prepareAssignment needs resourcesMut),
// stalling all task dispatch.
//
// Releasing t.mut during the send is safe: a task that becomes terminal or
// interrupted between the pre-check and the send is caught by the terminal
// re-check in [Scheduler.finalizeAssignment] (which runs under resourcesMut
// after TryAssign returns and cancels the assignment on the worker), and
// callers already tolerate assignTime being unset when a worker responds
// before the assignment is finalized.
func (t *task) TryAssign(doAssign func() error) error {
	t.mut.RLock()
	unassignable := t.status.State.Terminal() || t.interrupted
	t.mut.RUnlock()

	if unassignable {
		return errUnassignable
	}

	if err := doAssign(); err != nil {
		return err
	}

	t.mut.Lock()
	t.assignTime = time.Now()
	t.mut.Unlock()
	return nil
}

// markAssigned records wc as the task's owner unless the task has already
// reached a terminal state, returning true if the task was marked assigned.
//
// Holding t.mut makes the terminal check and the owner store atomic with
// respect to SetState, so a task that goes terminal concurrently (e.g. a fast
// worker completing it, or a Cancel) is never left owned by a worker. This lets
// finalizeAssignment run under resourcesMut.RLock instead of the global write
// lock.
func (t *task) markAssigned(wc *workerConn) bool {
	t.mut.Lock()
	defer t.mut.Unlock()

	if t.status.State.Terminal() {
		return false
	}
	t.owner.Store(wc)
	return true
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
		t.region.Record(schedulerstat.TaskStagingDuration.Observe(queueTime.Sub(t.createTime).Nanoseconds()))

		// Queue time ends at assignment, or at the terminal state if the task
		// was never assigned.
		queueEnd := now
		if !assignTime.IsZero() {
			queueEnd = assignTime
		}
		t.region.Record(schedulerstat.TaskQueueDuration.Observe(queueEnd.Sub(queueTime).Nanoseconds()))
	} else {
		// For a task that was never enqueued, record the entire duration
		// from creation to terminal state as staging duration.
		t.region.Record(schedulerstat.TaskStagingDuration.Observe(now.Sub(t.createTime).Nanoseconds()))
	}

	if !assignTime.IsZero() {
		t.region.Record(schedulerstat.TaskExecutionDuration.Observe(now.Sub(assignTime).Nanoseconds()))
	}
	t.region.Record(schedulerstat.TaskTotalDuration.Observe(now.Sub(t.createTime).Nanoseconds()))
	t.region.Record(schedulerstat.TaskFinishTime.Observe(now.UnixNano()))
	t.region.End()
}
