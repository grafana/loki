package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type NodeType uint32

const (
	NodeTypeDataObjScan NodeType = iota
	NodeTypeSortMerge
	NodeTypeProjection
	NodeTypeFilter
	NodeTypeLimit
)

func (t NodeType) String() string {
	switch t {
	case NodeTypeDataObjScan:
		return "DataObjScan"
	case NodeTypeSortMerge:
		return "SortMerge"
	case NodeTypeProjection:
		return "Projection"
	case NodeTypeFilter:
		return "Filter"
	case NodeTypeLimit:
		return "Limit"
	default:
		return "Undefined"
	}
}

type Node interface {
	Traversable

	ID() NodeType
	Schema() schema.Schema
	Children() []Node

	isNode()
}

var _ Node = (*DataObjScan)(nil)
var _ Node = (*SortMerge)(nil)
var _ Node = (*Projection)(nil)
var _ Node = (*Limit)(nil)
var _ Node = (*Filter)(nil)
