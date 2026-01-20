package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// A Manifest is a collection of related Tasks and Streams. A manifest is given
// to a [Runner] before tasks can run.
type Manifest struct {
	ID     ulid.ULID // ID of the manifest.
	Tenant string    // Tenant that this manifest is associated with, if any.
	Actor  []string  // Path to the actor that generated this manifest.

	// Streams are the collection of streams within a manifest.
	Streams []*Stream

	// Tasks are the collection of Tasks within a manifest. Tasks only reference
	// Streams within the same manifest.
	Tasks []*Task

	StreamEventHandler StreamEventHandler // Handler for stream events.
	TaskEventHandler   TaskEventHandler   // Handler for task events.
}

// A RecordWriter is used to write records to a stream.
type RecordWriter interface {
	// Write writes a new record to the stream.
	Write(ctx context.Context, record arrow.RecordBatch) error
}

// A Runner can asynchronously execute a workflow.
type Runner interface {
	// RegisterManifest registers a Manifest to use with the runner. Registering
	// a manifest records all of the streams and tasks inside of it for use.
	//
	// Tasks within the manifest must only reference streams within the same
	// manifest.
	//
	// RegisterManifest returns an error if a stream or task is already
	// associated with a different manifest.
	//
	// The handlers defined by the manifest will be invoked whenever the
	// corresponding resources of the manifest change state.
	RegisterManifest(ctx context.Context, manifest *Manifest) error

	// UnregisterManifest unregisters a Manifest from the runner. Unregistering
	// a manifest forcibly cancels any tasks associated with it.
	//
	// UnregisterManifest returns an error if the manifest is not registered or
	// if it contains unregistered streams or tasks.
	UnregisterManifest(ctx context.Context, manifest *Manifest) error

	// Listen binds the stream to write to the provided writer. Listen returns
	// an error if the stream was not defined in a registered manifest, or if a
	// task is already bound to the stream as a reader.
	Listen(ctx context.Context, writer RecordWriter, stream *Stream) error

	// Start begins executing the provided tasks in the background. Start
	// returns an error if any of the Tasks were not registered through a
	// manifest.
	//
	// The provided context is used for the lifetime of the Start call. To
	// cancel started tasks, use [Runner.Cancel].
	Start(ctx context.Context, tasks ...*Task) error

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

	// Capture contains observations about the execution of the task.
	Capture *xcap.Capture

	// Statistics report analytics about the lifetime of a task. Only set
	// for terminal task states (see [TaskState.Terminal]).
	Statistics *stats.Result

	// ContributingTimeRange of a running task. Only set for non-terminal states.
	ContributingTimeRange ContributingTimeRange
}

// ContributingTimeRange represents a time range of input data that can change the
// current state of a running task. Anything outside of this range can not meaningfully
// contribute to the task state.
type ContributingTimeRange struct {
	// End of the range
	Timestamp time.Time
	// Less than Timestamp
	LessThan bool
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
