package physical

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestPrinter(t *testing.T) {
	t.Run("simple tree", func(t *testing.T) {
		p := &Plan{}

		limit := p.graph.Add(&Limit{id: "limit"})
		filter := p.graph.Add(&Filter{id: "filter"})
		scanSet := p.graph.Add(&ScanSet{
			id: "set",

			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			},
		})

		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanSet})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		p := &Plan{}

		limit1 := p.graph.Add(&Limit{id: "limit1"})
		scan1 := p.graph.Add(&DataObjScan{id: "scan1"})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit1, Child: scan1})

		limit2 := p.graph.Add(&Limit{id: "limit2"})
		scan2 := p.graph.Add(&DataObjScan{id: "scan2"})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit2, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		p := &Plan{}
		limit := p.graph.Add(&Limit{id: "limit"})
		filter1 := p.graph.Add(&Limit{id: "filter1"})
		filter2 := p.graph.Add(&Limit{id: "filter2"})
		scan := p.graph.Add(&DataObjScan{id: "scan"})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter1})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter2})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scan})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: scan})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
