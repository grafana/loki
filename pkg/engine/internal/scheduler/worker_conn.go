package scheduler

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// connectionType represents the purpose of a worker's connection.
type connectionType int

const (
	// connectionTypeInitial represents a new connection from a worker with
	// unknown purpose.
	connectionTypeInitial connectionType = iota

	// connectionTypeControlPlane represents a connection from a worker where
	// the connection will be used to assign tasks and communicate state.
	//
	// The connection type is set to connectionTypeControlPlane when the worker
	// sends a [wire.WorkerHelloMessage].
	connectionTypeControlPlane

	// connectionTypeDataPlane represents a connection from a worker where
	// the connection will be used to communicate stream data.
	//
	// The connection type is set to connectionTypeDataPlane when the worker
	// sends a [wire.WorkerDataPlaneMessage].
	connectionTypeDataPlane
)

var connectionTypeNames = [...]string{
	connectionTypeInitial:      "initial",
	connectionTypeControlPlane: "control-plane",
	connectionTypeDataPlane:    "data-plane",
}

// String returns the string representation of the connection type.
func (t connectionType) String() string {
	if t < connectionTypeInitial || int(t) >= len(connectionTypeNames) {
		return fmt.Sprintf("connectionType(%d)", t)
	}
	name := connectionTypeNames[t]
	if name == "" {
		return fmt.Sprintf("connectionType(%d)", t)
	}
	return name
}

// A workerConn represents a connection to a worker.
type workerConn struct {
	// Peer connection to the worker.
	*wire.Peer

	// ctx is scoped to the lifetime of the connection. Long-lived goroutines
	// started from a message handler (whose own context ends when the handler
	// returns) must run under this instead.
	ctx context.Context

	// mutex of the worker. Protects all fields.
	mut sync.RWMutex

	// ty represents the type of worker connection. Messages sent by the worker
	// that are incompatible with the connection type are rejected.
	ty connectionType

	// tasks hold the collection of tasks currently assigned to the worker.
	tasks map[*task]struct{}

	// closed prevents accepted tasks from being assigned after removeWorker has
	// snapshotted this connection's tasks. closeReason is the connection error,
	// if any.
	closed      bool
	closeReason error

	// done is closed when the worker connection is closed. It is used to signal
	// worker goroutines to exit.
	done chan struct{}

	// wake un-parks the worker's assignment loop after it has parked on a 429.
	// A WorkerReady received while the loop is already running nudges this
	// channel to signal that the worker has freed a thread.
	wake chan struct{}
}

// Type returns the type of the worker connection.
func (wc *workerConn) Type() connectionType {
	wc.mut.RLock()
	defer wc.mut.RUnlock()

	return wc.ty
}

// HandleHello handles a WorkerHelloMessage. Returns an error if the worker is
// not in a valid state for a HelloMessage.
//
// After HandleHello is called, the worker connection is marked as a control
// plane connection.
func (wc *workerConn) HandleHello() error {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	if got, want := wc.ty, connectionTypeInitial; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}

	wc.ty = connectionTypeControlPlane
	return nil
}

// MarkReady marks the worker as ready to receive tasks. Returns an error if the
// worker is not a control plane connection, or if the worker is at full
// capacity.
func (wc *workerConn) MarkReady() error {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	if got, want := wc.ty, connectionTypeControlPlane; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}
	return nil
}

// MarkDataPlane marks the worker as a data plane connection. Returns an error
// if the worker is not in a valid state. MarkDataPlane is a no-op if the worker
// is already marked as a data plane connection.
func (wc *workerConn) MarkDataPlane() error {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	switch wc.ty {
	case connectionTypeInitial:
		// Flag the connection as a data plane connection.
		wc.ty = connectionTypeDataPlane
	case connectionTypeControlPlane:
		return fmt.Errorf("workers in state %s can not send stream data messages", wc.ty)
	}

	return nil
}

// MarkClosed marks the worker connection closed and returns a snapshot of its
// assigned tasks. Assign and MarkClosed share the same mutex: either Assign
// installs ownership before this snapshot, or it observes the closed fact and
// refuses ownership.
func (wc *workerConn) MarkClosed(reason error) []*task {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	if !wc.closed {
		wc.closed = true
		wc.closeReason = reason
	}
	return slices.Collect(maps.Keys(wc.tasks))
}

// CloseReason returns the reason the worker connection closed, if any.
func (wc *workerConn) CloseReason() error {
	wc.mut.RLock()
	defer wc.mut.RUnlock()

	return wc.closeReason
}

// Assign assigns a task to the worker. It returns false if the worker connection
// has already closed.
func (wc *workerConn) Assign(assigned *task) bool {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	if wc.closed {
		return false
	}

	assigned.owner = wc

	if wc.tasks == nil {
		wc.tasks = make(map[*task]struct{})
	}
	wc.tasks[assigned] = struct{}{}
	return true
}

// Unassign removes a task from the worker.
func (wc *workerConn) Unassign(assigned *task) {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	delete(wc.tasks, assigned)
}
