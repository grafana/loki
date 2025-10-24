package physical

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/ulid"
)

func TestPrinter(t *testing.T) {
	t.Run("simple tree", func(t *testing.T) {
		p := &physicalpb.Plan{}
		limitID := physicalpb.PlanNodeID{Value: ulid.New()}
		filterID := physicalpb.PlanNodeID{Value: ulid.New()}
		scanSetID := physicalpb.PlanNodeID{Value: ulid.New()}
		limit := p.Add(&physicalpb.Limit{Id: limitID})
		filter := p.Add(&physicalpb.Filter{Id: filterID})
		scanSet := p.Add(&physicalpb.ScanSet{
			Id: scanSetID,

			Targets: []*physicalpb.ScanTarget{
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
			},
		})

		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(filter)})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter), Child: physicalpb.GetNode(scanSet)})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		limit2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}

		p := &physicalpb.Plan{}

		limit1 := p.Add(&physicalpb.Limit{Id: limit1ID})
		scan1 := p.Add(&physicalpb.DataObjScan{Id: scan1ID})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit1.GetLimit(), Child: scan1.GetScan()})

		limit2 := p.Add(&physicalpb.Limit{Id: limit2ID})
		scan2 := p.Add(&physicalpb.DataObjScan{Id: scan2ID})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit2.GetLimit(), Child: scan2.GetScan()})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		limitID := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		filter2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scanID := physicalpb.PlanNodeID{Value: ulid.New()}

		p := &physicalpb.Plan{}
		limit := p.Add(&physicalpb.Limit{Id: limitID})
		filter1 := p.Add(&physicalpb.Limit{Id: filter1ID})
		filter2 := p.Add(&physicalpb.Limit{Id: filter2ID})
		scan := p.Add(&physicalpb.DataObjScan{Id: scanID})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter1.GetFilter()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter2.GetFilter()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1.GetFilter(), Child: scan.GetScan()})
		_ = p.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2.GetFilter(), Child: scan.GetScan()})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
