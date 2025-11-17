package scheduler

import (
	"fmt"
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

	// ty represents the type of worker connection. Messages sent by the worker
	// that are incompatible with the connection type are rejected.
	ty connectionType

	// maxThreads represents the maximum number of threads that can be used by
	// the worker. Only set for control plane connections.
	//
	// len(tasks) + readyThreads must not exceed maxThreads.
	maxThreads int

	mut          sync.RWMutex
	readyThreads int                // readyThreads represents the number of threads that are ready to be assigned a task.
	tasks        map[*task]struct{} // tasks hold the collection of tasks currently assigned to the worker.
}

func (wc *workerConn) trackAssignment(assigned *task) {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	assigned.owner = wc

	if wc.tasks == nil {
		wc.tasks = make(map[*task]struct{})
	}
	wc.tasks[assigned] = struct{}{}

	// Assigning a task removes a ready thread, since len(wc.tasks) increased by
	// 1.
	wc.readyThreads--
}

func (wc *workerConn) untrackAssignment(assigned *task) {
	wc.mut.Lock()
	defer wc.mut.Unlock()

	delete(wc.tasks, assigned)
}

// hasCapacity returns true if the worker has capacity to accept more tasks.
func (wc *workerConn) hasCapacity() bool {
	wc.mut.RLock()
	defer wc.mut.RUnlock()

	return wc.maxThreads > len(wc.tasks)+wc.readyThreads
}
