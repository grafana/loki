package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type SortOrder int8

const (
	DESC SortOrder = iota - 1
	_
	ASC
)

type SortMerge struct {
	inputs []Node
	expr   Expression
	order  SortOrder
}

func (*SortMerge) ID() NodeType {
	return NodeTypeSortMerge
}

func (m *SortMerge) Children() []Node {
	return m.inputs
}

func (m *SortMerge) Schema() schema.Schema {
	panic("unimplemented")
}

func (*SortMerge) isNode() {}

func (m *SortMerge) Accept(v Visitor) (bool, error) {
	return v.VisitSortMerge(m)
}
