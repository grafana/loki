package dag_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestGraph_Walk(t *testing.T) {
	// Graph can be inspected as SVG under the following URL:
	// https://is.gd/JfDUSw
	var g dag.Graph[*testNode]

	var (
		limit1  = g.Add(&testNode{id: "limit1"})
		sort1   = g.Add(&testNode{id: "sort1"})
		filter1 = g.Add(&testNode{id: "filter1"})
		sort2   = g.Add(&testNode{id: "sort2"})
		proj1   = g.Add(&testNode{id: "projection1"})
		proj2   = g.Add(&testNode{id: "projection2"})
		proj3   = g.Add(&testNode{id: "projection3"})
		scan1   = g.Add(&testNode{id: "scan1"})
	)

	_ = g.AddEdge(dag.Edge[*testNode]{Parent: limit1, Child: sort1})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: sort1, Child: filter1})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: sort1, Child: sort2})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: sort1, Child: proj3})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: filter1, Child: proj1})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: sort2, Child: proj1})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: sort2, Child: proj2})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: proj1, Child: scan1})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: proj2, Child: scan1})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: proj3, Child: scan1})

	roots := g.Roots()
	require.Len(t, roots, 1)

	t.Run("pre-order", func(t *testing.T) {
		expect := []string{"limit1", "sort1", "filter1", "projection1", "scan1", "sort2", "projection2", "projection3"}

		var actual []string
		walker := func(n *testNode) error {
			actual = append(actual, n.ID())
			return nil
		}

		require.NoError(t, g.Walk(roots[0], walker, dag.PreOrderWalk))
		require.Len(t, actual, 8)
		require.Equal(t, expect, actual)
	})

	t.Run("post-order", func(t *testing.T) {
		expect := []string{"scan1", "projection1", "filter1", "projection2", "sort2", "projection3", "sort1", "limit1"}

		var actual []string
		walker := func(n *testNode) error {
			actual = append(actual, n.ID())
			return nil
		}

		require.NoError(t, g.Walk(roots[0], walker, dag.PostOrderWalk))
		require.Len(t, actual, 8)
		require.Equal(t, expect, actual)
	})
}
