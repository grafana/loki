package dag_test

import (
	"fmt"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestGraph(t *testing.T) {
	t.Run("empty graph does not have roots and leaves", func(t *testing.T) {
		var g dag.Graph[*testNode]

		require.Len(t, g.Roots(), 0)
		require.Len(t, g.Leaves(), 0)
	})

	t.Run("adding a single node makes it both root and leave node", func(t *testing.T) {
		var g dag.Graph[*testNode]
		g.Add(&testNode{id: ulid.Make()})

		require.Len(t, g.Roots(), 1)
		require.Len(t, g.Leaves(), 1)
	})

	t.Run("adding an edge for nodes that do not exist fails", func(t *testing.T) {
		var g dag.Graph[*testNode]

		err := g.AddEdge(dag.Edge[*testNode]{
			Parent: &testNode{id: ulid.Make()},
			Child:  &testNode{id: ulid.Make()},
		})
		require.ErrorContains(t, err, "does not exist in graph")
	})

	t.Run("adding an edge for zero value nodes fails", func(t *testing.T) {
		var g dag.Graph[*testNode]

		err := g.AddEdge(dag.Edge[*testNode]{Parent: nil, Child: nil})
		require.ErrorContains(t, err, "parent and child nodes must not be zero values")
	})

	t.Run("test roots and leaves", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			scan1 = g.Add(&testNode{id: ulid.Make()})
			scan2 = g.Add(&testNode{id: ulid.Make()})
			merge = g.Add(&testNode{id: ulid.Make()})
		)

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: scan1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: scan2})

		require.Equal(t, g.Len(), 3)
		require.Len(t, g.Roots(), 1)
		require.Len(t, g.Leaves(), 2)
	})

	t.Run("child ordering is preserved", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			parent = g.Add(&testNode{id: ulid.Make()})
			child1 = g.Add(&testNode{id: ulid.Make()})
			child2 = g.Add(&testNode{id: ulid.Make()})
		)

		require.NoError(t, g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child1}))
		require.NoError(t, g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child2}))
		require.Equal(t, g.Children(parent), []*testNode{child1, child2})
	})

	t.Run("test eliminate node", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			parent = g.Add(&testNode{id: ulid.Make()})
			middle = g.Add(&testNode{id: ulid.Make()})
			child1 = g.Add(&testNode{id: ulid.Make()})
			child2 = g.Add(&testNode{id: ulid.Make()})
		)

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: middle})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: middle, Child: child1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: middle, Child: child2})

		require.Equal(t, g.Len(), 4)
		require.Equal(t, g.Parents(middle), []*testNode{parent})
		require.Equal(t, g.Parents(child1), []*testNode{middle})
		require.Equal(t, g.Parents(child2), []*testNode{middle})

		g.Eliminate(middle)

		require.Equal(t, g.Len(), 3)
		require.ElementsMatch(t, g.Parents(child1), []*testNode{parent})
		require.ElementsMatch(t, g.Parents(child2), []*testNode{parent})
		require.ElementsMatch(t, g.Children(parent), []*testNode{child1, child2})
	})

	t.Run("test inject node", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			parent = g.Add(&testNode{id: ulid.Make()})
			child1 = g.Add(&testNode{id: ulid.Make()})
			child2 = g.Add(&testNode{id: ulid.Make()})
		)

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child2})

		newNode := g.Inject(parent, &testNode{id: ulid.Make()})

		require.Equal(t, g.Len(), 4)
		require.Equal(t, g.Parents(newNode), []*testNode{parent})
		require.Equal(t, g.Parents(child1), []*testNode{newNode})
		require.Equal(t, g.Parents(child2), []*testNode{newNode})
		require.Equal(t, g.Children(parent), []*testNode{newNode})
		require.Equal(t, g.Children(newNode), []*testNode{child1, child2})
	})

	t.Run("test inject node panics if node already exists", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			parent   = g.Add(&testNode{id: ulid.Make()})
			existing = g.Add(&testNode{id: ulid.Make()})
		)

		require.Panics(t, func() { g.Inject(parent, existing) })
	})

	t.Run("test root returns error for empty graph", func(t *testing.T) {
		var g dag.Graph[*testNode]

		_, err := g.Root()
		require.ErrorContains(t, err, "plan has no root node")
	})

	t.Run("test root returns error for multiple roots", func(t *testing.T) {
		var g dag.Graph[*testNode]
		g.Add(&testNode{id: ulid.Make()})
		g.Add(&testNode{id: ulid.Make()})

		_, err := g.Root()
		require.ErrorContains(t, err, "plan has multiple root nodes")
	})

	t.Run("test root returns single root", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			root  = g.Add(&testNode{id: ulid.Make()})
			child = g.Add(&testNode{id: ulid.Make()})
		)
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: child})

		actualRoot, err := g.Root()
		require.NoError(t, err)
		require.Equal(t, root, actualRoot)
	})

	t.Run("adding zero value node is no-op", func(t *testing.T) {
		var g dag.Graph[*testNode]

		g.Add(nil)
		require.Equal(t, 0, g.Len())
	})

	t.Run("parent and children methods handle missing nodes", func(t *testing.T) {
		var g dag.Graph[*testNode]
		nonExistent := &testNode{id: ulid.Make()}

		require.Nil(t, g.Parents(nonExistent))
		require.Nil(t, g.Children(nonExistent))
	})

	t.Run("walk with error stops traversal", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			root  = g.Add(&testNode{id: ulid.Make()})
			child = g.Add(&testNode{id: ulid.Make()})
		)
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: child})

		expectedErr := fmt.Errorf("test error")
		err := g.Walk(root, func(_ *testNode) error { return expectedErr }, dag.PreOrderWalk)
		require.Equal(t, expectedErr, err)
	})
}

