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
		limitId := physicalpb.PlanNodeID{Value: ulid.New()}
		filterId := physicalpb.PlanNodeID{Value: ulid.New()}
		mergeId := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2Id := physicalpb.PlanNodeID{Value: ulid.New()}

		limit := p.graph.Add(&physicalpb.Limit{Id: limitId})
		filter := p.graph.Add(&physicalpb.Filter{Id: filterId})
		merge := p.graph.Add(&physicalpb.SortMerge{Id: mergeId})
		scan1 := p.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
		scan2 := p.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: merge})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan1})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		limit2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2Id := physicalpb.PlanNodeID{Value: ulid.New()}

		p := &Plan{}

		limit1 := p.graph.Add(&physicalpb.Limit{Id: limit1Id})
		scan1 := p.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit1, Child: scan1})

		limit2 := p.graph.Add(&physicalpb.Limit{Id: limit2Id})
		scan2 := p.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit2, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		limitId := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		filter2Id := physicalpb.PlanNodeID{Value: ulid.New()}
		scanId := physicalpb.PlanNodeID{Value: ulid.New()}

		p := &Plan{}
		limit := p.graph.Add(&physicalpb.Limit{Id: limitId})
		filter1 := p.graph.Add(&physicalpb.Limit{Id: filter1Id})
		filter2 := p.graph.Add(&physicalpb.Limit{Id: filter2Id})
		scan := p.graph.Add(&physicalpb.DataObjScan{Id: scanId})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter1})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter2})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1, Child: scan})
		_ = p.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2, Child: scan})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})
}
