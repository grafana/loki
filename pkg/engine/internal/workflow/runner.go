package workflow

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// A Runner can asynchronously execute a workflow.
type Runner interface {
	// AddStreams registers a list of Streams that can be used by Tasks.
	// AddStreams returns an error if any of the streams (by ID) are already
	// registered.
	//
	// The provided handler will be called whenever any of the provided streams
	// change state.
	AddStreams(ctx context.Context, handler StreamEventHandler, streams ...*Stream) error

	// RemoveStreams removes a list of Streams that can be used by Tasks. The
	// associated [StreamEventHandler] will no longer be called for removed
	// streams.
	//
	// RemoveStreams returns an error if there are active tasks using the
	// streams.
	RemoveStreams(ctx context.Context, streams ...*Stream) error

	// Listen binds the caller as the receiver of the specified stream.
	// Listening on a stream prevents tasks from reading from it.
	Listen(ctx context.Context, stream *Stream) (executor.Pipeline, error)

	// Start begins executing the provided tasks in the background. Start
	// returns an error if any of the Tasks references an unregistered Stream,
	// or if any of the tasks are a reader of a stream that's already bound.
	//
	// The provided handler will be called whenever any of the provided tasks
	// change state.
	//
	// Implementations must track executed tasks until the tasks enter a
	// terminal state.
	//
	// The provided context is used for the lifetime of the Start call. To
	// cancel started tasks, use [Runner.Cancel].
	Start(ctx context.Context, handler TaskEventHandler, tasks ...*Task) error

	// Cancel requests cancellation of the specified tasks. Cancel returns an
	// error if any of the tasks were not found.
	Cancel(ctx context.Context, tasks ...*Task) error
}

// StreamEventHandler is a function that handles events for changed streams.
type StreamEventHandler func(ctx context.Context, s *Stream, newState StreamState)

// StreamState represents the state of a stream. It is sent as an event by a
// [Runner] whenever a stream associated with a task changes its state.
//
// The zero value of StreamState is an inactive stream.
type StreamState int

const (
	// StreamStateIdle represents a stream that is waiting for both the sender
	// and receiver to be available.
	StreamStateIdle StreamState = iota

	// StreamStateOpen represents a stream that is open and transmitting data.
	StreamStateOpen

	// StreamStateBlocked represents a stream that is blocked (by backpressure)
	// on sending data.
	StreamStateBlocked

	// StreamStateClosed represents a stream that is closed and no longer
	// transmitting data.
	StreamStateClosed
)

var streamStates = [...]string{
	"Idle",
	"Open",
	"Blocked",
	"Closed",
}

// String returns a string representation of the StreamState.
func (s StreamState) String() string {
	if s >= 0 && int(s) < len(streamStates) {
		return streamStates[s]
	}
	return fmt.Sprintf("StreamState(%d)", s)
}

// TaskEventHandler is a function that handles events for changed tasks.
type TaskEventHandler func(ctx context.Context, t *Task, newStatus TaskStatus)

// TaskStatus holds the state of a task and additional information about that
// state.
type TaskStatus struct {
	State TaskState

	// Error holds the error that occurred during task execution, if any. Only
	// set when State is [TaskStateFailed].
	Error error

	// Statistics report analytics about the lifetime of a task. Only set
	// for terminal task states (see [TaskState.Terminal]).
	Statistics *stats.Result
}

// TaskState represents the state of a Task. It is sent as an event by a
// [Runner] whenever a Task associated with a task changes its state.
type TaskState int

const (
	// TaskStateCreated represents the initial state for a Task, where it has
	// been created but not given to a [Runner].
	TaskStateCreated TaskState = iota

	// TaskStatePending represents a Task that is pending execution by a
	// [Runner].
	TaskStatePending

	// TaskStateRunning represents a Task that is currently being executed by a
	// [Runner].
	TaskStateRunning

	// TaskStateCompleted represents a Task that has completed successfully.
	TaskStateCompleted

	// TaskStateCancelled represents a Task that has been cancelled, either by a
	// [Runner] or the owning [Workflow].
	TaskStateCancelled

	// TaskStateFailed represents a Task that has failed during execution.
	TaskStateFailed
)

var TaskStates = [...]string{
	"Created",
	"Pending",
	"Running",
	"Completed",
	"Cancelled",
	"Failed",
}

// Terminal returns true if the TaskState is terminal.
func (s TaskState) Terminal() bool {
	return s == TaskStateCompleted || s == TaskStateCancelled || s == TaskStateFailed
}

// String returns a string representation of the TaskState.
func (s TaskState) String() string {
	if s >= 0 && int(s) < len(TaskStates) {
		return TaskStates[s]
	}
	return fmt.Sprintf("TaskState(%d)", s)
}
