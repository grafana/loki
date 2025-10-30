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
		limitID := PlanNodeID{Value: ulid.New()}
		filterID := PlanNodeID{Value: ulid.New()}
		scanSetID := PlanNodeID{Value: ulid.New()}
		limit := p.Add(&Limit{Id: limitID})
		filter := p.Add(&Filter{Id: filterID})
		scanSet := p.Add(&ScanSet{
			Id: scanSetID,

			Targets: []*ScanTarget{
				{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
			},
		})

		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(limit), Child: GetNode(filter)})
		_ = p.AddEdge(dag.Edge[Node]{Parent: GetNode(filter), Child: GetNode(scanSet)})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		limit1ID := PlanNodeID{Value: ulid.New()}
		limit2ID := PlanNodeID{Value: ulid.New()}
		scan1ID := PlanNodeID{Value: ulid.New()}
		scan2ID := PlanNodeID{Value: ulid.New()}

		p := &Plan{}

		limit1 := p.Add(&Limit{Id: limit1ID})
		scan1 := p.Add(&DataObjScan{Id: scan1ID})
		_ = p.AddEdge(dag.Edge[Node]{Parent: limit1.GetLimit(), Child: scan1.GetScan()})

		limit2 := p.Add(&Limit{Id: limit2ID})
		scan2 := p.Add(&DataObjScan{Id: scan2ID})
		_ = p.AddEdge(dag.Edge[Node]{Parent: limit2.GetLimit(), Child: scan2.GetScan()})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		limitID := PlanNodeID{Value: ulid.New()}
		filter1ID := PlanNodeID{Value: ulid.New()}
		filter2ID := PlanNodeID{Value: ulid.New()}
		scanID := PlanNodeID{Value: ulid.New()}

		p := &Plan{}
		limit := p.Add(&Limit{Id: limitID})
		filter1 := p.Add(&Limit{Id: filter1ID})
		filter2 := p.Add(&Limit{Id: filter2ID})
		scan := p.Add(&DataObjScan{Id: scanID})
		_ = p.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: filter1.GetFilter()})
		_ = p.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: filter2.GetFilter()})
		_ = p.AddEdge(dag.Edge[Node]{Parent: filter1.GetFilter(), Child: scan.GetScan()})
		_ = p.AddEdge(dag.Edge[Node]{Parent: filter2.GetFilter(), Child: scan.GetScan()})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
