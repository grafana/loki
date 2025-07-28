package physical

import "testing"

func TestPrinter(t *testing.T) {
	t.Run("simple tree", func(t *testing.T) {
		p := &Plan{}

		limit := p.addNode(&Limit{id: "limit"})
		filter := p.addNode(&Filter{id: "filter"})
		merge := p.addNode(&SortMerge{id: "merge"})
		scan1 := p.addNode(&DataObjScan{id: "scan1"})
		scan2 := p.addNode(&DataObjScan{id: "scan2"})
		_ = p.addEdge(Edge{Parent: limit, Child: filter})
		_ = p.addEdge(Edge{Parent: filter, Child: merge})
		_ = p.addEdge(Edge{Parent: merge, Child: scan1})
		_ = p.addEdge(Edge{Parent: merge, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		p := &Plan{}

		limit1 := p.addNode(&Limit{id: "limit1"})
		scan1 := p.addNode(&DataObjScan{id: "scan1"})
		_ = p.addEdge(Edge{Parent: limit1, Child: scan1})

		limit2 := p.addNode(&Limit{id: "limit2"})
		scan2 := p.addNode(&DataObjScan{id: "scan2"})
		_ = p.addEdge(Edge{Parent: limit2, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		p := &Plan{}
		limit := p.addNode(&Limit{id: "limit"})
		filter1 := p.addNode(&Limit{id: "filter1"})
		filter2 := p.addNode(&Limit{id: "filter2"})
		scan := p.addNode(&DataObjScan{id: "scan"})
		_ = p.addEdge(Edge{Parent: limit, Child: filter1})
		_ = p.addEdge(Edge{Parent: limit, Child: filter2})
		_ = p.addEdge(Edge{Parent: filter1, Child: scan})
		_ = p.addEdge(Edge{Parent: filter2, Child: scan})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
