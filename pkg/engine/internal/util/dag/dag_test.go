package dag_test

import (
	"fmt"
	"testing"

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
		g.Add(&testNode{id: "scan"})

		require.Len(t, g.Roots(), 1)
		require.Len(t, g.Leaves(), 1)
	})

	t.Run("adding an edge for nodes that do not exist fails", func(t *testing.T) {
		var g dag.Graph[*testNode]

		err := g.AddEdge(dag.Edge[*testNode]{
			Parent: &testNode{id: "sort"},
			Child:  &testNode{id: "scan"},
		})
		require.ErrorContains(t, err, "node sort does not exist in graph")
	})

	t.Run("adding an edge for zero value nodes fails", func(t *testing.T) {
		var g dag.Graph[*testNode]

		err := g.AddEdge(dag.Edge[*testNode]{Parent: nil, Child: nil})
		require.ErrorContains(t, err, "parent and child nodes must not be zero values")
	})

	t.Run("test roots and leaves", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			scan1 = g.Add(&testNode{id: "scan1"})
			scan2 = g.Add(&testNode{id: "scan2"})
			merge = g.Add(&testNode{id: "sort"})
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
			parent = g.Add(&testNode{id: "parent"})
			child1 = g.Add(&testNode{id: "child1"})
			child2 = g.Add(&testNode{id: "child2"})
		)

		require.NoError(t, g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child1}))
		require.NoError(t, g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child2}))
		require.Equal(t, g.Children(parent), []*testNode{child1, child2})
	})

	t.Run("test eliminate node", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			parent = g.Add(&testNode{id: "parent"})
			middle = g.Add(&testNode{id: "middle"})
			child1 = g.Add(&testNode{id: "child1"})
			child2 = g.Add(&testNode{id: "child2"})
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
		require.Equal(t, g.Parents(child1), []*testNode{parent})
		require.Equal(t, g.Parents(child2), []*testNode{parent})
		require.Equal(t, g.Children(parent), []*testNode{child1, child2})
	})

	t.Run("test inject node", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			parent = g.Add(&testNode{id: "parent"})
			child1 = g.Add(&testNode{id: "child1"})
			child2 = g.Add(&testNode{id: "child2"})
		)

		_ = g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child1})
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: parent, Child: child2})

		newNode := g.Inject(parent, &testNode{id: "injected"})

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
			parent   = g.Add(&testNode{id: "parent"})
			existing = g.Add(&testNode{id: "existing"})
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
		g.Add(&testNode{id: "root1"})
		g.Add(&testNode{id: "root2"})

		_, err := g.Root()
		require.ErrorContains(t, err, "plan has multiple root nodes")
	})

	t.Run("test root returns single root", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			root  = g.Add(&testNode{id: "root"})
			child = g.Add(&testNode{id: "child"})
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
		nonExistent := &testNode{id: "nonexistent"}

		require.Nil(t, g.Parents(nonExistent))
		require.Nil(t, g.Children(nonExistent))
	})

	t.Run("walk with error stops traversal", func(t *testing.T) {
		var g dag.Graph[*testNode]

		var (
			root  = g.Add(&testNode{id: "root"})
			child = g.Add(&testNode{id: "child"})
		)
		_ = g.AddEdge(dag.Edge[*testNode]{Parent: root, Child: child})

		expectedErr := fmt.Errorf("test error")
		err := g.Walk(root, func(_ *testNode) error { return expectedErr }, dag.PreOrderWalk)
		require.Equal(t, expectedErr, err)
	})
}

// testNode is a simple implementation of the Node interface for testing.
type testNode struct{ id string }

func (n *testNode) ID() string { return n.id }
