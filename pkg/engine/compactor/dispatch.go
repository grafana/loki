package compactor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// runFunc executes one single-root physical.Plan as a workflow.
type runFunc func(ctx context.Context, opts workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error)

// Values of the outcome label of taskAttemptsTotal.
const (
	attemptOutcomeSuccess = "success"
	attemptOutcomeRetry   = "retry"
	attemptOutcomeFailed  = "failed"
	// attemptOutcomeCancelled counts an attempt that a failed sibling task
	// cut short, while it ran or while it waited to retry. The attempt
	// records no retry or failed outcome. A plan that skips its attempt
	// records no outcome.
	attemptOutcomeCancelled = "cancelled"
)

// planDispatcher runs compaction plans concurrently, retries the plans that
// fail, and validates their result artifacts.
type planDispatcher struct {
	logger  log.Logger
	runPlan runFunc
	// limit caps how many plans run at once. A value <= 0 means no cap. The
	// cap applies to each call to Run, not to the dispatcher as a whole.
	limit int
	// maxRetries is how many times a failed plan runs again before it fails
	// the Run. Zero disables retries.
	maxRetries int
	// retryBackoff sets the wait before each retry. Its MaxRetries is
	// ignored, because maxRetries limits the retries.
	retryBackoff backoff.Config
	attempts     *prometheus.CounterVec
}

// newPlanDispatcher returns a planDispatcher that counts task attempts in
// attempts, by actor and outcome.
func newPlanDispatcher(logger log.Logger, runPlan runFunc, limit, maxRetries int, retryBackoff backoff.Config, attempts *prometheus.CounterVec) *planDispatcher {
	return &planDispatcher{
		logger:       logger,
		runPlan:      runPlan,
		limit:        limit,
		maxRetries:   maxRetries,
		retryBackoff: retryBackoff,
		attempts:     attempts,
	}
}

// planRun holds what the plans of one Run share.
type planRun struct {
	tenant string

	// siblingCtx is done when a plan of the Run failed. Run then discards
	// every result.
	siblingCtx context.Context

	// retryCtx is the context of the caller. It is done at shutdown or when
	// the tenant stops. No retry starts after that, and a backoff wait ends
	// early.
	retryCtx context.Context
}

// Run executes every plan and returns their artifacts in plan order. Each
// plan runs under the actor of its root node.
//
// A failed plan runs again up to maxRetries times, after an exponential
// backoff. Failures often come in bursts, for example while the scheduler or
// a worker restarts, and the backoff lets a later attempt outlast the burst.
//
// Run returns an error if any plan fails with no retry left, and then returns
// no artifacts. The first such failure cancels the running plans, and the
// plans that did not start yet do not run.
//
// Cancelling ctx, at shutdown or when the tenant stops, does not stop the
// plans. Plans that did not start yet still start, so Run returns only after
// every plan finishes. Cancelling ctx stops only the retries: a plan that
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
func (d *planDispatcher) Run(ctx context.Context, tenant string, plans []*physical.Plan) ([]v2.ResultArtifact, error) {
	artifacts := make([]v2.ResultArtifact, len(plans))
	g, siblingCtx := errgroup.WithContext(context.WithoutCancel(ctx))
	if d.limit > 0 {
		g.SetLimit(d.limit)
	}
	run := planRun{tenant: tenant, siblingCtx: siblingCtx, retryCtx: ctx}
	for i, plan := range plans {
		g.Go(func() error {
			logger := log.With(d.logger, "tenant", tenant, "plan", i, "plans", len(plans))
			artifact, err := d.runPlanWithRetries(run, logger, plan)
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

// runPlanWithRetries runs plan and runs it again when it fails. Each attempt
// runs the same plan, because each workflow registers its own task and stream
// IDs with the scheduler.
//
// Retries do not classify errors, because worker errors reach the coordinator
// as plain strings. A plan without a valid root node fails at once.
func (d *planDispatcher) runPlanWithRetries(run planRun, logger log.Logger, plan *physical.Plan) (*v2.ResultArtifact, error) {
	root, err := plan.Root()
	if err != nil {
		return nil, fmt.Errorf("compaction plan root: %w", err)
	}
	actor, err := rootActor(root)
	if err != nil {
		return nil, err
	}
	logger = log.With(logger, "actor", actor, "node_id", root.ID())
	retryBackoff := backoff.New(context.Background(), d.retryBackoff)
	for attempt := 1; ; attempt++ {
		// After a sibling fails, Run discards every result, so skip the
		// attempt.
		if err := run.siblingCtx.Err(); err != nil {
			return nil, err
		}
		artifact, err := d.runOne(run.siblingCtx, run.tenant, actor, plan)
		if err == nil {
			d.attempts.WithLabelValues(actor, attemptOutcomeSuccess).Inc()
			return artifact, nil
		}
		if run.siblingCtx.Err() != nil {
			d.siblingCancelled(logger, actor, attempt, err)
			return nil, err
		}
		attemptLogger := log.With(logger, "attempt", attempt, "max_retries", d.maxRetries, "err", err)
		if attempt > d.maxRetries {
			d.attempts.WithLabelValues(actor, attemptOutcomeFailed).Inc()
			level.Warn(attemptLogger).Log("msg", "compaction task failed, no retries left")
			return nil, fmt.Errorf("%w (attempts: %d)", err, attempt)
		}
		if run.retryCtx.Err() == nil {
			delay := retryBackoff.NextDelay()
			level.Warn(attemptLogger).Log("msg", "compaction task failed, retrying after backoff", "backoff", delay)
			wait := time.NewTimer(delay)
			select {
			case <-wait.C:
			case <-run.retryCtx.Done():
			case <-run.siblingCtx.Done():
			}
			wait.Stop()
		}
		if stopErr := run.retryCtx.Err(); stopErr != nil {
			d.attempts.WithLabelValues(actor, attemptOutcomeFailed).Inc()
			level.Warn(attemptLogger).Log("msg", "compaction task failed, retries stopped", "reason", stopErr)
			// Format stopErr with %v, not %w. The task failed, so the caller
			// must not mistake the error for an interruption.
			return nil, fmt.Errorf("%w (attempts: %d, retries stopped: %v)", err, attempt, stopErr)
		}
		if run.siblingCtx.Err() != nil {
			d.siblingCancelled(logger, actor, attempt, err)
			return nil, run.siblingCtx.Err()
		}
		d.attempts.WithLabelValues(actor, attemptOutcomeRetry).Inc()
	}
}

// siblingCancelled records an attempt that a failed sibling task cut short.
// Run returns only its first error, so siblingCancelled logs err.
func (d *planDispatcher) siblingCancelled(logger log.Logger, actor string, attempt int, err error) {
	d.attempts.WithLabelValues(actor, attemptOutcomeCancelled).Inc()
	lvl := level.Warn
	if errors.Is(err, context.Canceled) {
		lvl = level.Debug
	}
	lvl(logger).Log("msg", "compaction task attempt ended after a sibling task failed", "attempt", attempt, "err", err)
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
