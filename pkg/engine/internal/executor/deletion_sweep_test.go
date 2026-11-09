package executor

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestExecuteDeletionSweep(t *testing.T) {
	run := func(t *testing.T, node *physical.DeletionSweep) error {
		var graph dag.Graph[physical.Node]
		graph.Add(node)
		pipeline := Run(t.Context(), Config{}, physical.FromGraph(graph), log.NewNopLogger())
		defer pipeline.Close()
		if err := pipeline.Open(t.Context()); err != nil {
			return err
		}
		_, err := pipeline.Read(t.Context())
		return err
	}

	t.Run("fails when the tenant is empty", func(t *testing.T) {
		err := run(t, &physical.DeletionSweep{})
		require.ErrorContains(t, err, "DeletionSweep: malformed plan")
	})

	t.Run("fails as not implemented for a valid node", func(t *testing.T) {
		err := run(t, &physical.DeletionSweep{Tenant: "t1"})
		require.ErrorIs(t, err, errDeletionSweepNotImplemented)
	})
}
