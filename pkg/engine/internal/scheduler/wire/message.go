package wire

import (
	"fmt"
	"net"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// MessageKind represents the type of a message.
type MessageKind int

const (
	// MessageKindInvalid represents an invalid message.
	MessageKindInvalid MessageKind = iota

	MessageKindWorkerReady  // MessageKindWorkerReady represents [WorkerReadyMessage].
	MessageKindTaskAssign   // MessageKindTaskAssign represents [TaskAssignMessage].
	MessageKindTaskCancel   // MessageKindTaskCancel represents [TaskCancelMessage].
	MessageKindTaskFlag     // MessageKindTaskFlag represents [TaskFlagMessage].
	MessageKindTaskStatus   // MessageKindTaskStatus represents [TaskStatusMessage].
	MessageKindStreamBind   // MessageKindStreamBind represents [StreamBindMessage].
	MessageKindStreamData   // MessageKindStreamData represents [StreamDataMessage].
	MessageKindStreamStatus // MessageKindStreamStatus represents [StreamStatusMessage].
)

var kindNames = [...]string{
	MessageKindInvalid:      "Invalid",
	MessageKindWorkerReady:  "WorkerReady",
	MessageKindTaskAssign:   "TaskAssign",
	MessageKindTaskCancel:   "TaskCancel",
	MessageKindTaskFlag:     "TaskFlag",
	MessageKindTaskStatus:   "TaskStatus",
	MessageKindStreamBind:   "StreamBind",
	MessageKindStreamData:   "StreamData",
	MessageKindStreamStatus: "StreamStatus",
}

// String returns a string representation of k.
func (k MessageKind) String() string {
	if k < MessageKindInvalid || int(k) >= len(kindNames) {
		return fmt.Sprintf("Kind(%d)", k)
	}

	name := kindNames[k]
	if name == "" {
		return fmt.Sprintf("Kind(%d)", k)
	}
	return name
}

// A Message is a message exchanged between peers.
type Message interface {
	isMessage()

	// Kind returns the kind of message.
	Kind() MessageKind
}

// Messages about workers.
type (
	// WorkerReadyMessage is sent by a worker to the scheduler to request a new
	// task to run. Ready workers are eventually assigned a task via
	// [TaskAssignMessage].
	//
	// Workers may send multiple WorkerReadyMessage messages to request more
	// tasks. Workers are automatically unmarked as ready once each
	// [WorkerReadyMessage] has been responded to with a [TaskAssignMessage].
	WorkerReadyMessage struct {
		// No fields.
	}
)

// Messages about tasks.
type (
	// TaskAssignMessage is sent by the scheduler to a worker when there is a
	// task to run. TaskAssignMessage is only sent to workers for which there is
	// still at least one [WorkerReadyMessage].
	TaskAssignMessage struct {
		Task *workflow.Task // Task to run.

		// StreamStates holds the most recent state of each stream that the task
		// reads from.
		//
		// StreamStates does not have any entries for streams that the task
		// writes to.
		StreamStates map[ulid.ULID]workflow.StreamState
	}

	// TaskCancelMessage is sent by the scheduler to a worker when a task is no
	// longer needed, and running that task should be aborted.
	TaskCancelMessage struct {
		ID ulid.ULID // ID of the Task to cancel.
	}

	// TaskFlagMessage is sent by the scheduler to update the runtime flags of a task.
	TaskFlagMessage struct {
		ID ulid.ULID // ID of the Task to update.

		// Interruptible indicates that tasks blocked on writing or reading to a
		// [Stream] can be paused, and that worker can accept new tasks to run.
		// Tasks are not interruptible by default.
		Interruptible bool
	}

	// TaskStatusMessage is sent by the worker to the scheduler to inform the
	// scheduler of the current status of a task.
	TaskStatusMessage struct {
		ID     ulid.ULID           // ID of the Task to update.
		Status workflow.TaskStatus // Current status of the task.
	}
)

// Messages about streams.
type (
	// StreamBindMessage is sent by the scheduler to a worker to inform the
	// worker about the location of a stream receiver.
	StreamBindMessage struct {
		StreamID ulid.ULID // ID of the stream.
		Receiver net.Addr  // Address of the stream receiver.
	}

	// StreamDataMessage is sent by a worker to a stream receiver to provide
	// payload data for a stream.
	StreamDataMessage struct {
		StreamID ulid.ULID    // ID of the stream.
		Data     arrow.Record // Payload data for the stream.
	}

	// StreamStatusMessage communicates the status of the sending side of a
	// stream. It is sent in two cases:
	//
	// - By the sender of the stream, to inform the scheduler about the status
	//   of that stream.
	//
	// - By the scheduler, to inform the stream reader about the status of the
	//   stream.
	//
	// The scheduler is responsible for informing stream receivers about stream
	// status to avoid keeping streams alive if the sender disconnects.
	StreamStatusMessage struct {
		StreamID ulid.ULID            // ID of the stream.
		State    workflow.StreamState // State of the stream.
	}
)

// Marker implementations

func (WorkerReadyMessage) isMessage()  {}
func (TaskAssignMessage) isMessage()   {}
func (TaskCancelMessage) isMessage()   {}
func (TaskFlagMessage) isMessage()     {}
func (TaskStatusMessage) isMessage()   {}
func (StreamBindMessage) isMessage()   {}
func (StreamDataMessage) isMessage()   {}
func (StreamStatusMessage) isMessage() {}

// Kinds

// Kind returns [MessageKindWorkerReady].
func (WorkerReadyMessage) Kind() MessageKind { return MessageKindWorkerReady }

// Kind returns [MessageKindTaskAssign].
func (TaskAssignMessage) Kind() MessageKind { return MessageKindTaskAssign }

// Kind returns [MessageKindTaskCancel].
func (TaskCancelMessage) Kind() MessageKind { return MessageKindTaskCancel }

// Kind returns [MessageKindTaskFlag].
func (TaskFlagMessage) Kind() MessageKind { return MessageKindTaskFlag }

// Kind returns [MessageKindTaskStatus].
func (TaskStatusMessage) Kind() MessageKind { return MessageKindTaskStatus }

// Kind returns [MessageKindStreamBind].
func (StreamBindMessage) Kind() MessageKind { return MessageKindStreamBind }

// Kind returns [MessageKindStreamData].
func (StreamDataMessage) Kind() MessageKind { return MessageKindStreamData }

// Kind returns [MessageKindStreamStatus].
func (StreamStatusMessage) Kind() MessageKind { return MessageKindStreamStatus }
