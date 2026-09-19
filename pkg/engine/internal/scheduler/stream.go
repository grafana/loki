package scheduler

import (
	"fmt"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// stream wraps a [workflow.Stream] with its handler and scheduler-side facts.
type stream struct {
	inner   *workflow.Stream
	handler workflow.StreamClosedHandler
	closed  bool

	localReceiver workflow.RecordWriter // Local receiver (for root task results)
	taskReceiver  ulid.ULID             // ID of the receiving task.
	taskSender    ulid.ULID             // ID of the sending task.
}

// markClosed records that the stream is closed. It returns false if the stream was
// already closed.
func (s *stream) markClosed(m *metrics) bool {
	if s.closed {
		return false
	}

	s.closed = true
	m.streamClosuresTotal.Inc()
	return true
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
