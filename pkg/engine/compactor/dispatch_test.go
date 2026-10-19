package compactor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestPlanDispatcherRun(t *testing.T) {
	plans := []*physical.Plan{
		buildIndexMergePlan("acme", time.Time{}, &compactionv2pb.TaskSpec{}),
		buildIndexMergePlan("acme", time.Time{}, &compactionv2pb.TaskSpec{}),
		buildIndexMergePlan("acme", time.Time{}, &compactionv2pb.TaskSpec{}),
	}
	pathOf := func(plan *physical.Plan) string {
		for i, p := range plans {
			if p == plan {
				return fmt.Sprintf("indexes/out-%d", i)
			}
		}
		return ""
	}

	t.Run("returns artifacts in plan order", func(t *testing.T) {
		d := &planDispatcher{runPlan: func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
			return &v2.ResultArtifact{Path: pathOf(plan)}, nil
		}}

		artifacts, err := d.Run(context.Background(), "acme", plans)
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{
			{Path: "indexes/out-0"},
			{Path: "indexes/out-1"},
			{Path: "indexes/out-2"},
		}, artifacts)
	})

	t.Run("runs each plan as the tenant with the actor of its root node", func(t *testing.T) {
		runner := &fakeRunner{}
		d := &planDispatcher{runPlan: runner.run}
		mixed := []*physical.Plan{
			buildLogMergePlan("acme", time.Time{}, &compactionv2pb.TaskSpec{}),
			buildIndexFilterPlan("acme", "indexes/source", []string{"logs/a"}),
		}

		_, err := d.Run(context.Background(), "acme", mixed)
		require.NoError(t, err)
		actors := map[*physical.Plan][]string{}
		for _, call := range runner.snapshot() {
			require.Equal(t, "acme", call.opts.Tenant)
			actors[call.plan] = call.opts.Actor
		}
		require.Equal(t, map[*physical.Plan][]string{
			mixed[0]: {"compaction", "log-merge"},
			mixed[1]: {"compaction", "index-filter"},
		}, actors)
	})

	t.Run("fails a plan whose root node is not a compaction node", func(t *testing.T) {
		var g dag.Graph[physical.Node]
		g.Add(&physical.Limit{})
		runner := &fakeRunner{}
		d := &planDispatcher{runPlan: runner.run}

		_, err := d.Run(context.Background(), "acme", []*physical.Plan{physical.FromGraph(g)})
		require.ErrorContains(t, err, "unsupported compaction plan root")
		require.Empty(t, runner.snapshot(), "an unsupported plan must not run")
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
			d := &planDispatcher{runPlan: func(_ context.Context, _ workflow.Options, plan *physical.Plan) (*v2.ResultArtifact, error) {
				if plan == plans[1] {
					return tc.artifact, tc.err
				}
				return &v2.ResultArtifact{Path: pathOf(plan)}, nil
			}}

			artifacts, err := d.Run(context.Background(), "acme", plans)
			require.ErrorContains(t, err, tc.wantErr)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			require.Nil(t, artifacts)
		})
	}
}
