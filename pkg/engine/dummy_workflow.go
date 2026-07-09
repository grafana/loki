package engine

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// DummyWorkflowParams configures a DummyLoad-based workflow for benchmarking
// and stress-testing the scheduler/worker infrastructure.
type DummyWorkflowParams struct {
	// Tenant is the tenant ID to associate with the workflow.
	Tenant string
	// NumBatches is the total number of batches to generate across all shards.
	NumBatches int
	// BatchSize is the number of rows per generated batch.
	BatchSize int
	// SleepPerBatch is how long to sleep before emitting each batch.
	SleepPerBatch time.Duration
	// Parallelism is the number of parallel DummyLoad shards.
	Parallelism int
	// TopK is the K value for the TopK node. Defaults to 1000 if zero.
	TopK int
	// Logger is an optional logger. Defaults to nop logger if nil.
	Logger log.Logger
}

// RunDummyWorkflow builds a TopK←Parallelize←DummyLoad physical plan,
// submits it as a workflow to sched, drains all results, and returns the
// total number of rows and batches produced.
//
// It is intended for benchmarking and stress-testing the scheduler/worker
// infrastructure with synthetic data.
func RunDummyWorkflow(ctx context.Context, sched *Scheduler, params DummyWorkflowParams) (rows, batches int64, err error) {
	logger := params.Logger
	if logger == nil {
		logger = log.NewNopLogger()
	}
	k := params.TopK
	if k <= 0 {
		k = 1000
	}

	plan := buildDummyPlan(params, k)

	opts := workflow.Options{
		Tenant:               params.Tenant,
		MaxRunningScanTasks:  0,
		MaxRunningOtherTasks: 0,
	}
	wf, err := workflow.New(ctx, opts, logger, sched.inner, plan)
	if err != nil {
		return 0, 0, err
	}
	defer wf.Close()

	pipeline, err := wf.Run(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer pipeline.Close()

	if err := pipeline.Open(ctx); err != nil {
		return 0, 0, err
	}

	for {
		rec, readErr := pipeline.Read(ctx)
		if readErr == executor.EOF {
			break
		}
		if readErr != nil {
			return rows, batches, readErr
		}
		if rec != nil {
			batches++
			rows += rec.NumRows()
		}
	}
	return rows, batches, nil
}

// buildDummyPlan builds: TopK ← Parallelize ← DummyLoad.
func buildDummyPlan(params DummyWorkflowParams, k int) *physical.Plan {
	topk := &physical.TopK{
		NodeID:     ulid.Make(),
		SortBy:     &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
		Ascending:  false,
		NullsFirst: false,
		K:          k,
	}
	parallelize := &physical.Parallelize{NodeID: ulid.Make()}
	dummyLoad := &physical.DummyLoad{
		NodeID:        ulid.Make(),
		NumBatches:    params.NumBatches,
		BatchSize:     params.BatchSize,
		SleepPerBatch: params.SleepPerBatch,
		Parallelism:   params.Parallelism,
	}

	g := dag.Graph[physical.Node]{}
	g.Add(topk)
	g.Add(parallelize)
	g.Add(dummyLoad)
	_ = g.AddEdge(dag.Edge[physical.Node]{Parent: topk, Child: parallelize})
	_ = g.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: dummyLoad})
	return physical.FromGraph(g)
}
