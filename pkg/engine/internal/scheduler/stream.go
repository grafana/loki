package scheduler

import (
	"fmt"
	"slices"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// stream wraps a [workflow.Stream] with its handler and state.
type stream struct {
	inner   *workflow.Stream
	handler workflow.StreamEventHandler

	state workflow.StreamState

	localReceiver workflow.RecordWriter // Local receiver (for root task results)
	taskReceiver  ulid.ULID             // ID of the receiving task.
	taskSender    ulid.ULID             // ID of the sending task.
}

var validStreamTransitions = map[workflow.StreamState][]workflow.StreamState{
	workflow.StreamStateIdle:    {workflow.StreamStateOpen, workflow.StreamStateBlocked, workflow.StreamStateClosed},
	workflow.StreamStateOpen:    {workflow.StreamStateBlocked, workflow.StreamStateClosed},
	workflow.StreamStateBlocked: {workflow.StreamStateOpen, workflow.StreamStateClosed},
	workflow.StreamStateClosed:  {}, // Closed streams cannot transition to any other state.
}

// setState updates the state of the stream. setState returns an error if the
// transition is invalid.
//
// Returns true if the state was updated, false otherwise (such as if the task
// is already in the desired state).
func (s *stream) setState(m *metrics, newState workflow.StreamState) (bool, error) {
	oldState := s.state

	if newState == oldState {
		return false, nil
	}

	validStates := validStreamTransitions[oldState]
	if !slices.Contains(validStates, newState) {
		return false, fmt.Errorf("invalid state transition from %s to %s", oldState, newState)
	}

	s.state = newState
	m.streamsTotal.WithLabelValues(newState.String()).Inc()
	return true, nil
}

// setLocalListener sets the local listener for the stream. Fails if there is
// already a bound listener (local or task).
func (s *stream) setLocalListener(writer workflow.RecordWriter) error {
	if s.localReceiver != nil {
		return fmt.Errorf("stream already bound to scheduler for reads")
	} else if s.taskReceiver != ulid.Zero {
		return fmt.Errorf("stream already bound to task for reads")
	}

	s.localReceiver = writer
	return nil
}

// setTaskReceiver sets the task receiver for the stream. Fails if there is
// already a bound receiver (local or task).
func (s *stream) setTaskReceiver(id ulid.ULID) error {
	if s.localReceiver != nil {
		return fmt.Errorf("stream already bound to scheduler for reads")
	} else if s.taskReceiver != ulid.Zero {
		return fmt.Errorf("stream already bound to task for reads")
	}

	s.taskReceiver = id
	return nil
}

// setTaskSender sets the task sender for the stream. Fails if there is already
// a bound sender.
func (s *stream) setTaskSender(id ulid.ULID) error {
	if s.taskSender != ulid.Zero {
		return fmt.Errorf("stream already bound to task for writes")
	}

	s.taskSender = id
	return nil
}
