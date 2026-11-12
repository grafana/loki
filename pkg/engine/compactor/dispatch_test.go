package compactor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestPlanDispatcherRunToCompletion(t *testing.T) {
	// Each plan has its own window, so a runner can tell the plans apart.
	plans := []*physical.Plan{
		buildIndexMergePlan("acme", time.Unix(0, 0), &compactionv2pb.TaskSpec{}),
		buildIndexMergePlan("acme", time.Unix(0, 1), &compactionv2pb.TaskSpec{}),
		buildIndexMergePlan("acme", time.Unix(0, 2), &compactionv2pb.TaskSpec{}),
	}
	// taskOf panics instead of failing t, because runners call it outside
	// the test goroutine.
	taskOf := func(plan *physical.Plan) int64 {
		root, err := plan.Root()
		if err != nil {
			panic(err)
		}
		return root.(*physical.IndexMerge).ToCWindowStart
	}
	newDispatcher := func(run runFunc, maxRetries int) (*planDispatcher, *prometheus.CounterVec) {
		counter := newCoordinatorMetrics(prometheus.NewRegistry()).taskAttemptsTotal
		retryBackoff := backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
		return newPlanDispatcher(log.NewNopLogger(), run, 0, maxRetries, retryBackoff, counter), counter
	}
	attempts := func(counter *prometheus.CounterVec, outcome string) float64 {
		return testutil.ToFloat64(counter.WithLabelValues("index-merge", outcome))
	}

	t.Run("returns artifacts in plan order", func(t *testing.T) {
		d, _ := newDispatcher(func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			return &v2.ResultArtifact{Path: fmt.Sprintf("indexes/out-%d", taskOf(plan))}, nil
		}, 0)

		artifacts, err := d.RunToCompletion(t.Context(), "acme", plans)
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{
			{Path: "indexes/out-0"},
			{Path: "indexes/out-1"},
			{Path: "indexes/out-2"},
		}, artifacts)
	})

	t.Run("runs each plan as the tenant with the actor of its root node", func(t *testing.T) {
		runner := &fakeRunner{}
		d, _ := newDispatcher(runner.run, 0)
		mixed := []*physical.Plan{
			buildLogMergePlan("acme", time.Time{}, &compactionv2pb.TaskSpec{}),
			buildIndexFilterPlan("acme", "indexes/source", []string{"logs/a"}),
			buildDeletionSweepPlan("acme"),
		}

		_, err := d.RunToCompletion(t.Context(), "acme", mixed)
		require.NoError(t, err)
		var actors [][]string
		for _, call := range runner.snapshot() {
			require.Equal(t, "acme", call.opts.Tenant)
			actors = append(actors, call.opts.Actor)
		}
		require.ElementsMatch(t, [][]string{
			{"compaction", "log-merge"},
			{"compaction", "index-filter"},
			{"compaction", "deletion-sweep"},
		}, actors)
	})

	t.Run("fails a plan without a root node", func(t *testing.T) {
		runner := &fakeRunner{}
		d, _ := newDispatcher(runner.run, 3)

		_, err := d.RunToCompletion(t.Context(), "acme", []*physical.Plan{physical.FromGraph(dag.Graph[physical.Node]{})})
		require.EqualError(t, err, "compaction plan root: plan has no root node")
		require.Empty(t, runner.snapshot())
	})

	for _, tc := range []struct {
		name     string
		artifact *v2.ResultArtifact
		err      error
		wantErr  string
	}{
		{name: "fails when a plan returns no artifact and no error", wantErr: "index-merge job produced no result artifact"},
		{name: "fails when a plan returns an artifact with an empty path", artifact: &v2.ResultArtifact{}, wantErr: "index-merge job:"},
		{name: "fails when a plan returns an error with an artifact", artifact: &v2.ResultArtifact{Path: "indexes/unpublished"}, err: errors.New("boom"), wantErr: "index-merge job: boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := newDispatcher(func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
				if taskOf(plan) == 1 {
					return tc.artifact, tc.err
				}
				return &v2.ResultArtifact{Path: "indexes/out"}, nil
			}, 0)

			artifacts, err := d.RunToCompletion(t.Context(), "acme", plans)
			require.ErrorContains(t, err, tc.wantErr)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			require.Nil(t, artifacts)
		})
	}

	t.Run("returns the artifact of a retry after a failed attempt", func(t *testing.T) {
		runner := &fakeRunner{failOnCall: 1}
		d, counter := newDispatcher(runner.run, 1)

		artifacts, err := d.RunToCompletion(t.Context(), "acme", plans[:1])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/tenants/test/aa/artifact-02"}}, artifacts)
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeSuccess))
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeFailed))
	})

	t.Run("runs a failing task once and reports one attempt when retries are disabled", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		d, counter := newDispatcher(runner.run, 0)

		_, err := d.RunToCompletion(t.Context(), "acme", plans[:1])
		require.EqualError(t, err, "index-merge job: boom (attempts: 1)")
		require.Len(t, runner.snapshot(), 1)
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeFailed))
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeRetry))
	})

	t.Run("retries a task whose attempt returned no artifact", func(t *testing.T) {
		var calls atomic.Int32
		d, counter := newDispatcher(func(context.Context, workflow.Options, *physical.Plan) (*v2.ResultArtifact, error) {
			if calls.Inc() == 1 {
				return nil, nil
			}
			return &v2.ResultArtifact{Path: "indexes/out"}, nil
		}, 1)

		artifacts, err := d.RunToCompletion(t.Context(), "acme", plans[:1])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/out"}}, artifacts)
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeSuccess))
	})

	t.Run("runs the same plan on every attempt", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		d, _ := newDispatcher(runner.run, 2)

		_, err := d.RunToCompletion(t.Context(), "acme", plans[:1])
		require.Error(t, err)
		calls := runner.snapshot()
		require.Len(t, calls, 3)
		for _, call := range calls {
			require.Same(t, plans[0], call.plan)
		}
	})

	t.Run("fails the run when a task fails on every attempt", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		d, counter := newDispatcher(runner.run, 2)

		artifacts, err := d.RunToCompletion(t.Context(), "acme", plans[:1])
		require.EqualError(t, err, "index-merge job: boom (attempts: 3)")
		require.Nil(t, artifacts)
		require.Len(t, runner.snapshot(), 3)
		require.Equal(t, 2.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeFailed))
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeSuccess))
	})

	t.Run("does not retry a task that a failed sibling task cancelled", func(t *testing.T) {
		started := make(chan struct{})
		var siblingCalls atomic.Int32
		d, counter := newDispatcher(func(ctx context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			if taskOf(plan) == 0 {
				<-started
				return nil, errors.New("boom")
			}
			if siblingCalls.Inc() == 1 {
				close(started)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}, 1)

		_, err := d.RunToCompletion(t.Context(), "acme", plans[:2])
		require.EqualError(t, err, "index-merge job: boom (attempts: 2)")
		require.Equal(t, int32(1), siblingCalls.Load())
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeFailed))
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeSuccess))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeCancelled))
	})

	t.Run("fails a task without a retry when it fails after the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		runner := &fakeRunner{respond: func(context.Context, workflow.Options, *physical.Plan) (*v2.ResultArtifact, error) {
			cancel()
			return nil, errors.New("boom")
		}}
		d, counter := newDispatcher(runner.run, 3)

		_, err := d.RunToCompletion(ctx, "acme", plans[:1])
		require.EqualError(t, err, "index-merge job: boom (attempts: 1, retries stopped: context canceled)")
		require.NotErrorIs(t, err, context.Canceled)
		require.Len(t, runner.snapshot(), 1)
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeFailed))
	})

	t.Run("runs the plans to completion when the context is cancelled before they start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var runCtxErr atomic.Error
		d, _ := newDispatcher(func(runCtx context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			if err := runCtx.Err(); err != nil {
				runCtxErr.Store(err)
			}
			return &v2.ResultArtifact{Path: fmt.Sprintf("indexes/out-%d", taskOf(plan))}, nil
		}, 3)

		artifacts, err := d.RunToCompletion(ctx, "acme", plans[:2])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/out-0"}, {Path: "indexes/out-1"}}, artifacts)
		require.NoError(t, runCtxErr.Load(), "attempts must not see the cancellation of the caller")
	})

	t.Run("returns the artifact of a retry that is running when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var calls atomic.Int32
		var runCtxErr atomic.Error
		d, counter := newDispatcher(func(runCtx context.Context, _ workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
			if calls.Inc() == 1 {
				return nil, errors.New("boom")
			}
			cancel()
			runCtxErr.Store(runCtx.Err())
			return &v2.ResultArtifact{Path: "indexes/out"}, nil
		}, 2)

		artifacts, err := d.RunToCompletion(ctx, "acme", plans[:1])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/out"}}, artifacts)
		require.NoError(t, runCtxErr.Load(), "the retry must not see the cancellation of the caller")
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeSuccess))
	})

	t.Run("waits for the backoff before each retry", func(t *testing.T) {
		counter := newCoordinatorMetrics(prometheus.NewRegistry()).taskAttemptsTotal
		retryBackoff := backoff.Config{MinBackoff: 50 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
		runner := &fakeRunner{err: errors.New("boom")}
		d := newPlanDispatcher(log.NewNopLogger(), runner.run, 0, 2, retryBackoff, counter)

		start := time.Now()
		_, err := d.RunToCompletion(t.Context(), "acme", plans[:1])
		require.EqualError(t, err, "index-merge job: boom (attempts: 3)")
		require.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
		require.Len(t, runner.snapshot(), 3)
	})

	t.Run("fails a task without another attempt when the context is cancelled during the backoff", func(t *testing.T) {
		counter := newCoordinatorMetrics(prometheus.NewRegistry()).taskAttemptsTotal
		retryBackoff := backoff.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour}
		ctx, cancel := context.WithCancel(t.Context())
		runner := &fakeRunner{respond: func(context.Context, workflow.Options, *physical.Plan) (*v2.ResultArtifact, error) {
			time.AfterFunc(10*time.Millisecond, cancel)
			return nil, errors.New("boom")
		}}
		d := newPlanDispatcher(log.NewNopLogger(), runner.run, 0, 3, retryBackoff, counter)

		_, err := d.RunToCompletion(ctx, "acme", plans[:1])
		require.EqualError(t, err, "index-merge job: boom (attempts: 1, retries stopped: context canceled)")
		require.NotErrorIs(t, err, context.Canceled)
		require.Len(t, runner.snapshot(), 1)
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeFailed))
	})
}

