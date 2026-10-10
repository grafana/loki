package compactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
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

// buildIndexFilterPlan returns a single-root physical.Plan holding one
// IndexFilter node that keeps the rows of objectPaths from sourceIndexPath.
func buildIndexFilterPlan(tenant, sourceIndexPath string, objectPaths []string) *physical.Plan {
	node := &physical.IndexFilter{
		NodeID:          ulid.Make(),
		Tenant:          tenant,
		SourceIndexPath: sourceIndexPath,
		ObjectPaths:     objectPaths,
	}
	var g dag.Graph[physical.Node]
	g.Add(node)
	return physical.FromGraph(g)
}

func buildSortObjectPlan(sourceObjectPath string, sortSchema []string) *physical.Plan {
	node := &physical.SortObject{
		NodeID:           ulid.Make(),
		SourceObjectPath: sourceObjectPath,
		SortSchema:       sortSchema,
	}
	var g dag.Graph[physical.Node]
	g.Add(node)
	return physical.FromGraph(g)
}

// runPlan constructs a workflow.Workflow from a single-root plan, runs it,
// and drains the pipeline. Success requires exactly one artifact followed by
// successful pipeline completion.
func runPlan(
	ctx context.Context,
	logger log.Logger,
	runner workflow.Runner,
	opts workflow.Options,
	plan *physical.Plan,
) (*v2.ResultArtifact, error) {
	wf, err := workflow.New(ctx, opts, logger, runner, plan)
	if err != nil {
		return nil, fmt.Errorf("workflow.New: %w", err)
	}
	defer wf.Close()

	pipeline, err := wf.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("workflow.Run: %w", err)
	}
	return readCompactionResult(ctx, pipeline)
}

// readCompactionResult owns the pipeline and returned records. It decodes the
// artifact before releasing its record, but does not return it until EOF.
func readCompactionResult(ctx context.Context, pipeline executor.Pipeline) (*v2.ResultArtifact, error) {
	reader := executor.TranslateEOF(pipeline)
	defer reader.Close()

	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("pipeline.Open: %w", err)
	}

	var result v2.ResultArtifact
	haveResult := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := reader.Read(ctx)
		if err != nil && rec != nil {
			rec.Release()
		}
		if errors.Is(err, io.EOF) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !haveResult {
				return nil, fmt.Errorf("compaction job produced no result record")
			}
			return &result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("pipeline.Read: %w", err)
		}
		if haveResult {
			if rec != nil {
				rec.Release()
			}
			return nil, fmt.Errorf("compaction job produced more than one result record")
		}
		err = result.FromRecordBatch(rec)
		if rec != nil {
			rec.Release()
		}
		if err != nil {
			return nil, fmt.Errorf("decode compaction result: %w", err)
		}
		haveResult = true
	}
}
