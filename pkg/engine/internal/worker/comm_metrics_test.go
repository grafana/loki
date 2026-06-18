package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

func TestClassifyStreamOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "success", err: nil, want: streamOutcomeSent},
		{name: "timeout", err: context.DeadlineExceeded, want: streamOutcomeTimeout},
		{name: "canceled", err: context.Canceled, want: streamOutcomeCanceled},
		{name: "connection closed", err: wire.ErrConnClosed, want: streamOutcomeConnClosed},
		{name: "other error", err: errors.New("failed"), want: streamOutcomeOtherError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyStreamOutcome(tt.err, streamOutcomeSent))
		})
	}
}

func TestClassifyMessageOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "accepted", err: nil, want: messageOutcomeAccepted},
		{name: "timeout", err: context.DeadlineExceeded, want: messageOutcomeTimeout},
		{name: "canceled", err: context.Canceled, want: messageOutcomeCanceled},
		{name: "connection closed", err: wire.ErrConnClosed, want: messageOutcomeConnClosed},
		{name: "send error", err: errors.New("failed"), want: messageOutcomeSendError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyMessageOutcome(tt.err))
		})
	}
}

func TestClassifyReceiveOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "accepted", err: nil, want: messageOutcomeAccepted},
		{name: "timeout", err: context.DeadlineExceeded, want: messageOutcomeTimeout},
		{name: "canceled", err: context.Canceled, want: messageOutcomeCanceled},
		{name: "connection closed", err: wire.ErrConnClosed, want: messageOutcomeConnClosed},
		{name: "handler error", err: errors.New("failed"), want: messageOutcomeHandlerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyReceiveOutcome(tt.err))
		})
	}
}
