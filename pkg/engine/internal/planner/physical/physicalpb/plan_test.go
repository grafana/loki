package physicalpb_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/ulid"
)

func TestGraph(t *testing.T) {
	t.Run("empty graph does not have roots and leaves", func(t *testing.T) {
		var p physicalpb.Plan

		require.Len(t, p.Roots(), 0)
		require.Len(t, p.Leaves(), 0)
	})

	t.Run("adding a single node makes it both root and leave node", func(t *testing.T) {
		var p physicalpb.Plan
		scanId := physicalpb.PlanNodeID{Value: ulid.New()}
		p.Add(&physicalpb.DataObjScan{Id: scanId})

		require.Len(t, p.Roots(), 1)
		require.Len(t, p.Leaves(), 1)
	})

	t.Run("adding an edge for nodes that do not exist fails", func(t *testing.T) {
		var p physicalpb.Plan
		scanId := physicalpb.PlanNodeID{Value: ulid.New()}
		sortId := physicalpb.PlanNodeID{Value: ulid.New()}

		err := p.AddEdge(dag.Edge[physicalpb.Node]{
			Parent: &physicalpb.SortMerge{Id: sortId},
			Child:  &physicalpb.DataObjScan{Id: scanId},
		})
		require.ErrorContains(t, err, fmt.Sprintf("both nodes %v and %v must already exist in the plan", sortId.Value.String(), scanId.String()))
	})

	t.Run("adding an edge for zero value nodes fails", func(t *testing.T) {
		var p physicalpb.Plan

		err := p.AddEdge(dag.Edge[physicalpb.Node]{Parent: nil, Child: nil})
		require.ErrorContains(t, err, "parent and child nodes must not be zero values")
	})

	t.Run("test roots and leaves", func(t *testing.T) {
		var p physicalpb.Plan
		scan1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		mergeId := physicalpb.PlanNodeID{Value: ulid.New()}

		var (
			scan1 = p.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 = p.Add(&physicalpb.DataObjScan{Id: scan2Id})
			merge = p.Add(&physicalpb.Merge{Id: mergeId})
		)

		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge.GetMerge(), Child: scan1.GetScan()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge.GetMerge(), Child: scan2.GetScan()})

		require.Equal(t, len(p.Nodes), 3)
		require.Len(t, p.Roots(), 1)
		require.Len(t, p.Leaves(), 2)
	})

	t.Run("child ordering is preserved", func(t *testing.T) {
		var p physicalpb.Plan
		parentId := physicalpb.PlanNodeID{Value: ulid.New()}
		child1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		child2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		var (
			parent = p.Add(&physicalpb.Merge{Id: parentId})
			child1 = p.Add(&physicalpb.Merge{Id: child1Id})
			child2 = p.Add(&physicalpb.Merge{Id: child2Id})
		)

		require.NoError(t, p.AddEdge(dag.Edge[physicalpb.Node]{Parent: parent.GetMerge(), Child: child1.GetMerge()}))
		require.NoError(t, p.AddEdge(dag.Edge[physicalpb.Node]{Parent: parent.GetMerge(), Child: child2.GetMerge()}))
		require.Equal(t, p.Children(parent.GetMerge()), []*physicalpb.Merge{child1.GetMerge(), child2.GetMerge()})
	})

	t.Run("test eliminate node", func(t *testing.T) {
		var p physicalpb.Plan
		parentId := physicalpb.PlanNodeID{Value: ulid.New()}
		middleId := physicalpb.PlanNodeID{Value: ulid.New()}
		child1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		child2Id := physicalpb.PlanNodeID{Value: ulid.New()}

		var (
			parent = p.Add(&physicalpb.Merge{Id: parentId})
			middle = p.Add(&physicalpb.Merge{Id: middleId})
			child1 = p.Add(&physicalpb.Merge{Id: child1Id})
			child2 = p.Add(&physicalpb.Merge{Id: child2Id})
		)

		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: parent.GetMerge(), Child: middle.GetMerge()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: middle.GetMerge(), Child: child1.GetMerge()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: middle.GetMerge(), Child: child2.GetMerge()})

		require.Equal(t, len(p.Nodes), 4)
		require.Equal(t, p.Parents(middle.GetMerge()), []*physicalpb.Merge{parent.GetMerge()})
		require.Equal(t, p.Parents(child1.GetMerge()), []*physicalpb.Merge{middle.GetMerge()})
		require.Equal(t, p.Parents(child2.GetMerge()), []*physicalpb.Merge{middle.GetMerge()})

		p.Eliminate(middle.GetMerge())

		require.Equal(t, len(p.Nodes), 3)
		require.Equal(t, p.Parents(child1.GetMerge()), []*physicalpb.Merge{parent.GetMerge()})
		require.Equal(t, p.Parents(child2.GetMerge()), []*physicalpb.Merge{parent.GetMerge()})
		require.Equal(t, p.Children(parent.GetMerge()), []*physicalpb.Merge{child1.GetMerge(), child2.GetMerge()})
	})
	t.Run("test root returns error for empty graph", func(t *testing.T) {
		var p physicalpb.Plan

		_, err := p.Root()
		require.ErrorContains(t, err, "plan has no root node")
	})

	t.Run("test root returns error for multiple roots", func(t *testing.T) {
		root1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		root2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		var p physicalpb.Plan
		p.Add(&physicalpb.Merge{Id: root1Id})
		p.Add(&physicalpb.Merge{Id: root2Id})

		_, err := p.Root()
		require.ErrorContains(t, err, "plan has multiple root nodes")
	})

	t.Run("test root returns single root", func(t *testing.T) {
		rootId := physicalpb.PlanNodeID{Value: ulid.New()}
		childId := physicalpb.PlanNodeID{Value: ulid.New()}
		var p physicalpb.Plan

		var (
			root  = p.Add(&physicalpb.Merge{Id: rootId})
			child = p.Add(&physicalpb.Merge{Id: childId})
		)
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: root.GetMerge(), Child: child.GetMerge()})

		actualRoot, err := p.Root()
		require.NoError(t, err)
		require.Equal(t, root, actualRoot)
	})

	t.Run("adding zero value node is no-op", func(t *testing.T) {
		var p physicalpb.Plan

		p.Add(nil)
		require.Equal(t, 0, len(p.Nodes))
	})

	t.Run("parent and children methods handle missing nodes", func(t *testing.T) {
		var p physicalpb.Plan
		nonExistentId := physicalpb.PlanNodeID{Value: ulid.New()}
		nonExistent := &physicalpb.Merge{Id: nonExistentId}

		require.Nil(t, p.Parents(nonExistent))
		require.Nil(t, p.Children(nonExistent))
	})

	t.Run("walk with error stops traversal", func(t *testing.T) {
		rootId := physicalpb.PlanNodeID{Value: ulid.New()}
		childId := physicalpb.PlanNodeID{Value: ulid.New()}
		var p physicalpb.Plan

		var (
			root  = p.Add(&physicalpb.Merge{Id: rootId})
			child = p.Add(&physicalpb.Merge{Id: childId})
		)
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: root.GetMerge(), Child: child.GetMerge()})

		expectedErr := fmt.Errorf("test error")
		err := p.Walk(root.GetMerge(), func(physicalpb.Node) error { return expectedErr }, physicalpb.PRE_ORDER_WALK)
		require.Equal(t, expectedErr, err)
	})
}
