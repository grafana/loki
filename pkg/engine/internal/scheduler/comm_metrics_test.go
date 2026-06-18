package scheduler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestClassifyTaskAssignmentOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "assigned", err: nil, want: assignmentOutcomeAssigned},
		{name: "unassignable", err: errUnassignable, want: assignmentOutcomeUnassignable},
		{name: "rejected", err: wire.Errorf(http.StatusTooManyRequests, "busy"), want: assignmentOutcomeRejected},
		{name: "timeout", err: context.DeadlineExceeded, want: assignmentOutcomeTimeout},
		{name: "canceled", err: context.Canceled, want: assignmentOutcomeCanceled},
		{name: "connection closed", err: wire.ErrConnClosed, want: assignmentOutcomeConnClosed},
		{name: "wire error", err: wire.Errorf(http.StatusInternalServerError, "failed"), want: assignmentOutcomeOtherError},
		{name: "send error", err: errors.New("write failed"), want: assignmentOutcomeSendError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyTaskAssignmentOutcome(tt.err))
		})
	}
}

func TestTaskStatusStateClass(t *testing.T) {
	tests := []struct {
		state workflow.TaskState
		want  string
	}{
		{state: workflow.TaskStateRunning, want: taskStatusStateClassRunning},
		{state: workflow.TaskStateCompleted, want: taskStatusStateClassTerminal},
		{state: workflow.TaskStateCancelled, want: taskStatusStateClassTerminal},
		{state: workflow.TaskStateFailed, want: taskStatusStateClassTerminal},
		{state: workflow.TaskStatePending, want: taskStatusStateClassOther},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			require.Equal(t, tt.want, taskStatusStateClass(tt.state))
		})
	}
}