func TestEliminateBatch(t *testing.T) {
	t.Run("empty batch is a no-op", func(t *testing.T) {
		var g dag.Graph[*testNode]
		root := g.Add(&testNode{id: ulid.Make()})
		g.EliminateBatch(nil)
		require.Equal(t, 1, g.Len())
		require.Equal(t, []*testNode{root}, g.Roots())
	})

	t.Run("single node behaves like Eliminate", func(t *testing.T) {
		var g dag.Graph[*testNode]
		parent := g.Add(&testNode{id: ulid.Make()})
		middle := g.Add(&testNode{id: ulid.Make()})
		child1 := g.Add(&testNode{id: ulid.Make()})
		child2 := g.Add(&testNode{id: ulid.Make()})

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: middle})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: middle, Child: child1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: middle, Child: child2})

		g.EliminateBatch([]*testNode{middle})

		require.Equal(t, 3, g.Len())
		require.ElementsMatch(t, []*testNode{parent}, g.Parents(child1))
		require.ElementsMatch(t, []*testNode{parent}, g.Parents(child2))
		require.ElementsMatch(t, []*testNode{child1, child2}, g.Children(parent))
	})

	t.Run("eliminate all leaves with cascade to parent", func(t *testing.T) {
		// root → merge → [leaf1, leaf2, leaf3]
		// Eliminating all leaves should leave only root and merge with no children.
		var g dag.Graph[*testNode]
		root := g.Add(&testNode{id: ulid.Make()})
		merge := g.Add(&testNode{id: ulid.Make()})
		leaf1 := g.Add(&testNode{id: ulid.Make()})
		leaf2 := g.Add(&testNode{id: ulid.Make()})
		leaf3 := g.Add(&testNode{id: ulid.Make()})

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: merge})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: leaf1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: leaf2})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: leaf3})

		// Eliminate leaves + merge together (simulates cascade-batch).
		g.EliminateBatch([]*testNode{leaf1, leaf2, leaf3, merge})

		require.Equal(t, 1, g.Len())
		require.ElementsMatch(t, []*testNode{root}, g.Roots())
		require.Empty(t, g.Children(root))
	})

	t.Run("eliminate some leaves, surviving leaves reconnect to grandparent", func(t *testing.T) {
		// root → merge → [leaf1, leaf2, leaf3]
		// Eliminate merge + leaf1 + leaf2 but NOT leaf3.
		// After batch: root → leaf3.
		var g dag.Graph[*testNode]
		root := g.Add(&testNode{id: ulid.Make()})
		merge := g.Add(&testNode{id: ulid.Make()})
		leaf1 := g.Add(&testNode{id: ulid.Make()})
		leaf2 := g.Add(&testNode{id: ulid.Make()})
		leaf3 := g.Add(&testNode{id: ulid.Make()})

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: merge})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: leaf1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: leaf2})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: leaf3})

		g.EliminateBatch([]*testNode{merge, leaf1, leaf2})

		require.Equal(t, 2, g.Len())
		require.ElementsMatch(t, []*testNode{root}, g.Parents(leaf3))
		require.ElementsMatch(t, []*testNode{leaf3}, g.Children(root))
	})

	t.Run("overlapping parents are deduplicated", func(t *testing.T) {
		// root → [merge1, merge2]; merge1 → leaf; merge2 → leaf (diamond)
		// Eliminate merge1 and merge2; leaf should have root as its only parent.
		var g dag.Graph[*testNode]
		root := g.Add(&testNode{id: ulid.Make()})
		merge1 := g.Add(&testNode{id: ulid.Make()})
		merge2 := g.Add(&testNode{id: ulid.Make()})
		leaf := g.Add(&testNode{id: ulid.Make()})

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: merge1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: merge2})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge1, Child: leaf})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge2, Child: leaf})

		g.EliminateBatch([]*testNode{merge1, merge2})

		require.Equal(t, 2, g.Len())
		require.ElementsMatch(t, []*testNode{root}, g.Parents(leaf))
		require.ElementsMatch(t, []*testNode{leaf}, g.Children(root))
	})
}

// buildStarGraph creates a root → merge → N-leaf graph used in benchmarks.
func buildStarGraph(b *testing.B, n int) (dag.Graph[*testNode], *testNode, []*testNode) {
	b.Helper()
	var g dag.Graph[*testNode]
	root := g.Add(&testNode{id: ulid.Make()})
	merge := g.Add(&testNode{id: ulid.Make()})
	_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: merge})
	leaves := make([]*testNode, n)
	for i := range leaves {
		l := g.Add(&testNode{id: ulid.Make()})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: merge, Child: l})
		leaves[i] = l
	}
	return g, merge, leaves
}

func BenchmarkEliminate(b *testing.B) {
	for _, n := range []int{100, 1_000, 10_000, 200_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				g, merge, leaves := buildStarGraph(b, n)
				for _, l := range leaves {
					g.Eliminate(l)
				}
				g.Eliminate(merge)
			}
		})
	}
}

func BenchmarkEliminateBatch(b *testing.B) {
	for _, n := range []int{100, 1_000, 10_000, 200_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				g, merge, leaves := buildStarGraph(b, n)
				toRemove := append(leaves, merge)
				g.EliminateBatch(toRemove)
			}
		})
	}
}

// testNode is a simple implementation of the Node interface for testing.
type testNode struct{ id ulid.ULID }

func (n *testNode) ID() ulid.ULID { return n.id }
