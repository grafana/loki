package scheduler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

const (
	assignmentOutcomeAssigned     = "assigned"
	assignmentOutcomeRejected     = "rejected"
	assignmentOutcomeTimeout      = "timeout"
	assignmentOutcomeCanceled     = "canceled"
	assignmentOutcomeConnClosed   = "conn_closed"
	assignmentOutcomeUnassignable = "unassignable"
	assignmentOutcomeSendError    = "send_error"
	assignmentOutcomeOtherError   = "other_error"
)

const (
	taskStatusOutcomeAccepted     = "accepted"
	taskStatusOutcomeStale        = "stale"
	taskStatusOutcomeHandlerError = "handler_error"
)

const (
	taskStatusPhaseTotal = "total"
)

const (
	taskStatusStateClassRunning  = "running"
	taskStatusStateClassTerminal = "terminal"
	taskStatusStateClassOther    = "other"
)

const (
	taskStatusStaleTaskNotFound      = "task_not_found"
	taskStatusStaleInvalidTransition = "invalid_transition"
	taskStatusStaleDuplicateNoop     = "duplicate_noop"
)

const (
	messageOutcomeAccepted   = "accepted"
	messageOutcomeTimeout    = "timeout"
	messageOutcomeCanceled   = "canceled"
	messageOutcomeConnClosed = "conn_closed"
	messageOutcomeSendError  = "send_error"
)

func observeTaskAssignment(m *metrics, outcome string, d time.Duration) {
	m.taskAssignmentAttemptsTotal.WithLabelValues(outcome).Inc()
	if d > 0 {
		m.taskAssignmentSendSeconds.WithLabelValues(outcome).Observe(d.Seconds())
	}
}

func classifyTaskAssignmentOutcome(err error) string {
	switch {
	case err == nil:
		return assignmentOutcomeAssigned
	case errors.Is(err, errUnassignable):
		return assignmentOutcomeUnassignable
	case isTooManyRequestsError(err):
		return assignmentOutcomeRejected
	case errors.Is(err, context.DeadlineExceeded):
		return assignmentOutcomeTimeout
	case errors.Is(err, context.Canceled):
		return assignmentOutcomeCanceled
	case errors.Is(err, wire.ErrConnClosed):
		return assignmentOutcomeConnClosed
	}

	var wireError *wire.Error
	if errors.As(err, &wireError) {
		return assignmentOutcomeOtherError
	}
	return assignmentOutcomeSendError
}

func classifyMessageOutcome(err error) string {
	switch {
	case err == nil:
		return messageOutcomeAccepted
	case errors.Is(err, context.DeadlineExceeded):
		return messageOutcomeTimeout
	case errors.Is(err, context.Canceled):
		return messageOutcomeCanceled
	case errors.Is(err, wire.ErrConnClosed):
		return messageOutcomeConnClosed
	default:
		return messageOutcomeSendError
	}
}

func taskStatusStateClass(state workflow.TaskState) string {
	switch {
	case state == workflow.TaskStateRunning:
		return taskStatusStateClassRunning
	case state.Terminal():
		return taskStatusStateClassTerminal
	default:
		return taskStatusStateClassOther
	}
}

func isWireStatus(err error, status int) bool {
	var wireError *wire.Error
	return errors.As(err, &wireError) && wireError.Code == int32(status)
}

func isTooManyRequestsError(err error) bool {
	return isWireStatus(err, http.StatusTooManyRequests)
}
