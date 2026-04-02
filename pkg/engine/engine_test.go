package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
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
		log.NewNopLogger(), "tenant", nil, nil, nil, nil, 0,
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

	params := fakeParams{start: time.Now().Add(-time.Hour), end: time.Now()}
	e.maybeDualResolve(
		log.NewNopLogger(), "tenant", params, nil, nil, nil, 0,
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

	params := fakeParams{start: time.Now().Add(-time.Hour), end: time.Now()}
	e.maybeDualResolve(
		log.NewNopLogger(), "tenant", params, nil, plannerCtx, nil, time.Millisecond,
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

func TestExtractScanSections(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tr1 := physical.TimeRange{Start: now, End: now.Add(10 * time.Second)}
	tr2 := physical.TimeRange{Start: now.Add(-time.Minute), End: now}

	var g dag.Graph[physical.Node]
	ss := g.Add(&physical.ScanSet{
		Targets: []*physical.ScanTarget{
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
				Location: "obj1", Section: 0, StreamIDs: []int64{10, 20}, MaxTimeRange: tr1,
			}},
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
				Location: "obj2", Section: 3, StreamIDs: []int64{5}, MaxTimeRange: tr2,
			}},
			{Type: physical.ScanTypePointers, Pointers: &physical.PointersScan{}},
		},
	})
	limit := g.Add(&physical.Limit{Fetch: 10})
	_ = g.AddEdge(dag.Edge[physical.Node]{Parent: limit, Child: ss})
	plan := physical.FromGraph(g)

	sections := extractScanSections(plan)
	require.Len(t, sections, 2)
	require.Equal(t, sectionKey{Location: "obj1", Section: 0}, sections[0].Key)
	require.Equal(t, []int64{10, 20}, sections[0].StreamIDs)
	require.Equal(t, tr1, sections[0].TimeRange)
	require.Equal(t, sectionKey{Location: "obj2", Section: 3}, sections[1].Key)
	require.Equal(t, []int64{5}, sections[1].StreamIDs)
	require.Equal(t, tr2, sections[1].TimeRange)
}

