package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type Filter struct {
	input Node
	expr  Expression // filter predicate
	proj  []string
}

func (*Filter) ID() NodeType {
	return NodeTypeFilter
}

func (f *Filter) Children() []Node {
	return []Node{f.input}
}

func (f *Filter) Schema() schema.Schema {
	return f.input.Schema()
}

func (*Filter) isNode() {}

func (f *Filter) Accept(v Visitor) (bool, error) {
	return v.VisitFilter(f)
}
