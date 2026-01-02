package scheduler

import (
	"errors"
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

	// mutex of the worker. Protects all fields.
	mut sync.RWMutex

	// ty represents the type of worker connection. Messages sent by the worker
	// that are incompatible with the connection type are rejected.
	ty connectionType

	// tasks hold the collection of tasks currently assigned to the worker.
	tasks map[*task]struct{}
}

// Type returns the type of the worker connection.
func (wc *workerConn) Type() connectionType {
	wc.mut.RLock()
	defer wc.mut.RUnlock()

	return wc.ty
}

// HandleHello handles a WorkerHelloMessage. Returns an error if the worker is
// not in a valid state for a HelloMessage, or if the message is invalid.
//
// After HandleHello is called, the worker connection is marked as a control
// plane connection.
func (wc *workerConn) HandleHello(msg wire.WorkerHelloMessage) error {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	if got, want := wc.ty, connectionTypeInitial; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	} else if msg.Threads <= 0 {
		return errors.New("worker must advertise at least one thread")
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

// Assigned returns a copy of the assigned tasks in an undefined order.
func (wc *workerConn) Assigned() []*task {
	wc.mut.RLock()
	defer wc.mut.RUnlock()

	return slices.Collect(maps.Keys(wc.tasks))
}

// Assign assigns a task to the worker.
func (wc *workerConn) Assign(assigned *task) {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	assigned.owner = wc

	if wc.tasks == nil {
		wc.tasks = make(map[*task]struct{})
	}
	wc.tasks[assigned] = struct{}{}
}

// Unassign removes a task from the worker.
func (wc *workerConn) Unassign(assigned *task) {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	delete(wc.tasks, assigned)
}
