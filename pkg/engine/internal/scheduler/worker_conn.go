package scheduler

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// A workerConn represents a connection to a worker.
type workerConn struct {
	// Peer connection to the worker.
	*wire.Peer

	// tasks hold the collection of tasks currently assigned to the worker.
	tasks map[*task]struct{}
}
