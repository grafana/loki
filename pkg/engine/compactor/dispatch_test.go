package compactor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestPlanDispatcherRun(t *testing.T) {
	plans := []*physical.Plan{{}, {}, {}}
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

		artifacts, err := d.Run(context.Background(), "acme", "index-merge", plans)
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{
			{Path: "indexes/out-0"},
			{Path: "indexes/out-1"},
			{Path: "indexes/out-2"},
		}, artifacts)
	})

	t.Run("runs plans as the tenant with the compaction actor", func(t *testing.T) {
		runner := &fakeRunner{}
		d := &planDispatcher{runPlan: runner.run}

		_, err := d.Run(context.Background(), "acme", "log-merge", plans[:1])
		require.NoError(t, err)
		calls := runner.snapshot()
		require.Len(t, calls, 1)
		require.Equal(t, workflow.Options{Tenant: "acme", Actor: []string{"compaction", "log-merge"}}, calls[0].opts)
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

			artifacts, err := d.Run(context.Background(), "acme", "index-merge", plans)
			require.ErrorContains(t, err, tc.wantErr)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			require.Nil(t, artifacts)
		})
	}
}
