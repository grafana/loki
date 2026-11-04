package compactor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
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

// planFuncTask is a dispatchTask that builds its plan with the function.
type planFuncTask func(tenant string) *physical.Plan

func (f planFuncTask) physicalPlan(tenant string) *physical.Plan { return f(tenant) }

func TestPlanDispatcherRun(t *testing.T) {
	// Each task has its own window, so the window identifies the task of a
	// plan across attempts.
	tasks := []dispatchTask{
		indexMergeTask{window: time.Unix(0, 0), spec: &compactionv2pb.TaskSpec{}},
		indexMergeTask{window: time.Unix(0, 1), spec: &compactionv2pb.TaskSpec{}},
		indexMergeTask{window: time.Unix(0, 2), spec: &compactionv2pb.TaskSpec{}},
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
		return newPlanDispatcher(log.NewNopLogger(), run, 0, maxRetries, counter), counter
	}
	attempts := func(counter *prometheus.CounterVec, outcome string) float64 {
		return testutil.ToFloat64(counter.WithLabelValues("index-merge", outcome))
	}

	t.Run("returns artifacts in task order", func(t *testing.T) {
		d, _ := newDispatcher(func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			return &v2.ResultArtifact{Path: fmt.Sprintf("indexes/out-%d", taskOf(plan))}, nil
		}, 0)

		artifacts, err := d.Run(t.Context(), "acme", tasks)
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{
			{Path: "indexes/out-0"},
			{Path: "indexes/out-1"},
			{Path: "indexes/out-2"},
		}, artifacts)
	})

	t.Run("runs each task as the tenant with the actor of its root node", func(t *testing.T) {
		runner := &fakeRunner{}
		d, _ := newDispatcher(runner.run, 0)
		mixed := []dispatchTask{
			newLogMergeTask(time.Time{}, &compactionv2pb.TaskSpec{}),
			indexFilterTask{sourceIndexPath: "indexes/source", objectPaths: []string{"logs/a"}},
			planFuncTask(buildDeletionSweepPlan),
		}

		_, err := d.Run(t.Context(), "acme", mixed)
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

	t.Run("fails a task whose plan has no root node without running it", func(t *testing.T) {
		runner := &fakeRunner{}
		d, _ := newDispatcher(runner.run, 3)

		_, err := d.Run(t.Context(), "acme", []dispatchTask{planFuncTask(func(string) *physical.Plan {
			return physical.FromGraph(dag.Graph[physical.Node]{})
		})})
		require.EqualError(t, err, "compaction plan root: plan has no root node")
		require.Empty(t, runner.snapshot())
	})

	for _, tc := range []struct {
		name     string
		artifact *v2.ResultArtifact
		err      error
		wantErr  string
	}{
		{name: "fails when a task returns no artifact and no error", wantErr: "index-merge job produced no result artifact"},
		{name: "fails when a task returns an artifact with an empty path", artifact: &v2.ResultArtifact{}, wantErr: "index-merge job:"},
		{name: "fails when a task returns an error with an artifact", artifact: &v2.ResultArtifact{Path: "indexes/unpublished"}, err: errors.New("boom"), wantErr: "index-merge job: boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := newDispatcher(func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
				if taskOf(plan) == 1 {
					return tc.artifact, tc.err
				}
				return &v2.ResultArtifact{Path: "indexes/out"}, nil
			}, 0)

			artifacts, err := d.Run(t.Context(), "acme", tasks)
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

		artifacts, err := d.Run(t.Context(), "acme", tasks[:1])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/tenants/test/aa/artifact-02"}}, artifacts)
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeSuccess))
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeFailed))
	})

	t.Run("runs a failing task once and reports one attempt when retries are disabled", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		d, counter := newDispatcher(runner.run, 0)

		_, err := d.Run(t.Context(), "acme", tasks[:1])
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

		artifacts, err := d.Run(t.Context(), "acme", tasks[:1])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/out"}}, artifacts)
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeSuccess))
	})

	t.Run("builds a new plan for each attempt", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		d, _ := newDispatcher(runner.run, 2)

		_, err := d.Run(t.Context(), "acme", tasks[:1])
		require.Error(t, err)
		calls := runner.snapshot()
		require.Len(t, calls, 3)
		seen := map[ulid.ULID]struct{}{}
		for _, call := range calls {
			root, err := call.plan.Root()
			require.NoError(t, err)
			seen[root.ID()] = struct{}{}
		}
		require.Len(t, seen, 3)
	})

	t.Run("fails the run when a task fails on every attempt", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		d, counter := newDispatcher(runner.run, 2)

		artifacts, err := d.Run(t.Context(), "acme", tasks[:1])
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

		_, err := d.Run(t.Context(), "acme", tasks[:2])
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

		_, err := d.Run(ctx, "acme", tasks[:1])
		require.EqualError(t, err, "index-merge job: boom (attempts: 1, retries stopped: context canceled)")
		require.NotErrorIs(t, err, context.Canceled)
		require.Len(t, runner.snapshot(), 1)
		require.Equal(t, 0.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeFailed))
	})

	t.Run("runs the tasks to completion when the context is cancelled before they start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var runCtxErr atomic.Error
		d, _ := newDispatcher(func(runCtx context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			if err := runCtx.Err(); err != nil {
				runCtxErr.Store(err)
			}
			return &v2.ResultArtifact{Path: fmt.Sprintf("indexes/out-%d", taskOf(plan))}, nil
		}, 3)

		artifacts, err := d.Run(ctx, "acme", tasks[:2])
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

		artifacts, err := d.Run(ctx, "acme", tasks[:1])
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{{Path: "indexes/out"}}, artifacts)
		require.NoError(t, runCtxErr.Load(), "the retry must not see the cancellation of the caller")
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeRetry))
		require.Equal(t, 1.0, attempts(counter, attemptOutcomeSuccess))
	})
}
