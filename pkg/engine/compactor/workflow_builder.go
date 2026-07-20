package compactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// buildIndexMergePlan returns a single-root physical.Plan holding exactly one
// IndexMerge node. Each coordinator cycle runs ⌈P/K⌉ such plans
// concurrently — one per sorted pile of index sections.
func buildIndexMergePlan(
	tenant string,
	window time.Time,
	task *compactionv2pb.TaskSpec,
) *physical.Plan {
	node := &physical.IndexMerge{
		// Each call mints a fresh NodeID so racing builds don't collide on the
		// scheduler's manifest registry.
		NodeID:         ulid.Make(),
		Tenant:         tenant,
		ToCWindowStart: window.UnixNano(),
		Runs:           task.Runs,
	}
	var g dag.Graph[physical.Node]
	g.Add(node)
	return physical.FromGraph(g)
}

func buildLogMergePlan(
	tenant string,
	window time.Time,
	task *compactionv2pb.TaskSpec,
) *physical.Plan {
	node := &physical.LogMerge{
		// Each call mints a fresh NodeID so racing builds don't collide
		NodeID:         ulid.Make(),
		Tenant:         tenant,
		ToCWindowStart: window.UnixNano(),
		Runs:           task.Runs,
		SortSchema:     task.SortSchema,
	}
	var g dag.Graph[physical.Node]
	g.Add(node)
	return physical.FromGraph(g)
}

// runPlan constructs a workflow.Workflow from a single-root plan, runs it,
// and drains the pipeline. A compaction job emits exactly one record batch
// reporting the artifacts it produced. Returns the record batch or nil,nil
// if the pipeline was empty.
func runPlan(
	ctx context.Context,
	logger log.Logger,
	runner workflow.Runner,
	opts workflow.Options,
	plan *physical.Plan,
) (arrow.RecordBatch, error) {
	wf, err := workflow.New(ctx, opts, logger, runner, plan)
	if err != nil {
		return nil, fmt.Errorf("workflow.New: %w", err)
	}
	defer wf.Close()

	pipeline, err := wf.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("workflow.Run: %w", err)
	}
	reader := executor.TranslateEOF(pipeline)
	defer reader.Close()

	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("pipeline.Open: %w", err)
	}

	var result arrow.RecordBatch
	for {
		rec, err := reader.Read(ctx)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("pipeline.Read: %w", err)
		}
		if result != nil {
			return nil, fmt.Errorf("compaction job produced more than one result record")
		}
		result = rec
	}
}
