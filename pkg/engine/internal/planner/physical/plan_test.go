package physical_test

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
		var p Plan

		require.Len(t, p.Roots(), 0)
		require.Len(t, p.Leaves(), 0)
	})

	t.Run("adding a single node makes it both root and leave node", func(t *testing.T) {
		var p Plan
		scanID := PlanNodeID{Value: ulid.New()}
		p.Add(&DataObjScan{Id: scanID})

		require.Len(t, p.Roots(), 1)
		require.Len(t, p.Leaves(), 1)
	})

	t.Run("adding an edge for nodes that do not exist fails", func(t *testing.T) {
		var p Plan
		scanID := PlanNodeID{Value: ulid.New()}
		sortID := PlanNodeID{Value: ulid.New()}

		err := p.AddEdge(dag.Edge[Node]{
			Parent: &SortMerge{Id: sortID},
			Child:  &DataObjScan{Id: scanID},
		})
		require.ErrorContains(t, err, fmt.Sprintf("both nodes %v and %v must already exist in the plan", sortID.Value.String(), scanID.Value.String()))
	})

	t.Run("adding an edge for zero value nodes fails", func(t *testing.T) {
		var p Plan

		err := p.AddEdge(dag.Edge[Node]{Parent: nil, Child: nil})
		require.ErrorContains(t, err, "parent and child nodes must not be zero values")
	})

	t.Run("test roots and leaves", func(t *testing.T) {
		var p Plan
		scan1ID := PlanNodeID{Value: ulid.New()}
		scan2ID := PlanNodeID{Value: ulid.New()}
		mergeID := PlanNodeID{Value: ulid.New()}

		var (
			scan1 = p.Add(&DataObjScan{Id: scan1ID})
			scan2 = p.Add(&DataObjScan{Id: scan2ID})
			merge = p.Add(&Merge{Id: mergeID})
		)

		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(merge), Child: GetNode(scan1)})
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(merge), Child: GetNode(scan2)})

		require.Equal(t, len(p.Nodes), 3)
		require.Len(t, p.Roots(), 1)
		require.Equal(t, mergeID.Value.String(), p.Roots()[0].ID())
		require.Len(t, p.Leaves(), 2)
	})

	t.Run("child ordering is preserved", func(t *testing.T) {
		var p Plan
		parentID := PlanNodeID{Value: ulid.New()}
		child1ID := PlanNodeID{Value: ulid.New()}
		child2ID := PlanNodeID{Value: ulid.New()}
		var (
			parent = p.Add(&Merge{Id: parentID})
			child1 = p.Add(&Merge{Id: child1ID})
			child2 = p.Add(&Merge{Id: child2ID})
		)

		require.NoError(t, p.AddEdge(dag.Edge[Node]{Parent: GetNode(parent), Child: GetNode(child1)}))
		require.NoError(t, p.AddEdge(dag.Edge[Node]{Parent: GetNode(parent), Child: GetNode(child2)}))
		require.Equal(t, []Node{child1.GetMerge(), child2.GetMerge()}, p.Children(GetNode(parent)))
	})

	t.Run("test eliminate node", func(t *testing.T) {
		var p Plan
		parentID := PlanNodeID{Value: ulid.New()}
		middleID := PlanNodeID{Value: ulid.New()}
		child1ID := PlanNodeID{Value: ulid.New()}
		child2ID := PlanNodeID{Value: ulid.New()}

		var (
			parent = p.Add(&Merge{Id: parentID})
			middle = p.Add(&Merge{Id: middleID})
			child1 = p.Add(&Merge{Id: child1ID})
			child2 = p.Add(&Merge{Id: child2ID})
		)

		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(parent), Child: GetNode(middle)})
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(middle), Child: GetNode(child1)})
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(middle), Child: GetNode(child2)})

		require.Equal(t, len(p.Nodes), 4)
		require.Equal(t, []Node{GetNode(parent)}, p.Parents(GetNode(middle)))
		require.Equal(t, []Node{GetNode(middle)}, p.Parents(GetNode(child1)))
		require.Equal(t, []Node{GetNode(middle)}, p.Parents(GetNode(child2)))

		p.Eliminate(GetNode(middle))

		require.Equal(t, len(p.Nodes), 3)
		require.Equal(t, []Node{GetNode(parent)}, p.Parents(GetNode(child1)))
		require.Equal(t, []Node{GetNode(parent)}, p.Parents(GetNode(child2)))
		require.Equal(t, []Node{GetNode(child1), GetNode(child2)}, p.Children(GetNode(parent)))
	})
	t.Run("test root returns error for empty graph", func(t *testing.T) {
		var p Plan

		_, err := p.Root()
		require.ErrorContains(t, err, "plan has no root node")
	})

	t.Run("test root returns error for multiple roots", func(t *testing.T) {
		root1ID := PlanNodeID{Value: ulid.New()}
		root2ID := PlanNodeID{Value: ulid.New()}
		var p Plan
		p.Add(&Merge{Id: root1ID})
		p.Add(&Merge{Id: root2ID})

		_, err := p.Root()
		require.ErrorContains(t, err, "plan has multiple root nodes")
	})

	t.Run("test root returns single root", func(t *testing.T) {
		rootID := PlanNodeID{Value: ulid.New()}
		childID := PlanNodeID{Value: ulid.New()}
		var p Plan

		var (
			root  = p.Add(&Merge{Id: rootID})
			child = p.Add(&Merge{Id: childID})
		)
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(root), Child: GetNode(child)})

		actualRoot, err := p.Root()
		require.NoError(t, err)
		require.Equal(t, GetNode(root).ID(), actualRoot.ID())
	})

	t.Run("adding zero value node is no-op", func(t *testing.T) {
		var p Plan

		p.Add(nil)
		require.Equal(t, 0, len(p.Nodes))
	})

	t.Run("parent and children methods handle missing nodes", func(t *testing.T) {
		var p Plan
		nonExistentID := PlanNodeID{Value: ulid.New()}
		nonExistent := &Merge{Id: nonExistentID}

		require.Empty(t, p.Parents(nonExistent))
		require.Empty(t, p.Children(nonExistent))
	})

	t.Run("walk with error stops traversal", func(t *testing.T) {
		rootID := PlanNodeID{Value: ulid.New()}
		childID := PlanNodeID{Value: ulid.New()}
		var p Plan

		var (
			root  = p.Add(&Merge{Id: rootID})
			child = p.Add(&Merge{Id: childID})
		)
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(root), Child: GetNode(child)})

		expectedErr := fmt.Errorf("test error")
		err := p.Walk(GetNode(root), func(Node) error { return expectedErr }, PRE_ORDER_WALK)
		require.Equal(t, expectedErr, err)
	})

	t.Run("test inject node", func(t *testing.T) {
		var p Plan
		parentID := PlanNodeID{Value: ulid.New()}
		child1ID := PlanNodeID{Value: ulid.New()}
		child2ID := PlanNodeID{Value: ulid.New()}
		injectedID := PlanNodeID{Value: ulid.New()}
		var (
			parent = p.Add(&Merge{Id: parentID})
			child1 = p.Add(&Merge{Id: child1ID})
			child2 = p.Add(&Merge{Id: child2ID})
		)

		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(parent), Child: GetNode(child1)})
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(parent), Child: GetNode(child2)})

		newNode := p.Inject(GetNode(parent), &Merge{Id: injectedID})

		require.Equal(t, len(p.Nodes), 4)
		require.Equal(t, []Node{GetNode(parent)}, p.Parents(newNode))
		require.Equal(t, []Node{newNode}, p.Parents(GetNode(child1)))
		require.Equal(t, []Node{newNode}, p.Parents(GetNode(child2)))
		require.Equal(t, []Node{newNode}, p.Children(GetNode(parent)))
		require.Equal(t, []Node{GetNode(child1), GetNode(child2)}, p.Children(newNode))
	})

	t.Run("test inject node panics if node already exists", func(t *testing.T) {
		var p Plan
		parentID := PlanNodeID{Value: ulid.New()}
		existingID := PlanNodeID{Value: ulid.New()}

		var (
			parent   = p.Add(&Merge{Id: parentID})
			existing = p.Add(&Merge{Id: existingID})
		)

		require.Panics(t, func() { p.Inject(GetNode(parent), GetNode(existing)) })
	})

}