func TestCompareSections(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tr := physical.TimeRange{Start: now, End: now.Add(10 * time.Second)}

	t.Run("exact match", func(t *testing.T) {
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 2}, TimeRange: tr},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{3}, TimeRange: tr},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{2, 1}, TimeRange: tr},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{3}, TimeRange: tr},
		}
		cmp := compareSections(ms, igw)
		require.Equal(t, 2, cmp.MsCount)
		require.Equal(t, 2, cmp.IgwCount)
		require.True(t, cmp.IgwSupersetOfMs)
		require.Empty(t, cmp.MissingFromIgw)
		require.Empty(t, cmp.StreamMismatches)
		require.Empty(t, cmp.TimeMismatches)
	})

	t.Run("igw is superset", func(t *testing.T) {
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1}, TimeRange: tr},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1}, TimeRange: tr},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{2}, TimeRange: tr},
		}
		cmp := compareSections(ms, igw)
		require.Equal(t, 1, cmp.MsCount)
		require.Equal(t, 2, cmp.IgwCount)
		require.True(t, cmp.IgwSupersetOfMs)
		require.Empty(t, cmp.MissingFromIgw)
		require.Empty(t, cmp.StreamMismatches)
		require.Empty(t, cmp.TimeMismatches)
	})

	t.Run("missing sections from igw", func(t *testing.T) {
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1}, TimeRange: tr},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{2}, TimeRange: tr},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1}, TimeRange: tr},
		}
		cmp := compareSections(ms, igw)
		require.Equal(t, 2, cmp.MsCount)
		require.Equal(t, 1, cmp.IgwCount)
		require.False(t, cmp.IgwSupersetOfMs)
		require.Equal(t, []sectionKey{{"obj2", 1}}, cmp.MissingFromIgw)
		require.Empty(t, cmp.StreamMismatches)
		require.Empty(t, cmp.TimeMismatches)
	})

	t.Run("stream id mismatches", func(t *testing.T) {
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 2, 3}, TimeRange: tr},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 2, 4}, TimeRange: tr},
		}
		cmp := compareSections(ms, igw)
		require.Equal(t, 1, cmp.MsCount)
		require.Equal(t, 1, cmp.IgwCount)
		require.True(t, cmp.IgwSupersetOfMs)
		require.Empty(t, cmp.MissingFromIgw)
		require.Equal(t, []sectionKey{{"obj1", 0}}, cmp.StreamMismatches)
		require.Empty(t, cmp.TimeMismatches)
	})

	t.Run("time range mismatches", func(t *testing.T) {
		trDifferent := physical.TimeRange{Start: now.Add(-time.Minute), End: now}
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 2}, TimeRange: tr},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{3}, TimeRange: tr},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 2}, TimeRange: trDifferent},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{3}, TimeRange: tr},
		}
		cmp := compareSections(ms, igw)
		require.Equal(t, 2, cmp.MsCount)
		require.Equal(t, 2, cmp.IgwCount)
		require.True(t, cmp.IgwSupersetOfMs)
		require.Empty(t, cmp.MissingFromIgw)
		require.Empty(t, cmp.StreamMismatches)
		require.Equal(t, []sectionKey{{"obj1", 0}}, cmp.TimeMismatches)
	})

	t.Run("sub-millisecond difference is tolerated", func(t *testing.T) {
		trNano := physical.TimeRange{Start: now.Add(123 * time.Nanosecond), End: now.Add(time.Hour).Add(456 * time.Nanosecond)}
		trMilli := physical.TimeRange{Start: now.Truncate(time.Millisecond), End: now.Add(time.Hour).Truncate(time.Millisecond)}
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1}, TimeRange: trNano},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1}, TimeRange: trMilli},
		}
		cmp := compareSections(ms, igw)
		require.Empty(t, cmp.TimeMismatches)
	})

	t.Run("both missing and mismatched", func(t *testing.T) {
		ms := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 2}, TimeRange: tr},
			{Key: sectionKey{"obj2", 1}, StreamIDs: []int64{3}, TimeRange: tr},
			{Key: sectionKey{"obj3", 2}, StreamIDs: []int64{5}, TimeRange: tr},
		}
		igw := []resolvedSection{
			{Key: sectionKey{"obj1", 0}, StreamIDs: []int64{1, 99}, TimeRange: tr},
		}
		cmp := compareSections(ms, igw)
		require.Equal(t, 3, cmp.MsCount)
		require.Equal(t, 1, cmp.IgwCount)
		require.False(t, cmp.IgwSupersetOfMs)
		require.Len(t, cmp.MissingFromIgw, 2)
		require.Len(t, cmp.StreamMismatches, 1)
		require.Equal(t, sectionKey{"obj1", 0}, cmp.StreamMismatches[0])
		require.Empty(t, cmp.TimeMismatches)
	})

	t.Run("empty inputs", func(t *testing.T) {
		cmp := compareSections(nil, nil)
		require.Equal(t, 0, cmp.MsCount)
		require.Equal(t, 0, cmp.IgwCount)
		require.True(t, cmp.IgwSupersetOfMs)
		require.Empty(t, cmp.MissingFromIgw)
		require.Empty(t, cmp.StreamMismatches)
		require.Empty(t, cmp.TimeMismatches)
	})
}

