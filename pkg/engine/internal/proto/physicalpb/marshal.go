package physicalpb

import (
	fmt "fmt"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// MarshalPhysical converts a protobuf plan into standard representation.
// Returns an error if the conversion fails or is unsupported.
func (p *Plan) MarshalPhysical() (*physical.Plan, error) {
	var (
		graph   = dag.Graph[physical.Node]{}
		nodeMap = make(map[ulid.ULID]physical.Node)
	)

	for _, protoNode := range p.Nodes {
		physicalNode, err := protoNode.MarshalPhysical()
		if err != nil {
			return nil, err
		}
		graph.Add(physicalNode)
		nodeMap[physicalNode.ID()] = physicalNode
	}

	for _, edge := range p.Edges {
		parentNode, parentExists := nodeMap[ulid.ULID(edge.Parent.Value)]
		childNode, childExists := nodeMap[ulid.ULID(edge.Child.Value)]

		if !parentExists {
			return nil, fmt.Errorf("invalid edge: parent %s does not exist", edge.Parent.Value)
		} else if !childExists {
			return nil, fmt.Errorf("invalid edge: child %s does not exist", edge.Child.Value)
		}

		if err := graph.AddEdge(dag.Edge[physical.Node]{Parent: parentNode, Child: childNode}); err != nil {
			return nil, err
		}
	}

	return physical.FromGraph(graph), nil
}
