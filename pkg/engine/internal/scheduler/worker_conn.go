package scheduler

import (
	"fmt"

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
	maxThreads int

	// tasks hold the collection of tasks currently assigned to the worker.
	tasks map[*task]struct{}
}
