package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
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