func TestPlanDispatcherRunCancellable(t *testing.T) {
	plans := []*physical.Plan{
		buildIndexMergePlan("acme", time.Unix(0, 0), &compactionv2pb.TaskSpec{}),
		buildIndexMergePlan("acme", time.Unix(0, 1), &compactionv2pb.TaskSpec{}),
	}
	taskOf := func(plan *physical.Plan) int64 {
		root, err := plan.Root()
		if err != nil {
			panic(err)
		}
		return root.(*physical.IndexMerge).ToCWindowStart
	}
	newDispatcher := func(run runFunc, maxRetries int) (*planDispatcher, *prometheus.CounterVec) {
		counter := newCoordinatorMetrics(prometheus.NewRegistry()).taskAttemptsTotal
		retryBackoff := backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
		return newPlanDispatcher(log.NewNopLogger(), run, 0, maxRetries, retryBackoff, counter), counter
	}

	t.Run("returns the artifacts in plan order when every plan succeeds", func(t *testing.T) {
		d, _ := newDispatcher(func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			return &v2.ResultArtifact{Path: fmt.Sprintf("indexes/out-%d", taskOf(plan))}, nil
		}, 0)

		artifacts, err := d.RunCancellable(t.Context(), "acme", plans)
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/out-0"}, {Path: "indexes/out-1"}}, artifacts)
	})

	t.Run("stops a running plan and returns an error when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var runCtxErr atomic.Error
		d, _ := newDispatcher(func(runCtx context.Context, _ workflow.Options, _ *physical.Plan) (*v2.ResultArtifact, error) {
			cancel()
			<-runCtx.Done()
			runCtxErr.Store(runCtx.Err())
			return nil, runCtx.Err()
		}, 3)

		artifacts, err := d.RunCancellable(ctx, "acme", plans[:1])
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, artifacts)
		require.ErrorIs(t, runCtxErr.Load(), context.Canceled, "the plan must see the cancellation of the caller")
	})

	t.Run("runs no plan when the context is cancelled before the run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		runner := &fakeRunner{}
		d, _ := newDispatcher(runner.run, 3)

		_, err := d.RunCancellable(ctx, "acme", plans)
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, runner.snapshot())
	})
}

