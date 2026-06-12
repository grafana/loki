package engine

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

type noOpAdmissionLaneLimits struct {
	logql.Limits
	t *testing.T
}

func (l *noOpAdmissionLaneLimits) MaxScanTaskParallelism(_ string) int {
	l.t.Fatal("max_scan_task_parallelism should be a no-op")
	return 0
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

func TestEngine_MaxScanTaskParallelismIsNoop(t *testing.T) {
	limits := &noOpAdmissionLaneLimits{Limits: logql.NoLimits, t: t}
	e := newTestEngine(t, limits)

	ctx := user.InjectOrgID(t.Context(), "test-tenant")
	q, ctx, err := e.newQuery(ctx, log.NewNopLogger(), "test", false)
	require.NoError(t, err)
	defer q.Close()

	wf, err := q.Prepare(ctx, minimalPlan())
	require.NoError(t, err)
	defer wf.Close()
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
