package compactor

import (
	"context"
	"errors"
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
	// call must return new node IDs, so that logs tell attempts apart.
	physicalPlan(tenant string) *physical.Plan
}

// Values of the outcome label of taskAttemptsTotal.
const (
	attemptOutcomeSuccess = "success"
	attemptOutcomeRetry   = "retry"
	attemptOutcomeFailed  = "failed"
	// attemptOutcomeCancelled counts an attempt that ended after a failed
	// sibling task cancelled the Run. The attempt records no retry or failed
	// outcome. A task that skips its attempt records no outcome.
	attemptOutcomeCancelled = "cancelled"
)

// planDispatcher runs compaction tasks concurrently and validates their
// result artifacts.
type planDispatcher struct {
	logger  log.Logger
	runPlan runFunc
	// limit caps how many tasks run at once. A value <= 0 means no cap. The
	// cap applies to each call to Run, not to the dispatcher as a whole.
	limit int
	// maxRetries is how many times a failed task runs again before it fails
	// the Run. Zero disables retries.
	maxRetries int
	attempts   *prometheus.CounterVec
}

// newPlanDispatcher returns a planDispatcher that counts task attempts in
// attempts, by actor and outcome.
func newPlanDispatcher(logger log.Logger, runPlan runFunc, limit, maxRetries int, attempts *prometheus.CounterVec) *planDispatcher {
	return &planDispatcher{
		logger:     logger,
		runPlan:    runPlan,
		limit:      limit,
		maxRetries: maxRetries,
		attempts:   attempts,
	}
}

// taskRun holds what the tasks of one Run share.
type taskRun struct {
	tenant string

	// siblingFailed is done when a task of the Run failed. Run then discards
	// every result.
	siblingFailed context.Context

	// stopRetries returns an error after the caller cancelled the Run, at
	// shutdown or when the tenant stops.
	stopRetries func() error
}

// Run executes every task and returns their artifacts in task order. Each
// task runs under the actor of the root node of its plan.
//
// A failed task runs again at once, up to maxRetries times, with a new plan.
// There is no wait between attempts: a task runs for minutes, so a short wait
// does not spread load. The tenant loop backs off after a failed phase.
//
// Run returns an error if any task fails with no retry left, and then returns
// no artifacts. The first such failure cancels the running tasks, and the
// tasks that did not start yet do not run.
//
// Cancelling ctx, at shutdown or when the tenant stops, does not stop the
// tasks. Tasks that did not start yet still start, so Run returns only after
// every task finishes. Cancelling ctx stops only the retries: a task that
// fails after ctx is done fails the Run at once.
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
	g, siblingFailed := errgroup.WithContext(context.WithoutCancel(ctx))
	if d.limit > 0 {
		g.SetLimit(d.limit)
	}
	run := taskRun{tenant: tenant, siblingFailed: siblingFailed, stopRetries: ctx.Err}
	for i, task := range tasks {
		g.Go(func() error {
			logger := log.With(d.logger, "tenant", tenant, "task", i, "tasks", len(tasks))
			artifact, err := d.runTask(run, logger, task)
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

// runTask runs task and retries it when it fails.
//
// Retries do not classify errors, because worker errors reach the coordinator
// as plain strings. A plan without a valid root node fails at once, because a
// retry builds the same plan.
func (d *planDispatcher) runTask(run taskRun, logger log.Logger, task dispatchTask) (*v2.ResultArtifact, error) {
	plan, root, err := taskPlan(run.tenant, task)
	if err != nil {
		return nil, err
	}
	// Every plan of a task has the same kind of root node, so the actor of
	// the first plan holds for every attempt.
	actor, err := rootActor(root)
	if err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		// After a sibling fails, Run discards every result, so skip the
		// attempt.
		if err := run.siblingFailed.Err(); err != nil {
			return nil, err
		}
		if attempt > 1 {
			if plan, root, err = taskPlan(run.tenant, task); err != nil {
				return nil, err
			}
		}
		artifact, err := d.runOne(run.siblingFailed, run.tenant, actor, plan)
		if err == nil {
			d.attempts.WithLabelValues(actor, attemptOutcomeSuccess).Inc()
			return artifact, nil
		}
		if run.siblingFailed.Err() != nil {
			d.attempts.WithLabelValues(actor, attemptOutcomeCancelled).Inc()
			// Run returns only its first error, so log this one here.
			lvl := level.Warn
			if errors.Is(err, context.Canceled) {
				lvl = level.Debug
			}
			lvl(logger).Log("msg", "compaction task attempt ended after a sibling task failed",
				"actor", actor, "node_id", root.ID(), "attempt", attempt, "err", err)
			return nil, err
		}
		attemptLogger := log.With(logger, "actor", actor, "node_id", root.ID(), "attempt", attempt, "max_retries", d.maxRetries, "err", err)
		if attempt > d.maxRetries {
			d.attempts.WithLabelValues(actor, attemptOutcomeFailed).Inc()
			level.Warn(attemptLogger).Log("msg", "compaction task failed, no retries left")
			return nil, fmt.Errorf("%w (attempts: %d)", err, attempt)
		}
		// Format stopErr with %v, not %w. The task failed, so the caller
		// must not mistake the error for an interruption.
		if stopErr := run.stopRetries(); stopErr != nil {
			d.attempts.WithLabelValues(actor, attemptOutcomeFailed).Inc()
			level.Warn(attemptLogger).Log("msg", "compaction task failed, retries stopped", "reason", stopErr)
			return nil, fmt.Errorf("%w (attempts: %d, retries stopped: %v)", err, attempt, stopErr)
		}
		d.attempts.WithLabelValues(actor, attemptOutcomeRetry).Inc()
		level.Warn(attemptLogger).Log("msg", "compaction task failed, retrying")
	}
}

// taskPlan returns a new plan of task and the root node of the plan.
func taskPlan(tenant string, task dispatchTask) (*physical.Plan, physical.Node, error) {
	plan := task.physicalPlan(tenant)
	root, err := plan.Root()
	if err != nil {
		return nil, nil, fmt.Errorf("compaction plan root: %w", err)
	}
	return plan, root, nil
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
