package workflow

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/oklog/ulid/v2"

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
	TaskResultHandler  TaskResultHandler  // Handler for terminal task results.
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
	// corresponding resources produce events.
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

	// StreamStateClosed represents a stream that is closed and no longer
	// transmitting data.
	StreamStateClosed
)

var streamStates = [...]string{
	"Idle",
	"Open",
	"Closed",
}

// String returns a string representation of the StreamState.
func (s StreamState) String() string {
	if s >= 0 && int(s) < len(streamStates) {
		return streamStates[s]
	}
	return fmt.Sprintf("StreamState(%d)", s)
}

// TaskResultHandler handles a terminal result for a task.
type TaskResultHandler func(ctx context.Context, t *Task, result TaskResult)

// TaskResult is the terminal result of a task.
type TaskResult struct {
	Outcome TaskOutcome

	// Error holds the error that occurred during task execution, if any. It is
	// only set when Outcome is [TaskOutcomeFailed].
	Error error

	// Capture contains observations about the execution of the task.
	Capture *xcap.Capture
}

// TaskOutcome represents the terminal outcome of a task.
type TaskOutcome int

const (
	// TaskOutcomeInvalid is the zero value of TaskOutcome.
	TaskOutcomeInvalid TaskOutcome = iota

	// TaskOutcomeCompleted means that a task completed successfully.
	TaskOutcomeCompleted

	// TaskOutcomeCancelled means that a task was cancelled.
	TaskOutcomeCancelled

	// TaskOutcomeFailed means that a task failed during execution.
	TaskOutcomeFailed
)

// Valid reports whether o is a terminal task outcome.
func (o TaskOutcome) Valid() bool {
	return o == TaskOutcomeCompleted || o == TaskOutcomeCancelled || o == TaskOutcomeFailed
}

// String returns a string representation of the TaskOutcome.
func (o TaskOutcome) String() string {
	switch o {
	case TaskOutcomeInvalid:
		return "Invalid"
	case TaskOutcomeCompleted:
		return "Completed"
	case TaskOutcomeCancelled:
		return "Cancelled"
	case TaskOutcomeFailed:
		return "Failed"
	default:
		return fmt.Sprintf("TaskOutcome(%d)", o)
	}
}
