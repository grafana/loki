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
		scanID := physicalpb.PlanNodeID{Value: ulid.New()}
		p.Add(&physicalpb.DataObjScan{Id: scanID})

		require.Len(t, p.Roots(), 1)
		require.Len(t, p.Leaves(), 1)
	})

	t.Run("adding an edge for nodes that do not exist fails", func(t *testing.T) {
		var p physicalpb.Plan
		scanID := physicalpb.PlanNodeID{Value: ulid.New()}
		sortID := physicalpb.PlanNodeID{Value: ulid.New()}

		err := p.AddEdge(dag.Edge[physicalpb.Node]{
			Parent: &physicalpb.SortMerge{Id: sortID},
			Child:  &physicalpb.DataObjScan{Id: scanID},
		})
		require.ErrorContains(t, err, fmt.Sprintf("both nodes %v and %v must already exist in the plan", sortID.Value.String(), scanID.Value.String()))
	})

	t.Run("adding an edge for zero value nodes fails", func(t *testing.T) {
		var p physicalpb.Plan

		err := p.AddEdge(dag.Edge[physicalpb.Node]{Parent: nil, Child: nil})
		require.ErrorContains(t, err, "parent and child nodes must not be zero values")
	})

	t.Run("test roots and leaves", func(t *testing.T) {
		var p physicalpb.Plan
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		mergeID := physicalpb.PlanNodeID{Value: ulid.New()}

		var (
			scan1 = p.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 = p.Add(&physicalpb.DataObjScan{Id: scan2ID})
			merge = p.Add(&physicalpb.Merge{Id: mergeID})
		)

		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(merge), Child: physicalpb.GetNode(scan1)})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(merge), Child: physicalpb.GetNode(scan2)})

		require.Equal(t, len(p.Nodes), 3)
		require.Len(t, p.Roots(), 1)
		require.Equal(t, mergeID.Value.String(), p.Roots()[0].ID())
		require.Len(t, p.Leaves(), 2)
	})

	t.Run("child ordering is preserved", func(t *testing.T) {
		var p physicalpb.Plan
		parentID := physicalpb.PlanNodeID{Value: ulid.New()}
		child1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		child2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		var (
			parent = p.Add(&physicalpb.Merge{Id: parentID})
			child1 = p.Add(&physicalpb.Merge{Id: child1ID})
			child2 = p.Add(&physicalpb.Merge{Id: child2ID})
		)

		require.NoError(t, p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parent), Child: physicalpb.GetNode(child1)}))
		require.NoError(t, p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parent), Child: physicalpb.GetNode(child2)}))
		require.Equal(t, []physicalpb.Node{child1.GetMerge(), child2.GetMerge()}, p.Children(physicalpb.GetNode(parent)))
	})

	t.Run("test eliminate node", func(t *testing.T) {
		var p physicalpb.Plan
		parentID := physicalpb.PlanNodeID{Value: ulid.New()}
		middleID := physicalpb.PlanNodeID{Value: ulid.New()}
		child1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		child2ID := physicalpb.PlanNodeID{Value: ulid.New()}

		var (
			parent = p.Add(&physicalpb.Merge{Id: parentID})
			middle = p.Add(&physicalpb.Merge{Id: middleID})
			child1 = p.Add(&physicalpb.Merge{Id: child1ID})
			child2 = p.Add(&physicalpb.Merge{Id: child2ID})
		)

		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parent), Child: physicalpb.GetNode(middle)})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(middle), Child: physicalpb.GetNode(child1)})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(middle), Child: physicalpb.GetNode(child2)})

		require.Equal(t, len(p.Nodes), 4)
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(parent)}, p.Parents(physicalpb.GetNode(middle)))
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(middle)}, p.Parents(physicalpb.GetNode(child1)))
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(middle)}, p.Parents(physicalpb.GetNode(child2)))

		p.Eliminate(physicalpb.GetNode(middle))

		require.Equal(t, len(p.Nodes), 3)
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(parent)}, p.Parents(physicalpb.GetNode(child1)))
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(parent)}, p.Parents(physicalpb.GetNode(child2)))
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(child1), physicalpb.GetNode(child2)}, p.Children(physicalpb.GetNode(parent)))
	})
	t.Run("test root returns error for empty graph", func(t *testing.T) {
		var p physicalpb.Plan

		_, err := p.Root()
		require.ErrorContains(t, err, "plan has no root node")
	})

	t.Run("test root returns error for multiple roots", func(t *testing.T) {
		root1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		root2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		var p physicalpb.Plan
		p.Add(&physicalpb.Merge{Id: root1ID})
		p.Add(&physicalpb.Merge{Id: root2ID})

		_, err := p.Root()
		require.ErrorContains(t, err, "plan has multiple root nodes")
	})

	t.Run("test root returns single root", func(t *testing.T) {
		rootID := physicalpb.PlanNodeID{Value: ulid.New()}
		childID := physicalpb.PlanNodeID{Value: ulid.New()}
		var p physicalpb.Plan

		var (
			root  = p.Add(&physicalpb.Merge{Id: rootID})
			child = p.Add(&physicalpb.Merge{Id: childID})
		)
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(root), Child: physicalpb.GetNode(child)})

		actualRoot, err := p.Root()
		require.NoError(t, err)
		require.Equal(t, physicalpb.GetNode(root).ID(), actualRoot.ID())
	})

	t.Run("adding zero value node is no-op", func(t *testing.T) {
		var p physicalpb.Plan

		p.Add(nil)
		require.Equal(t, 0, len(p.Nodes))
	})

	t.Run("parent and children methods handle missing nodes", func(t *testing.T) {
		var p physicalpb.Plan
		nonExistentID := physicalpb.PlanNodeID{Value: ulid.New()}
		nonExistent := &physicalpb.Merge{Id: nonExistentID}

		require.Empty(t, p.Parents(nonExistent))
		require.Empty(t, p.Children(nonExistent))
	})

	t.Run("walk with error stops traversal", func(t *testing.T) {
		rootID := physicalpb.PlanNodeID{Value: ulid.New()}
		childID := physicalpb.PlanNodeID{Value: ulid.New()}
		var p physicalpb.Plan

		var (
			root  = p.Add(&physicalpb.Merge{Id: rootID})
			child = p.Add(&physicalpb.Merge{Id: childID})
		)
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(root), Child: physicalpb.GetNode(child)})

		expectedErr := fmt.Errorf("test error")
		err := p.Walk(physicalpb.GetNode(root), func(physicalpb.Node) error { return expectedErr }, physicalpb.PRE_ORDER_WALK)
		require.Equal(t, expectedErr, err)
	})

	t.Run("test inject node", func(t *testing.T) {
		var p physicalpb.Plan
		parentID := physicalpb.PlanNodeID{Value: ulid.New()}
		child1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		child2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		injectedID := physicalpb.PlanNodeID{Value: ulid.New()}
		var (
			parent = p.Add(&physicalpb.Merge{Id: parentID})
			child1 = p.Add(&physicalpb.Merge{Id: child1ID})
			child2 = p.Add(&physicalpb.Merge{Id: child2ID})
		)

		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parent), Child: physicalpb.GetNode(child1)})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parent), Child: physicalpb.GetNode(child2)})

		newNode := p.Inject(physicalpb.GetNode(parent), &physicalpb.Merge{Id: injectedID})

		require.Equal(t, len(p.Nodes), 4)
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(parent)}, p.Parents(newNode))
		require.Equal(t, []physicalpb.Node{newNode}, p.Parents(physicalpb.GetNode(child1)))
		require.Equal(t, []physicalpb.Node{newNode}, p.Parents(physicalpb.GetNode(child2)))
		require.Equal(t, []physicalpb.Node{newNode}, p.Children(physicalpb.GetNode(parent)))
		require.Equal(t, []physicalpb.Node{physicalpb.GetNode(child1), physicalpb.GetNode(child2)}, p.Children(newNode))
	})

	t.Run("test inject node panics if node already exists", func(t *testing.T) {
		var p physicalpb.Plan
		parentID := physicalpb.PlanNodeID{Value: ulid.New()}
		existingID := physicalpb.PlanNodeID{Value: ulid.New()}

		var (
			parent   = p.Add(&physicalpb.Merge{Id: parentID})
			existing = p.Add(&physicalpb.Merge{Id: existingID})
		)

		require.Panics(t, func() { p.Inject(physicalpb.GetNode(parent), physicalpb.GetNode(existing)) })
	})

}
