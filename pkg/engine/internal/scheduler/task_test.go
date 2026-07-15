package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestTask_AssignmentRetries(t *testing.T) {
	tests := []struct {
		name     string
		requeues int
	}{
		{name: "no retries", requeues: 0},
		{name: "single retry", requeues: 1},
		{name: "multiple retries", requeues: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captureCtx, capture := xcap.NewCapture(t.Context(), nil)
			_, region := xcap.StartRegion(captureCtx, "scheduler")

			task := &task{
				createTime: time.Now(),
				capture:    capture,
				region:     region,
			}

			for range tc.requeues {
				task.MarkRequeued()
			}

			task.RecordTerminalObservations(time.Now())

			require.Equal(t, int64(tc.requeues), xcap.Value[int64](capture, schedulerstat.TaskAssignmentRetries),
				"assignment retry count should equal the number of requeues")
		})
	}
}

func TestTaskFastResultAfterAssignmentAck(t *testing.T) {
	var testTask task
	metrics := newMetrics()

	assignmentStarted := make(chan struct{})
	allowAck := make(chan struct{})
	assignmentDone := make(chan error, 1)
	go func() {
		assignmentDone <- testTask.TryAssign(func() error {
			close(assignmentStarted)
			<-allowAck
			return nil
		})
	}()
	<-assignmentStarted

	type setResult struct {
		changed bool
		err     error
	}
	resultDone := make(chan setResult, 1)
	go func() {
		changed, err := testTask.SetResult(metrics, workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted})
		resultDone <- setResult{changed: changed, err: err}
	}()

	// The task mutex keeps the result behind the assignment ACK. Once ACKed,
	// TryAssign records acceptance before the result can be observed.
	close(allowAck)
	require.NoError(t, <-assignmentDone)
	set := <-resultDone
	require.NoError(t, set.err)
	require.True(t, set.changed)
	require.False(t, testTask.AssignTime().IsZero())

	result, ok := testTask.Result()
	require.True(t, ok)
	require.Equal(t, workflow.TaskOutcomeCompleted, result.Outcome)
}

func TestTaskFirstResultIsAuthoritative(t *testing.T) {
	var testTask task
	metrics := newMetrics()

	changed, err := testTask.SetResult(metrics, workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted})
	require.NoError(t, err)
	require.True(t, changed)

	changed, err = testTask.SetResult(metrics, workflow.TaskResult{Outcome: workflow.TaskOutcomeFailed})
	require.NoError(t, err)
	require.False(t, changed)

	result, ok := testTask.Result()
	require.True(t, ok)
	require.Equal(t, workflow.TaskOutcomeCompleted, result.Outcome)
}