func TestValidate_UseIndexGatewayPlanning(t *testing.T) {
	newScheduler := func(t *testing.T) *Scheduler {
		t.Helper()
		s, err := scheduler.New(scheduler.Config{
			Logger:   log.NewNopLogger(),
			Listener: &wire.Local{Address: wire.LocalScheduler},
		})
		require.NoError(t, err)
		return &Scheduler{inner: s}
	}

	t.Run("errors when enabled without gateway client", func(t *testing.T) {
		p := Params{
			Logger:     log.NewNopLogger(),
			Registerer: prometheus.NewRegistry(),
			Config: Config{
				UseIndexGatewayPlanning: true,
				Executor:                ExecutorConfig{BatchSize: 100},
			},
			Scheduler: newScheduler(t),
			Metastore: fakeMetastore{},
		}
		_, err := New(p)
		require.Error(t, err)
		require.Contains(t, err.Error(), "index gateway client is required")
	})

	t.Run("succeeds when enabled with gateway client", func(t *testing.T) {
		p := Params{
			Logger:     log.NewNopLogger(),
			Registerer: prometheus.NewRegistry(),
			Config: Config{
				UseIndexGatewayPlanning: true,
				Executor:                ExecutorConfig{BatchSize: 100},
			},
			Scheduler:    newScheduler(t),
			Metastore:    fakeMetastore{},
			IndexGateway: &fakeIndexGatewayClient{},
		}
		e, err := New(p)
		require.NoError(t, err)
		require.NotNil(t, e)
	})

	t.Run("succeeds when disabled without gateway client", func(t *testing.T) {
		p := Params{
			Logger:     log.NewNopLogger(),
			Registerer: prometheus.NewRegistry(),
			Config: Config{
				UseIndexGatewayPlanning: false,
				Executor:                ExecutorConfig{BatchSize: 100},
			},
			Scheduler: newScheduler(t),
			Metastore: fakeMetastore{},
		}
		e, err := New(p)
		require.NoError(t, err)
		require.NotNil(t, e)
	})
}

func TestTSDBCoversQuery(t *testing.T) {
	tsdbDate := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("unset date always returns true", func(t *testing.T) {
		cfg := Config{}
		require.True(t, cfg.TSDBCoversQuery(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
		require.True(t, cfg.TSDBCoversQuery(time.Now()))
	})

	t.Run("query after TSDB start date", func(t *testing.T) {
		cfg := Config{TSDBStartDate: flagext.Time(tsdbDate)}
		require.True(t, cfg.TSDBCoversQuery(tsdbDate.Add(24*time.Hour)))
	})

	t.Run("query exactly at TSDB start date", func(t *testing.T) {
		cfg := Config{TSDBStartDate: flagext.Time(tsdbDate)}
		require.True(t, cfg.TSDBCoversQuery(tsdbDate))
	})

	t.Run("query before TSDB start date", func(t *testing.T) {
		cfg := Config{TSDBStartDate: flagext.Time(tsdbDate)}
		require.False(t, cfg.TSDBCoversQuery(tsdbDate.Add(-24*time.Hour)))
	})
}

func TestMergeDataObjSections(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	t.Run("empty batches", func(t *testing.T) {
		result := mergeDataObjSections(nil)
		require.Empty(t, result)
	})

	t.Run("single batch passthrough", func(t *testing.T) {
		batch := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1, 2}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
			{Location: "obj2", Sections: []int{1}, Streams: []int64{3}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		result := mergeDataObjSections([][]physical.DataObjSections{batch})
		require.Len(t, result, 2)
	})

	t.Run("dedup same section across batches", func(t *testing.T) {
		batch1 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1, 2}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		batch2 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1, 2}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		result := mergeDataObjSections([][]physical.DataObjSections{batch1, batch2})
		require.Len(t, result, 1)
		require.Equal(t, physical.DataObjLocation("obj1"), result[0].Location)
		require.Equal(t, []int{0}, result[0].Sections)
	})

	t.Run("merges stream IDs as union", func(t *testing.T) {
		batch1 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1, 3}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		batch2 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{2, 3}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		result := mergeDataObjSections([][]physical.DataObjSections{batch1, batch2})
		require.Len(t, result, 1)
		require.Equal(t, []int64{1, 2, 3}, result[0].Streams)
	})

	t.Run("widens time range on merge", func(t *testing.T) {
		batch1 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1}, TimeRange: physical.TimeRange{Start: now, End: now.Add(30 * time.Minute)}},
		}
		batch2 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1}, TimeRange: physical.TimeRange{Start: now.Add(-10 * time.Minute), End: now.Add(time.Hour)}},
		}
		result := mergeDataObjSections([][]physical.DataObjSections{batch1, batch2})
		require.Len(t, result, 1)
		require.Equal(t, now.Add(-10*time.Minute), result[0].TimeRange.Start)
		require.Equal(t, now.Add(time.Hour), result[0].TimeRange.End)
	})

	t.Run("different sections not merged", func(t *testing.T) {
		batch1 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		batch2 := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{1}, Streams: []int64{2}, TimeRange: physical.TimeRange{Start: now, End: now.Add(time.Hour)}},
		}
		result := mergeDataObjSections([][]physical.DataObjSections{batch1, batch2})
		require.Len(t, result, 2)
	})

	t.Run("different locations not merged", func(t *testing.T) {
		tr := physical.TimeRange{Start: now, End: now.Add(time.Hour)}
		batch := []physical.DataObjSections{
			{Location: "obj1", Sections: []int{0}, Streams: []int64{1}, TimeRange: tr},
			{Location: "obj2", Sections: []int{0}, Streams: []int64{1}, TimeRange: tr},
		}
		result := mergeDataObjSections([][]physical.DataObjSections{batch})
		require.Len(t, result, 2)
	})
}

