package physical

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlan(t *testing.T) {

	t.Run("empty plan does not have roots and leaves", func(t *testing.T) {
		p := &Plan{}
		require.Len(t, p.Roots(), 0)
		require.Len(t, p.Leaves(), 0)
	})

	t.Run("adding a single node makes it both root and leave node", func(t *testing.T) {
		p := &Plan{}
		p.addNode(&DataObjScan{
			id: "scan",
		})
		require.Len(t, p.Roots(), 1)
		require.Len(t, p.Leaves(), 1)
	})

	t.Run("adding an edge for nodes that do no exist fails", func(t *testing.T) {
		p := &Plan{}
		err := p.addEdge(Edge{
			Parent: &SortMerge{id: "merge"},
			Child:  &DataObjScan{id: "scan"},
		})
		require.ErrorContains(t, err, "node merge does not exist in graph")
	})

	t.Run("adding an edge for nil nodes fails", func(t *testing.T) {
		p := &Plan{}
		err := p.addEdge(Edge{})
		require.ErrorContains(t, err, "parent and child nodes must not be nil")
	})

	t.Run("test roots and leaves", func(t *testing.T) {
		p := &Plan{}
		scan1 := p.addNode(&DataObjScan{
			id: "scan1",
		})
		scan2 := p.addNode(&DataObjScan{
			id: "scan2",
		})
		merge := p.addNode(&SortMerge{
			id: "merge",
		})
		_ = p.addEdge(Edge{Parent: merge, Child: scan1})
		_ = p.addEdge(Edge{Parent: merge, Child: scan2})

		require.Equal(t, p.Len(), 3)
		require.Len(t, p.Roots(), 1)
		require.Len(t, p.Leaves(), 2)
	})

	t.Run("get node by id", func(t *testing.T) {
		p := &Plan{}
		scan1 := p.addNode(&DataObjScan{
			id: "scan1",
		})
		scan2 := p.addNode(&DataObjScan{
			id: "scan2",
		})
		merge := p.addNode(&SortMerge{
			id: "merge",
		})
		_ = p.addEdge(Edge{Parent: merge, Child: scan1})
		_ = p.addEdge(Edge{Parent: merge, Child: scan2})

		n := p.NodeByID("merge")
		require.Len(t, p.Parents(n), 0)  // no parents
		require.Len(t, p.Children(n), 2) // both scan nodes
	})

	t.Run("graph can be walked in pre-order and post-order", func(t *testing.T) {
		// Graph can be inspected as SVG under the following URL:
		// https://dreampuf.github.io/GraphvizOnline/?engine=dot#digraph%20G%20%7B%0A%20%20%20%20limit1%20-%3Emerge1%3B%0A%20%20%20%20merge1%20-%3E%20filter1%3B%0A%20%20%20%20merge1%20-%3E%20merge2%3B%0A%20%20%20%20merge1%20-%3E%20proj3%3B%0A%20%20%20%20filter1%20-%3E%20proj1%3B%0A%20%20%20%20merge2%20-%3E%20proj1%3B%0A%20%20%20%20merge2%20-%3E%20proj2%3B%0A%20%20%20%20proj1%20-%3E%20scan1%3B%0A%20%20%20%20proj2%20-%3E%20scan1%3B%0A%20%20%20%20proj3%20-%3E%20scan1%3B%0A%7D
		p := &Plan{}
		limit1 := p.addNode(&Limit{id: "limit1"})
		merge1 := p.addNode(&SortMerge{id: "merge1"})
		filter1 := p.addNode(&Filter{id: "filter1"})
		merge2 := p.addNode(&SortMerge{id: "merge2"})
		proj1 := p.addNode(&Projection{id: "projection1"})
		proj2 := p.addNode(&Projection{id: "projection2"})
		proj3 := p.addNode(&Projection{id: "projection3"})
		scan1 := p.addNode(&DataObjScan{id: "scan1"})

		_ = p.addEdge(Edge{Parent: limit1, Child: merge1})
		_ = p.addEdge(Edge{Parent: merge1, Child: filter1})
		_ = p.addEdge(Edge{Parent: merge1, Child: merge2})
		_ = p.addEdge(Edge{Parent: merge1, Child: proj3})
		_ = p.addEdge(Edge{Parent: filter1, Child: proj1})
		_ = p.addEdge(Edge{Parent: merge2, Child: proj1})
		_ = p.addEdge(Edge{Parent: merge2, Child: proj2})
		_ = p.addEdge(Edge{Parent: proj1, Child: scan1})
		_ = p.addEdge(Edge{Parent: proj2, Child: scan1})
		_ = p.addEdge(Edge{Parent: proj3, Child: scan1})

		roots := p.Roots()
		require.Len(t, roots, 1)

		t.Run("pre-order", func(t *testing.T) {
			visitor := &nodeCollectVisitor{}
			err := p.DFSWalk(roots[0], visitor, PreOrderWalk)
			require.NoError(t, err)
			require.Len(t, visitor.visited, 8)
			require.Equal(t, []string{
				"Limit.limit1",
				"SortMerge.merge1",
				"Filter.filter1",
				"Projection.projection1",
				"DataObjScan.scan1",
				"SortMerge.merge2",
				"Projection.projection2",
				"Projection.projection3",
			}, visitor.visited)
		})

		t.Run("post-order", func(t *testing.T) {
			visitor := &nodeCollectVisitor{}
			err := p.DFSWalk(roots[0], visitor, PostOrderWalk)
			require.NoError(t, err)
			require.Len(t, visitor.visited, 8)
			require.Equal(t, []string{
				"DataObjScan.scan1",
				"Projection.projection1",
				"Filter.filter1",
				"Projection.projection2",
				"SortMerge.merge2",
				"Projection.projection3",
				"SortMerge.merge1",
				"Limit.limit1",
			}, visitor.visited)
		})
	})
}
