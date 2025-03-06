package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type Projection struct {
	input   Node
	columns []Expression
}

func (*Projection) ID() NodeType {
	return NodeTypeProjection
}

func (p *Projection) Children() []Node {
	return []Node{p.input}
}

func (p *Projection) Schema() schema.Schema {
	panic("unimplemented")
}

func (*Projection) isNode() {}

func (p *Projection) Accept(v Visitor) (bool, error) {
	return v.VisitProjection(p)
}
