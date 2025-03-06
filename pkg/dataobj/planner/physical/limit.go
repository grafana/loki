package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type Limit struct {
	input         Node
	offset, limit uint32
}

func (*Limit) ID() NodeType {
	return NodeTypeLimit
}

func (l *Limit) Children() []Node {
	return []Node{l.input}
}

func (l *Limit) Schema() schema.Schema {
	panic("unimplemented")
}

func (*Limit) isNode() {}

func (l *Limit) Accept(v Visitor) (bool, error) {
	return v.VisitLimit(l)
}
