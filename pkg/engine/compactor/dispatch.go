package compactor

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// runFunc executes one single-root physical.Plan as a workflow.
type runFunc func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error)

// dispatchTask is one unit of compaction work that runs as one single-root
// physical plan.
type dispatchTask interface {
	// physicalPlan returns a new single-root physical plan for the task. Each
	// call must return new node IDs, because the scheduler rejects a node ID
	// that it already saw. A retry therefore calls physicalPlan again.
	physicalPlan(tenant string) *physical.Plan
}

// Values of the outcome label of taskAttemptsTotal.
const (
	attemptOutcomeSuccess = "success"
	attemptOutcomeRetry   = "retry"
	attemptOutcomeFailed  = "failed"
)

// planDispatcher runs compaction tasks concurrently and validates their
// result artifacts.
type planDispatcher struct {
	logger  log.Logger
	runPlan runFunc
	// limit caps how many tasks run at once. A value <= 0 means no cap.
	// it is applied locally for each call to Run, not globally for the dispatcher.
	limit int
	// maxRetries is how many times a failed task runs again before it fails
	// the Run. Zero disables retries.
	maxRetries int
	attempts   *prometheus.CounterVec
}

func newPlanDispatcher(logger log.Logger, runPlan runFunc, limit, maxRetries int, metrics *coordinatorMetrics) *planDispatcher {
	return &planDispatcher{
		logger:     logger,
		runPlan:    runPlan,
		limit:      limit,
		maxRetries: maxRetries,
		attempts:   metrics.taskAttemptsTotal,
	}
}

// Run executes every task and returns their artifacts in task order. Each
// task runs under the actor of the root node of its plan.
//
// A failed task runs again at once, up to maxRetries times, with a new plan.
// There is no wait between attempts: a task runs for minutes, so a short wait
// does not spread load. The tenant loop backs off after a failed Run.
//
// Run returns an error if any task fails after its retries, and then returns
// no artifacts. The first such failure cancels the tasks that are still
// running.
//
// Each attempt must return a valid artifact or an error:
//
//   - A valid artifact with a nil error is ready for publication. The ToC does
//     not reference it yet.
//   - A non-nil error fails the attempt. Run ignores any artifact that comes
//     with it.
//   - A nil artifact with a nil error, or an artifact with an empty path, is
//     an invalid response and fails the attempt.
//
// No-work decisions belong to planning, before dispatch.
func (d *planDispatcher) Run(ctx context.Context, tenant string, tasks []dispatchTask) ([]v2.ResultArtifact, error) {
	artifacts := make([]v2.ResultArtifact, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	if d.limit > 0 {
		g.SetLimit(d.limit)
	}
	for i, task := range tasks {
		g.Go(func() error {
			logger := log.With(d.logger, "tenant", tenant, "task", i, "tasks", len(tasks))
			artifact, err := d.runTask(gctx, logger, tenant, task)
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

// runTask runs task until an attempt succeeds, the retries run out, or ctx is
// done. Retries do not classify errors, because worker errors reach the
// coordinator as plain strings. A plan without a valid root node fails at
// once, because a retry builds the same plan.
func (d *planDispatcher) runTask(ctx context.Context, logger log.Logger, tenant string, task dispatchTask) (*v2.ResultArtifact, error) {
	for attempt := 1; ; attempt++ {
		// A done ctx means shutdown or a failed sibling task, so an attempt
		// cannot succeed. Stop before the attempt registers a workflow.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		plan := task.physicalPlan(tenant)
		root, err := plan.Root()
		if err != nil {
			return nil, fmt.Errorf("compaction plan root: %w", err)
		}
		actor, err := rootActor(root)
		if err != nil {
			return nil, err
		}
		artifact, err := d.runOne(ctx, tenant, actor, plan)
		if err == nil {
			d.attempts.WithLabelValues(actor, attemptOutcomeSuccess).Inc()
			return artifact, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		attemptLogger := log.With(logger, "actor", actor, "node_id", root.ID(), "attempt", attempt, "max_retries", d.maxRetries, "err", err)
		if attempt > d.maxRetries {
			d.attempts.WithLabelValues(actor, attemptOutcomeFailed).Inc()
			level.Warn(attemptLogger).Log("msg", "compaction task failed, no retries left")
			return nil, fmt.Errorf("%w (attempts: %d)", err, attempt)
		}
		d.attempts.WithLabelValues(actor, attemptOutcomeRetry).Inc()
		level.Warn(attemptLogger).Log("msg", "compaction task failed, retrying")
	}
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

// rootActor returns the scheduler actor for a compaction plan, based on the
// type of its root node. The scheduler uses the actor to queue tasks fairly,
// so each kind of compaction task gets its own queue.
func rootActor(root physical.Node) (string, error) {
	switch root.(type) {
	case *physical.IndexMerge:
		return "index-merge", nil
	case *physical.LogMerge:
		return "log-merge", nil
	case *physical.IndexFilter:
		return "index-filter", nil
	case *physical.DeletionSweep:
		return "deletion-sweep", nil
	default:
		return "", fmt.Errorf("unsupported compaction plan root %T", root)
	}
}
