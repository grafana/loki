package compactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

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
	outputIndexPath string,
	taskTTL time.Duration,
) *physical.Plan {
	node := &physical.IndexMerge{
		// Each call mints a fresh NodeID so racing builds don't collide on the
		// scheduler's manifest registry.
		NodeID:          ulid.Make(),
		Tenant:          tenant,
		ToCWindowStart:  window.UnixNano(),
		Runs:            task.Runs,
		OutputIndexPath: outputIndexPath,
		TaskTTL:         taskTTL,
	}
	var g dag.Graph[physical.Node]
	g.Add(node)
	return physical.FromGraph(g)
}

// runPlan constructs a workflow.Workflow from a single-root plan, runs it,
// and drains the pipeline. Returns when the workflow has fully terminated.
func runPlan(
	ctx context.Context,
	logger log.Logger,
	runner workflow.Runner,
	opts workflow.Options,
	plan *physical.Plan,
) error {
	wf, err := workflow.New(ctx, opts, logger, runner, plan)
	if err != nil {
		return fmt.Errorf("workflow.New: %w", err)
	}
	defer wf.Close()

	pipeline, err := wf.Run(ctx)
	if err != nil {
		return fmt.Errorf("workflow.Run: %w", err)
	}
	reader := executor.TranslateEOF(pipeline)
	defer reader.Close()

	if err := reader.Open(ctx); err != nil {
		return fmt.Errorf("pipeline.Open: %w", err)
	}
	for {
		_, err := reader.Read(ctx)
		// Compaction tasks are sink-only (no Arrow record batches), so draining is
		// equivalent to waiting for EOF.
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("pipeline.Read: %w", err)
		}
	}
}
