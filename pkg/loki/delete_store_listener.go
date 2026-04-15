package loki

import (
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
)

func deleteRequestsStoreListener(d deletion.DeleteRequestsClient) *listener {
	return &listener{d}
}

type listener struct {
	deleteRequestsClient deletion.DeleteRequestsClient
}

// Starting is called when the service transitions from NEW to STARTING.
func (l *listener) Starting() {}

// Running is called when the service transitions from STARTING to RUNNING.
func (l *listener) Running() {}

// Stopping is called when the service transitions to the STOPPING state.
func (l *listener) Stopping(from services.State) {
	if from == services.Stopping || from == services.Terminated || from == services.Failed {
		// no need to do anything
		return
	}
	l.deleteRequestsClient.Stop()
}

// Terminated is called when the service transitions to the TERMINATED state.
func (l *listener) Terminated(from services.State) {
	if from == services.Stopping || from == services.Terminated || from == services.Failed {
		// no need to do anything
		return
	}
	l.deleteRequestsClient.Stop()
}

// Failed is called when the service transitions to the FAILED state.
func (l *listener) Failed(from services.State, _ error) {
	if from == services.Stopping || from == services.Terminated || from == services.Failed {
		// no need to do anything
		return
	}
	l.deleteRequestsClient.Stop()
}
