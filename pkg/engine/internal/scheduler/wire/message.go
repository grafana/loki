package wire

import (
	"fmt"
	"net"
	"net/http"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// MessageKind represents the type of a message.
type MessageKind int

const (
	// MessageKindInvalid represents an invalid message.
	MessageKindInvalid MessageKind = iota

	MessageKindWorkerHello     // MessageKindWorkerHello represents [WorkerHelloMessage].
	MessageKindWorkerSubscribe // MessageKindWorkerSubscribe represents [WorkerSubscribeMessage].
	MessageKindWorkerReady     // MessageKindWorkerReady represents [WorkerReadyMessage].
	MessageKindTaskAssign      // MessageKindTaskAssign represents [TaskAssignMessage].
	MessageKindTaskCancel      // MessageKindTaskCancel represents [TaskCancelMessage].
	MessageKindTaskFlag        // MessageKindTaskFlag represents [TaskFlagMessage].
	MessageKindTaskStatus      // MessageKindTaskStatus represents [TaskStatusMessage].
	MessageKindStreamBind      // MessageKindStreamBind represents [StreamBindMessage].
	MessageKindStreamData      // MessageKindStreamData represents [StreamDataMessage].
	MessageKindStreamStatus    // MessageKindStreamStatus represents [StreamStatusMessage].
)

var kindNames = [...]string{
	MessageKindInvalid:         "Invalid",
	MessageKindWorkerHello:     "WorkerHello",
	MessageKindWorkerSubscribe: "WorkerSubscribe",
	MessageKindWorkerReady:     "WorkerReady",
	MessageKindTaskAssign:      "TaskAssign",
	MessageKindTaskCancel:      "TaskCancel",
	MessageKindTaskFlag:        "TaskFlag",
	MessageKindTaskStatus:      "TaskStatus",
	MessageKindStreamBind:      "StreamBind",
	MessageKindStreamData:      "StreamData",
	MessageKindStreamStatus:    "StreamStatus",
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
	// WorkerHelloMessage is sent by a peer to the scheduler to establish
	// itself as a control plane connection that can run tasks.
	//
	// WorkerHelloMessage must be sent by workers before any other worker
	// messages.
	WorkerHelloMessage struct {
		// Threads is the number of threads the worker has available.
		//
		// The scheduler uses Threads to determine the maximum number of tasks
		// that can be assigned concurrently to a worker.
		Threads int
	}

	// WorkerSubscribeMessage is sent by a scheduler to request a
	// [WorkerReadyMessage] from workers once they have at least one worker
	// thread available.
	//
	// The subscription is cleared once the next [WorkerReadyMessage] is sent.
	WorkerSubscribeMessage struct {
		// No fields.
	}

	// WorkerReadyMessage is sent by a worker to the scheduler to signal that
	// the worker has at least one worker thread available for running tasks.
	//
	// Workers may send WorkerReadyMessage at any time, but one must be sent in
	// response to a [WorkerSubscribeMessage] once at least one worker thread is
	// available.
	WorkerReadyMessage struct {
		// No fields.
	}
)

// Messages about tasks.
type (
	// TaskAssignMessage is sent by the scheduler to a worker when there is a
	// task to run.
	//
	// Workers that have no threads available should reject task assignment with
	// a HTTP 429 Too Many Requests. When this happens, the scheduler will
	// remove the ready state from the worker until it receives a
	// WorkerReadyMessage.
	TaskAssignMessage struct {
		Task *workflow.Task // Task to run.

		// StreamStates holds the most recent state of each stream that the task
		// reads from.
		//
		// StreamStates does not have any entries for streams that the task
		// writes to.
		StreamStates map[ulid.ULID]workflow.StreamState

		// Metadata holds additional metadata about the task.
		Metadata http.Header
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
		StreamID ulid.ULID         // ID of the stream.
		Data     arrow.RecordBatch // Payload data for the stream.
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

func (WorkerHelloMessage) isMessage()     {}
func (WorkerSubscribeMessage) isMessage() {}
func (WorkerReadyMessage) isMessage()     {}
func (TaskAssignMessage) isMessage()      {}
func (TaskCancelMessage) isMessage()      {}
func (TaskFlagMessage) isMessage()        {}
func (TaskStatusMessage) isMessage()      {}
func (StreamBindMessage) isMessage()      {}
func (StreamDataMessage) isMessage()      {}
func (StreamStatusMessage) isMessage()    {}

// Kinds

// Kind returns [MessageKindWorkerHello].
func (WorkerHelloMessage) Kind() MessageKind { return MessageKindWorkerHello }

// Kind returns [MessageKindWorkerSubscribe].
func (WorkerSubscribeMessage) Kind() MessageKind { return MessageKindWorkerSubscribe }

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
