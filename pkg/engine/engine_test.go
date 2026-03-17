package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

type fakeMetastore struct{}

func (fakeMetastore) Sections(_ context.Context, _ metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	return metastore.SectionsResponse{}, nil
}
func (fakeMetastore) GetIndexes(_ context.Context, _ metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{}, nil
}
func (fakeMetastore) IndexSectionsReader(_ context.Context, _ metastore.IndexSectionsReaderRequest) (metastore.IndexSectionsReaderResponse, error) {
	return metastore.IndexSectionsReaderResponse{}, nil
}
func (fakeMetastore) CollectSections(_ context.Context, _ metastore.CollectSectionsRequest) (metastore.CollectSectionsResponse, error) {
	return metastore.CollectSectionsResponse{}, nil
}
func (fakeMetastore) Labels(_ context.Context, _, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}
func (fakeMetastore) Values(_ context.Context, _, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

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

func TestCountScanTargets(t *testing.T) {
	t.Run("no scan targets", func(t *testing.T) {
		plan := minimalPlan()
		require.Equal(t, 0, countScanTargets(plan))
	})

	t.Run("with data object scan targets", func(t *testing.T) {
		var g dag.Graph[physical.Node]
		ss := g.Add(&physical.ScanSet{
			Targets: []*physical.ScanTarget{
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{}},
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{}},
				{Type: physical.ScanTypePointers, Pointers: &physical.PointersScan{}},
			},
		})
		limit := g.Add(&physical.Limit{Fetch: 10})
		_ = g.AddEdge(dag.Edge[physical.Node]{Parent: limit, Child: ss})
		plan := physical.FromGraph(g)

		require.Equal(t, 2, countScanTargets(plan))
	})
}

type fakeIndexGatewayClient struct {
	mu       sync.Mutex
	calls    int
	resp     *logproto.GetDataobjSectionsResponse
	err      error
	duration time.Duration
}

func (f *fakeIndexGatewayClient) GetDataobjSections(_ context.Context, _ *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
	if f.duration > 0 {
		time.Sleep(f.duration)
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.resp, f.err
}

func TestMaybeDualResolve_Disabled(t *testing.T) {
	e := &Engine{
		logger:  log.NewNopLogger(),
		metrics: newMetrics(prometheus.NewRegistry()),
	}

	// dualResolveSem is nil, should be a no-op
	e.maybeDualResolve(
		log.NewNopLogger(), "tenant", nil, nil, nil, 0, 0,
	)
}

func TestMaybeDualResolve_DropsWhenFull(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	e := &Engine{
		logger:         log.NewNopLogger(),
		metrics:        m,
		dualResolveSem: make(chan struct{}, 1),
	}

	// Fill the semaphore
	e.dualResolveSem <- struct{}{}

	e.maybeDualResolve(
		log.NewNopLogger(), "tenant", nil, nil, nil, 0, 0,
	)

	// Verify the dropped counter was incremented
	mfs, err := reg.Gather()
	require.NoError(t, err)

	var found *io_prometheus.MetricFamily
	for _, mf := range mfs {
		if mf.GetName() == "loki_engine_v2_dual_resolve_dropped_total" {
			found = mf
			break
		}
	}
	require.NotNil(t, found)
	require.Len(t, found.GetMetric(), 1)
	require.Equal(t, 1.0, found.GetMetric()[0].GetCounter().GetValue())

	// Drain semaphore
	<-e.dualResolveSem
}

func TestMaybeDualResolve_RunsAsync(t *testing.T) {
	client := &fakeIndexGatewayClient{
		resp: &logproto.GetDataobjSectionsResponse{},
		err:  fmt.Errorf("expected error"),
	}

	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	e := &Engine{
		logger:         log.NewNopLogger(),
		metrics:        m,
		indexGateway:   client,
		dualResolveSem: make(chan struct{}, 10),
	}

	// Use a real planner context
	plannerCtx := physical.NewContext(time.Now().Add(-time.Hour), time.Now())

	e.maybeDualResolve(
		log.NewNopLogger(), "tenant", nil, nil, plannerCtx, 5, time.Millisecond,
	)

	// Wait for the goroutine to finish by waiting for the semaphore to be drained
	require.Eventually(t, func() bool {
		return len(e.dualResolveSem) == 0
	}, 5*time.Second, 10*time.Millisecond)
}

func TestEngine_DualResolveSemaphore_Initialization(t *testing.T) {
	t.Run("disabled when concurrency is zero", func(t *testing.T) {
		inner, err := scheduler.New(scheduler.Config{
			Logger:   log.NewNopLogger(),
			Listener: &wire.Local{Address: wire.LocalScheduler},
		})
		require.NoError(t, err)

		e := &Engine{
			logger:    log.NewNopLogger(),
			metrics:   newMetrics(prometheus.NewRegistry()),
			scheduler: &Scheduler{inner: inner},
			cfg:       Config{DualResolveMaxConcurrency: 0},
		}
		require.Nil(t, e.dualResolveSem)
	})

	t.Run("disabled when no gateway client", func(t *testing.T) {
		e, err := New(Params{
			Logger:     log.NewNopLogger(),
			Registerer: prometheus.NewRegistry(),
			Config: Config{
				DualResolveMaxConcurrency: 10,
				Executor:                  ExecutorConfig{BatchSize: 100},
			},
			Scheduler: &Scheduler{inner: func() *scheduler.Scheduler {
				s, _ := scheduler.New(scheduler.Config{
					Logger:   log.NewNopLogger(),
					Listener: &wire.Local{Address: wire.LocalScheduler},
				})
				return s
			}()},
			Metastore: fakeMetastore{},
		})
		require.NoError(t, err)
		require.Nil(t, e.dualResolveSem)
	})

	t.Run("enabled when gateway and concurrency set", func(t *testing.T) {
		e, err := New(Params{
			Logger:     log.NewNopLogger(),
			Registerer: prometheus.NewRegistry(),
			Config: Config{
				DualResolveMaxConcurrency: 5,
				Executor:                  ExecutorConfig{BatchSize: 100},
			},
			Scheduler: &Scheduler{inner: func() *scheduler.Scheduler {
				s, _ := scheduler.New(scheduler.Config{
					Logger:   log.NewNopLogger(),
					Listener: &wire.Local{Address: wire.LocalScheduler},
				})
				return s
			}()},
			Metastore:    fakeMetastore{},
			IndexGateway: &fakeIndexGatewayClient{},
		})
		require.NoError(t, err)
		require.NotNil(t, e.dualResolveSem)
		require.Equal(t, 5, cap(e.dualResolveSem))
	})
}
