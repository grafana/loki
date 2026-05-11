package engine

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
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

type fakeMetastore struct {
	metastore.Metastore
	indexes []metastore.IndexEntry
}

func (f *fakeMetastore) GetIndexes(_ context.Context, _ metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{Indexes: f.indexes}, nil
}

func (f *fakeMetastore) CollectSections(ctx context.Context, req metastore.CollectSectionsRequest) (metastore.CollectSectionsResponse, error) {
	return metastore.CollectSectionsResponse{SectionsResponse: metastore.SectionsResponse{Sections: []*metastore.DataobjSectionDescriptor{
		{
			SectionKey: metastore.SectionKey{
				ObjectPath: "index/0",
				SectionIdx: 0,
			},
		},
	}}}, nil
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
		metastore: &fakeMetastore{indexes: []metastore.IndexEntry{
			{
				Path:  "index/0",
				Start: time.Now(),
				End:   time.Now().Add(time.Hour),
			},
		}},
		cfg: Config{
			Executor: ExecutorConfig{
				BatchSize: 128,
			},
		},
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
			wf, _, err := e.buildWorkflow(context.Background(), tenant, log.NewNopLogger(), minimalPlan(), tt.useAdmissionLanes, false)
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

func TestEngine_LimitedQueryPlansContributeTimeRanges(t *testing.T) {
	const tenant = "test-tenant"
	const parallelism = 42

	limits := &fakeLimits{
		Limits:      logql.NoLimits,
		parallelism: parallelism,
	}

	tests := []struct {
		name       string
		query      string
		expectTopK bool
	}{
		{
			name:       "log query",
			query:      `{app="test"}`,
			expectTopK: true,
		},
		{
			name:       "metric query",
			query:      `rate({app="test"}[5m])`,
			expectTopK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build an execution plan & workflow from the input query
			params, err := logql.NewLiteralParams(tt.query, time.Now().UTC(), time.Now().Add(time.Hour).UTC(), 0, 0, logproto.BACKWARD, 1000, nil, nil)
			require.NoError(t, err)

			logicalPlan, err := logical.BuildPlan(context.Background(), params)
			require.NoError(t, err)

			e := newTestEngine(t, limits)

			physicalPlan, _, err := e.buildPhysicalPlan(context.Background(), tenant, log.NewNopLogger(), params, logicalPlan, false)
			require.NoError(t, err)

			// Verify the plan contains a TopK node
			require.Equal(t, tt.expectTopK, containsTopK(physicalPlan))
			if !tt.expectTopK {
				return
			}

			// Build a workflow to generate the batching pipelines
			wf, _, err := e.buildWorkflow(context.Background(), tenant, log.NewNopLogger(), physicalPlan, false, false)
			require.NoError(t, err)
			defer wf.Close()

			for _, task := range wf.AllTasks() {
				if len(task.Sources) != 0 {
					continue // not a leaf task
				}

				// For each leaf task fragment, confirm it unwraps to a ContributingTimeRangeChangedNotifier
				pipeline := executor.Run(context.Background(), executor.Config{BatchSize: 128}, task.Fragment, log.NewNopLogger())
				require.NoError(t, err)
				defer pipeline.Close()

				unwrapped := executor.Unwrap(pipeline)
				require.NotNil(t, unwrapped)

				_, isNotifier := unwrapped.(executor.ContributingTimeRangeChangedNotifier)
				require.True(t, isNotifier, "expected a contributing time range changed notifier")
			}
		})
	}
}

func containsTopK(fragment *physical.Plan) bool {
	containsTopK := false
	for _, root := range fragment.Roots() {
		fragment.DFSWalk(root, func(n physical.Node) error {
			if n.Type() == physical.NodeTypeTopK {
				containsTopK = true
			}
			return nil
		}, dag.PreOrderWalk)
	}
	return containsTopK
}
