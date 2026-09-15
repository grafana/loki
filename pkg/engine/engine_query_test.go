package engine

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// TestQueryClose_FanoutObservation pins the decision that the per-query fan-out
// histograms are observed only once a query has generated at least one task, so
// queries that fail during planning don't flood the histograms with zeros. The
// residual duration histogram is observed unconditionally.
func TestQueryClose_FanoutObservation(t *testing.T) {
	tests := []struct {
		name         string
		tasksPlanned int64
		tasksPruned  int64
		recordTasks  bool
		wantFanout   bool
	}{
		{
			name:        "failed during planning observes no fan-out",
			recordTasks: false,
			wantFanout:  false,
		},
		{
			name:         "query that generated tasks observes fan-out",
			tasksPlanned: 4,
			tasksPruned:  1,
			recordTasks:  true,
			wantFanout:   true,
		},
		{
			name:         "all tasks pruned still observes fan-out",
			tasksPlanned: 0,
			tasksPruned:  3,
			recordTasks:  true,
			wantFanout:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			e := &Engine{
				logger:  log.NewNopLogger(),
				metrics: newMetrics(reg),
			}

			ctx := user.InjectOrgID(t.Context(), "test-tenant")
			q, _, err := e.newQuery(ctx, log.NewNopLogger(), "logs", false)
			require.NoError(t, err)

			if tt.recordTasks {
				q.rootRegion.Record(scheduler.StatPlannedTasks.Observe(tt.tasksPlanned))
				q.rootRegion.Record(workflow.StatPrunedTasks.Observe(tt.tasksPruned))
			}

			q.Close()

			// The residual duration histogram is always observed.
			require.Equal(t, 1, testutil.CollectAndCount(e.metrics.query.other), "residual duration always observed")

			wantSeries := 0
			if tt.wantFanout {
				wantSeries = 1
			}
			require.Equal(t, wantSeries, testutil.CollectAndCount(e.metrics.execution.tasksPerQuery), "tasks_per_query observation")
			require.Equal(t, wantSeries, testutil.CollectAndCount(e.metrics.execution.tasksGenerated), "tasks_generated_per_query observation")
		})
	}
}
