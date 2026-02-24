package engine

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logql"
)

type fakeLimits struct {
	logql.Limits
	parallelism int
}

func (l *fakeLimits) MaxScanTaskParallelism(_ string) int {
	return l.parallelism
}

func newTestEngine(t *testing.T, limits logql.Limits) *Engine {
	t.Helper()

	inner, err := scheduler.New(scheduler.Config{
		Logger:   log.NewNopLogger(),
		Listener: &wire.Local{Address: wire.LocalScheduler},
	})
	require.NoError(t, err)

	return &Engine{
		logger:    log.NewNopLogger(),
		metrics:   newMetrics(prometheus.NewRegistry()),
		limits:    limits,
		scheduler: &Scheduler{inner: inner},
	}
}

func minimalPlan() *physical.Plan {
	var g dag.Graph[physical.Node]
	g.Add(&physical.DataObjScan{})
	return physical.FromGraph(g)
}

func TestEngine_AdmissionLanes(t *testing.T) {
	const tenant = "test-tenant"
	const parallelism = 42

	limits := &fakeLimits{
		Limits:      logql.NoLimits,
		parallelism: parallelism,
	}

	tests := []struct {
		name              string
		useAdmissionLanes bool
		wantMaxScanTasks  int
	}{
		{
			name:              "admission lanes enabled",
			useAdmissionLanes: true,
			wantMaxScanTasks:  parallelism,
		},
		{
			name:              "admission lanes disabled",
			useAdmissionLanes: false,
			wantMaxScanTasks:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEngine(t, limits)
			wf, _, err := e.buildWorkflow(context.Background(), tenant, log.NewNopLogger(), minimalPlan(), tt.useAdmissionLanes)
			require.NoError(t, err)
			defer wf.Close()

			require.EqualValues(t, tt.wantMaxScanTasks, wf.Opts().MaxRunningScanTasks)
			require.EqualValues(t, 0, wf.Opts().MaxRunningOtherTasks)
		})
	}
}

func TestEngine_IsMetricQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{
			name:  "log query",
			query: `{app="test"}`,
			want:  false,
		},
		{
			name:  "metric query",
			query: `rate({app="test"}[5m])`,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)
			require.Equal(t, tt.want, isMetricQuery(expr))
		})
	}
}
