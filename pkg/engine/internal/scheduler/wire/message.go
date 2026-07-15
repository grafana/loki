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
	MessageKindTaskResult      // MessageKindTaskResult represents [TaskResultMessage].
	MessageKindStreamBind      // MessageKindStreamBind represents [StreamBindMessage].
	MessageKindStreamData      // MessageKindStreamData represents [StreamDataMessage].
	MessageKindStreamClosed    // MessageKindStreamClosed represents [StreamClosedMessage].
)

var kindNames = [...]string{
	MessageKindInvalid:         "Invalid",
	MessageKindWorkerHello:     "WorkerHello",
	MessageKindWorkerSubscribe: "WorkerSubscribe",
	MessageKindWorkerReady:     "WorkerReady",
	MessageKindTaskAssign:      "TaskAssign",
	MessageKindTaskCancel:      "TaskCancel",
	MessageKindTaskResult:      "TaskResult",
	MessageKindStreamBind:      "StreamBind",
	MessageKindStreamData:      "StreamData",
	MessageKindStreamClosed:    "StreamClosed",
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
		// No fields.
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

		// ClosedSourceIDs identifies source streams that closed before this
		// assignment. It does not contain streams that the task writes to.
		ClosedSourceIDs []ulid.ULID

		// Metadata holds additional metadata about the task.
		Metadata http.Header
	}

	// TaskCancelMessage is sent by the scheduler to a worker when a task is no
	// longer needed, and running that task should be aborted.
	TaskCancelMessage struct {
		ID ulid.ULID // ID of the Task to cancel.
	}

	// TaskResultMessage is sent by the worker to the scheduler with the
	// terminal result of a task.
	TaskResultMessage struct {
		ID     ulid.ULID           // ID of the task that finished.
		Result workflow.TaskResult // Terminal result of the task.
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

	// StreamClosedMessage is sent by the scheduler to a stream receiver after
	// the stream's producer finishes or is cancelled or disconnected.
	StreamClosedMessage struct {
		StreamID ulid.ULID // ID of the stream that closed.
	}
)

// Marker implementations

func (WorkerHelloMessage) isMessage()     {}
func (WorkerSubscribeMessage) isMessage() {}
func (WorkerReadyMessage) isMessage()     {}
func (TaskAssignMessage) isMessage()      {}
func (TaskCancelMessage) isMessage()      {}
func (TaskResultMessage) isMessage()      {}
func (StreamBindMessage) isMessage()      {}
func (StreamDataMessage) isMessage()      {}
func (StreamClosedMessage) isMessage()    {}

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

// Kind returns [MessageKindTaskResult].
func (TaskResultMessage) Kind() MessageKind { return MessageKindTaskResult }

// Kind returns [MessageKindStreamBind].
func (StreamBindMessage) Kind() MessageKind { return MessageKindStreamBind }

// Kind returns [MessageKindStreamData].
func (StreamDataMessage) Kind() MessageKind { return MessageKindStreamData }

// Kind returns [MessageKindStreamClosed].
func (StreamClosedMessage) Kind() MessageKind { return MessageKindStreamClosed }
