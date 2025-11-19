package physicalpb

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/ulid"
)

// UnmarshalPhysical reads from into p. Returns an error if the conversion fails
// or is unsupported.
func (p *Plan) UnmarshalPhysical(from *physical.Plan) error {
	graph := from.Graph()

	*p = Plan{
		Nodes: make([]*Node, 0, graph.Len()),
		Edges: make([]*PlanEdge, 0),
	}

	for node := range graph.Nodes() {
		protoNode := &Node{}
		if err := protoNode.UnmarshalPhysical(node); err != nil {
			return err
		}
		p.Nodes = append(p.Nodes, protoNode)
	}

	for node := range graph.Nodes() {
		for _, child := range graph.Children(node) {
			edge := &PlanEdge{
				Parent: NodeID{Value: ulid.ULID(node.ID())},
				Child:  NodeID{Value: ulid.ULID(child.ID())},
			}
			p.Edges = append(p.Edges, edge)
		}
	}

	return nil
}
