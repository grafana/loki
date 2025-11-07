package dag_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestGraph_Walk(t *testing.T) {
	// Graph can be inspected as SVG under the following URL:
	// https://is.gd/JfDUSw
	var g dag.Graph[*testNode]

	var (
		limit1  = g.Add(&testNode{id: ulid.Make()})
		sort1   = g.Add(&testNode{id: ulid.Make()})
		filter1 = g.Add(&testNode{id: ulid.Make()})
		sort2   = g.Add(&testNode{id: ulid.Make()})
		proj1   = g.Add(&testNode{id: ulid.Make()})
		proj2   = g.Add(&testNode{id: ulid.Make()})
		proj3   = g.Add(&testNode{id: ulid.Make()})
		scan1   = g.Add(&testNode{id: ulid.Make()})
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
		expect := []ulid.ULID{limit1.ID(), sort1.ID(), filter1.ID(), proj1.ID(), scan1.ID(), sort2.ID(), proj2.ID(), proj3.ID()}

		var actual []ulid.ULID
		walker := func(n *testNode) error {
			actual = append(actual, n.ID())
			return nil
		}

		require.NoError(t, g.Walk(roots[0], walker, dag.PreOrderWalk))
		require.Len(t, actual, 8)
		require.Equal(t, expect, actual)
	})

	t.Run("post-order", func(t *testing.T) {
		expect := []ulid.ULID{scan1.ID(), proj1.ID(), filter1.ID(), proj2.ID(), sort2.ID(), proj3.ID(), sort1.ID(), limit1.ID()}

		var actual []ulid.ULID
		walker := func(n *testNode) error {
			actual = append(actual, n.ID())
			return nil
		}

		require.NoError(t, g.Walk(roots[0], walker, dag.PostOrderWalk))
		require.Len(t, actual, 8)
		require.Equal(t, expect, actual)
	})
}
