package loki

import "github.com/grafana/dskit/services"

// stopListener stops a resource when the service it listens to shuts down.
//
// It calls stop once, on the first transition into a shutdown state: Stopping, Terminated or Failed.
// Later transitions do nothing, because some resources panic when stopped twice.
type stopListener struct {
	stop func()
}

func newStopListener(stop func()) *stopListener {
	return &stopListener{stop: stop}
}

func (l *stopListener) Starting() {}
func (l *stopListener) Running()  {}

func (l *stopListener) Stopping(from services.State)        { l.maybeStop(from) }
func (l *stopListener) Terminated(from services.State)      { l.maybeStop(from) }
func (l *stopListener) Failed(from services.State, _ error) { l.maybeStop(from) }

func (l *stopListener) maybeStop(from services.State) {
	if from == services.Stopping || from == services.Terminated || from == services.Failed {
		return
	}
	l.stop()
}
