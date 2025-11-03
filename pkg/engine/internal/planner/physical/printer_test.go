package physical

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestPrinter(t *testing.T) {
	t.Run("simple tree", func(t *testing.T) {
		p := &Plan{}

		topk := p.graph.Add(&TopK{SortBy: &ColumnExpr{}})
		filter := p.graph.Add(&Filter{})
		scanSet := p.graph.Add(&ScanSet{
			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			},
		})

		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: topk, Child: filter})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanSet})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		p := &Plan{}

		topk1 := p.graph.Add(&TopK{SortBy: &ColumnExpr{}})
		scan1 := p.graph.Add(&DataObjScan{})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: topk1, Child: scan1})

		topk2 := p.graph.Add(&TopK{SortBy: &ColumnExpr{}})
		scan2 := p.graph.Add(&DataObjScan{})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: topk2, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		p := &Plan{}
		topk := p.graph.Add(&TopK{SortBy: &ColumnExpr{}})
		filter1 := p.graph.Add(&Filter{})
		filter2 := p.graph.Add(&Filter{})
		scan := p.graph.Add(&DataObjScan{})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: topk, Child: filter1})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: topk, Child: filter2})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scan})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: scan})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
