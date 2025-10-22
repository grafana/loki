package physical

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/ulid"
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
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		limit2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2Id := physicalpb.PlanNodeID{Value: ulid.New()}

		p := &physicalpb.Plan{}

		limit1 := p.Add(&physicalpb.Limit{Id: limit1Id})
		scan1 := p.Add(&physicalpb.DataObjScan{Id: scan1Id})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit1.GetLimit(), Child: scan1.GetScan()})

		limit2 := p.Add(&physicalpb.Limit{Id: limit2Id})
		scan2 := p.Add(&physicalpb.DataObjScan{Id: scan2Id})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit2.GetLimit(), Child: scan2.GetScan()})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		limitId := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		filter2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scanId := physicalpb.PlanNodeID{Value: ulid.New()}

		p := &physicalpb.Plan{}
		limit := p.Add(&physicalpb.Limit{Id: limitId})
		filter1 := p.Add(&physicalpb.Limit{Id: filter1Id})
		filter2 := p.Add(&physicalpb.Limit{Id: filter2Id})
		scan := p.Add(&physicalpb.DataObjScan{Id: scanId})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter1.GetFilter()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter2.GetFilter()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1.GetFilter(), Child: scan.GetScan()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2.GetFilter(), Child: scan.GetScan()})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
