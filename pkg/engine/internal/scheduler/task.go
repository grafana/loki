package scheduler

import (
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// task wraps a [workflow.Task] with its handler.
type task struct {
	inner   *workflow.Task
	handler workflow.TaskEventHandler

	owner  *wire.Peer
	status workflow.TaskStatus
}

var validTaskTransitions = map[workflow.TaskState][]workflow.TaskState{
	workflow.TaskStateCreated: {workflow.TaskStatePending, workflow.TaskStateRunning, workflow.TaskStateCancelled},
	workflow.TaskStatePending: {workflow.TaskStateRunning, workflow.TaskStateCancelled, workflow.TaskStateFailed},
	workflow.TaskStateRunning: {workflow.TaskStateCompleted, workflow.TaskStateCancelled, workflow.TaskStateFailed},

	workflow.TaskStateCompleted: {}, // Terminal state, can't transition
	workflow.TaskStateCancelled: {}, // Terminal state, can't transition
	workflow.TaskStateFailed:    {}, // Terminal state, can't transition
}

// setState updates the state of the task. setState returns an error if the
// transition is invalid.
//
// Returns true if the state was updated, false otherwise (such as if the task
// is already in the desired state).
func (t *task) setState(newStatus workflow.TaskStatus) (bool, error) {
	oldState, newState := t.status.State, newStatus.State

	if newState == oldState {
		return false, nil
	}

	validStates := validTaskTransitions[oldState]
	if !slices.Contains(validStates, newState) {
		return false, fmt.Errorf("invalid state transition from %s to %s", oldState, newState)
	}

	t.status = newStatus
	return true, nil
}