func TestPlanDispatcherRunPlanWithRetries(t *testing.T) {
	t.Run("counts a cancelled attempt without another attempt when a sibling task fails during the backoff", func(t *testing.T) {
		counter := newCoordinatorMetrics(prometheus.NewRegistry()).taskAttemptsTotal
		retryBackoff := backoff.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour}
		siblingCtx, siblingFailed := context.WithCancel(t.Context())
		runner := &fakeRunner{respond: func(context.Context, workflow.Options, *physical.Plan) (*v2.ResultArtifact, error) {
			time.AfterFunc(10*time.Millisecond, siblingFailed)
			return nil, errors.New("boom")
		}}
		d := newPlanDispatcher(log.NewNopLogger(), runner.run, 0, 3, retryBackoff, counter)
		run := planRun{tenant: "acme", siblingCtx: siblingCtx, retryCtx: t.Context()}

		_, err := d.runPlanWithRetries(run, log.NewNopLogger(), buildIndexMergePlan("acme", time.Time{}, &compactionv2pb.TaskSpec{}))
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, runner.snapshot(), 1)
		require.Equal(t, 1.0, testutil.ToFloat64(counter.WithLabelValues("index-merge", attemptOutcomeCancelled)))
		require.Equal(t, 0.0, testutil.ToFloat64(counter.WithLabelValues("index-merge", attemptOutcomeRetry)))
		require.Equal(t, 0.0, testutil.ToFloat64(counter.WithLabelValues("index-merge", attemptOutcomeFailed)))
	})
}
