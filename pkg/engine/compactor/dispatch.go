package compactor

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// runFunc executes one single-root physical.Plan as a workflow.
type runFunc func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error)

// planDispatcher runs compaction plans concurrently and validates their
// result artifacts.
type planDispatcher struct {
	runPlan runFunc
	// limit caps how many plans run at once. A value <= 0 means no cap.
	// it is applied locally for each call to Run, not globally for the dispatcher.
	limit int
}

// Run executes every plan and returns their artifacts in plan order.
//
// Run returns an error if any plan fails, and then returns no artifacts. The
// first failure cancels the plans that are still running.
//
// Each plan must return a valid artifact or an error:
//
//   - A valid artifact with a nil error is ready for publication. The ToC does
//     not reference it yet.
//   - A non-nil error fails the plan. Run ignores any artifact that comes
//     with it.
//   - A nil artifact with a nil error, or an artifact with an empty path, is
//     an invalid response and fails the plan.
//
// No-work decisions belong to planning, before dispatch.
func (d *planDispatcher) Run(ctx context.Context, tenant, actor string, plans []*physical.Plan) ([]v2.ResultArtifact, error) {
	artifacts := make([]v2.ResultArtifact, len(plans))
	g, gctx := errgroup.WithContext(ctx)
	if d.limit > 0 {
		g.SetLimit(d.limit)
	}
	for i, plan := range plans {
		g.Go(func() error {
			artifact, err := d.runOne(gctx, tenant, actor, plan)
			if err != nil {
				return err
			}
			artifacts[i] = *artifact
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (d *planDispatcher) runOne(ctx context.Context, tenant, actor string, plan *physical.Plan) (*v2.ResultArtifact, error) {
	opts := workflow.Options{Tenant: tenant, Actor: []string{"compaction", actor}}
	artifact, err := d.runPlan(ctx, opts, plan)
	if err != nil {
		return nil, fmt.Errorf("%s job: %w", actor, err)
	}
	if artifact == nil {
		return nil, fmt.Errorf("%s job produced no result artifact", actor)
	}
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("%s job: %w", actor, err)
	}
	return artifact, nil
}