func TestUnionSortedInt64(t *testing.T) {
	t.Run("disjoint", func(t *testing.T) {
		require.Equal(t, []int64{1, 2, 3, 4}, unionSortedInt64([]int64{1, 3}, []int64{2, 4}))
	})

	t.Run("overlapping", func(t *testing.T) {
		require.Equal(t, []int64{1, 2, 3}, unionSortedInt64([]int64{1, 2}, []int64{2, 3}))
	})

	t.Run("empty inputs", func(t *testing.T) {
		require.Empty(t, unionSortedInt64(nil, nil))
	})

	t.Run("one empty", func(t *testing.T) {
		require.Equal(t, []int64{1, 2}, unionSortedInt64([]int64{1, 2}, nil))
	})
}

// recordingIndexGatewayClient records every request it receives and returns
// a response based on a user-supplied function.
type recordingIndexGatewayClient struct {
	mu       sync.Mutex
	requests []*logproto.GetDataobjSectionsRequest
	respFunc func(*logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error)
}

func (r *recordingIndexGatewayClient) GetDataobjSections(_ context.Context, in *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
	r.mu.Lock()
	r.requests = append(r.requests, in)
	r.mu.Unlock()
	return r.respFunc(in)
}

func TestTSDBSplitResolve(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	section := logproto.DataobjSectionRef{
		Path:      "obj1",
		SectionId: 0,
		StreamIds: []int64{1, 2},
		MinTime:   model.TimeFromUnix(now.Add(-3 * time.Hour).Unix()),
		MaxTime:   model.TimeFromUnix(now.Unix()),
	}

	t.Run("splits 3h range into 1h sub-requests", func(t *testing.T) {
		client := &recordingIndexGatewayClient{
			respFunc: func(_ *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
				return &logproto.GetDataobjSectionsResponse{
					Sections: []logproto.DataobjSectionRef{section},
				}, nil
			},
		}

		e := &Engine{
			logger:       log.NewNopLogger(),
			metrics:      newMetrics(prometheus.NewRegistry()),
			cfg:          Config{TSDBSplitInterval: time.Hour, TSDBSplitConcurrency: 8},
			indexGateway: client,
		}

		start := now.Add(-3 * time.Hour)
		end := now
		result, err := e.tsdbSplitResolve(context.Background(), `{app="test"}`, start, end, time.Hour)
		require.NoError(t, err)

		// Should produce 3 sub-requests
		client.mu.Lock()
		require.Equal(t, 3, len(client.requests))
		client.mu.Unlock()

		// Same section returned by all 3 sub-requests should be deduped to 1
		require.Len(t, result, 1)
		require.Equal(t, physical.DataObjLocation("obj1"), result[0].Location)
	})

	t.Run("no split when range smaller than interval", func(t *testing.T) {
		client := &recordingIndexGatewayClient{
			respFunc: func(_ *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
				return &logproto.GetDataobjSectionsResponse{
					Sections: []logproto.DataobjSectionRef{section},
				}, nil
			},
		}

		e := &Engine{
			logger:       log.NewNopLogger(),
			metrics:      newMetrics(prometheus.NewRegistry()),
			cfg:          Config{TSDBSplitInterval: 24 * time.Hour, TSDBSplitConcurrency: 8},
			indexGateway: client,
		}

		start := now.Add(-3 * time.Hour)
		end := now
		result, err := e.tsdbSplitResolve(context.Background(), `{app="test"}`, start, end, 24*time.Hour)
		require.NoError(t, err)

		client.mu.Lock()
		require.Equal(t, 1, len(client.requests))
		client.mu.Unlock()

		require.Len(t, result, 1)
	})

	t.Run("propagates errors from sub-requests", func(t *testing.T) {
		client := &recordingIndexGatewayClient{
			respFunc: func(_ *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
				return nil, fmt.Errorf("rpc failed")
			},
		}

		e := &Engine{
			logger:       log.NewNopLogger(),
			metrics:      newMetrics(prometheus.NewRegistry()),
			cfg:          Config{TSDBSplitInterval: time.Hour, TSDBSplitConcurrency: 8},
			indexGateway: client,
		}

		start := now.Add(-3 * time.Hour)
		end := now
		_, err := e.tsdbSplitResolve(context.Background(), `{app="test"}`, start, end, time.Hour)
		require.Error(t, err)
		require.Contains(t, err.Error(), "rpc failed")
	})

	t.Run("resolver uses split when configured", func(t *testing.T) {
		client := &recordingIndexGatewayClient{
			respFunc: func(_ *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
				return &logproto.GetDataobjSectionsResponse{
					Sections: []logproto.DataobjSectionRef{section},
				}, nil
			},
		}

		e := &Engine{
			logger:       log.NewNopLogger(),
			metrics:      newMetrics(prometheus.NewRegistry()),
			cfg:          Config{TSDBSplitInterval: time.Hour, TSDBSplitConcurrency: 8},
			indexGateway: client,
		}

		resolver := e.tsdbSectionsResolver(context.Background(), "tenant")
		start := now.Add(-2 * time.Hour)
		end := now
		result, err := resolver(`{app="test"}`, start, end)
		require.NoError(t, err)
		require.Len(t, result, 1)

		// Should have made 2 sub-requests via the split path
		client.mu.Lock()
		require.Equal(t, 2, len(client.requests))
		client.mu.Unlock()
	})

	t.Run("resolver skips split when not configured", func(t *testing.T) {
		client := &recordingIndexGatewayClient{
			respFunc: func(_ *logproto.GetDataobjSectionsRequest) (*logproto.GetDataobjSectionsResponse, error) {
				return &logproto.GetDataobjSectionsResponse{
					Sections: []logproto.DataobjSectionRef{section},
				}, nil
			},
		}

		e := &Engine{
			logger:       log.NewNopLogger(),
			metrics:      newMetrics(prometheus.NewRegistry()),
			cfg:          Config{TSDBSplitInterval: 0},
			indexGateway: client,
		}

		resolver := e.tsdbSectionsResolver(context.Background(), "tenant")
		start := now.Add(-3 * time.Hour)
		end := now
		result, err := resolver(`{app="test"}`, start, end)
		require.NoError(t, err)
		require.Len(t, result, 1)

		// Should have made exactly 1 request (no splitting)
		client.mu.Lock()
		require.Equal(t, 1, len(client.requests))
		client.mu.Unlock()
	})
}
