package worker

import (
	"context"
	"errors"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

const (
	assignmentOutcomeAssigned   = "assigned"
	assignmentOutcomeRejected   = "rejected"
	assignmentOutcomeOtherError = "other_error"
)

const (
	assignmentPhaseNewJob        = "new_job"
	assignmentPhaseThreadHandoff = "thread_handoff"
	assignmentPhaseTotal         = "total"
)

const (
	streamOutcomeSent       = "sent"
	streamOutcomeReceived   = "received"
	streamOutcomeTimeout    = "timeout"
	streamOutcomeCanceled   = "canceled"
	streamOutcomeConnClosed = "conn_closed"
	streamOutcomeSendError  = "send_error"
	streamOutcomeOtherError = "other_error"
)

const (
	streamPhaseBindWait     = "bind_wait"
	streamPhaseDial         = "dial"
	streamPhaseLookupSource = "lookup_source"
	streamPhaseRetryBackoff = "retry_backoff"
	streamPhaseSourceWrite  = "source_write"
	streamPhaseTotal        = "total"
)

const (
	messageDirectionSent     = "sent"
	messageDirectionReceived = "received"
)

const (
	messageOutcomeAccepted     = "accepted"
	messageOutcomeTimeout      = "timeout"
	messageOutcomeCanceled     = "canceled"
	messageOutcomeConnClosed   = "conn_closed"
	messageOutcomeSendError    = "send_error"
	messageOutcomeHandlerError = "handler_error"
)

const (
	schedulerConnectionStateConnected = "connected"
)

const (
	reconnectReasonDialError  = "dial_error"
	reconnectReasonConnClosed = "conn_closed"
	reconnectReasonOtherError = "other_error"
)

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

func classifyReceiveOutcome(err error) string {
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
		return messageOutcomeHandlerError
	}
}

func classifyStreamOutcome(err error, success string) string {
	switch {
	case err == nil:
		return success
	case errors.Is(err, context.DeadlineExceeded):
		return streamOutcomeTimeout
	case errors.Is(err, context.Canceled):
		return streamOutcomeCanceled
	case errors.Is(err, wire.ErrConnClosed):
		return streamOutcomeConnClosed
	default:
		return streamOutcomeOtherError
	}
}

type streamDataReceiveObservation struct {
	m     *metrics
	start time.Time
}

func (m *metrics) beginStreamDataReceive() streamDataReceiveObservation {
	return streamDataReceiveObservation{m: m, start: time.Now()}
}

func (o streamDataReceiveObservation) finish(err error) {
	o.m.streamDataReceiveSeconds.WithLabelValues(streamPhaseTotal, classifyStreamOutcome(err, streamOutcomeReceived)).Observe(time.Since(o.start).Seconds())
}

type streamDataReceivePhase struct {
	m     *metrics
	phase string
	start time.Time
}

func (o streamDataReceiveObservation) beginPhase(phase string) streamDataReceivePhase {
	return streamDataReceivePhase{m: o.m, phase: phase, start: time.Now()}
}

func (p streamDataReceivePhase) finish(err error) {
	p.m.streamDataReceiveSeconds.WithLabelValues(p.phase, classifyStreamOutcome(err, streamOutcomeReceived)).Observe(time.Since(p.start).Seconds())
}

func classifyReconnectReason(err error) string {
	if errors.Is(err, wire.ErrConnClosed) {
		return reconnectReasonConnClosed
	}
	return reconnectReasonOtherError
}
