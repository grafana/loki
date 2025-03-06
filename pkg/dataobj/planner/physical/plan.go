package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type NodeType uint32

const (
	NodeTypeIndexScan NodeType = iota
	NodeTypeDataObjScan
	NodeTypeChunkScan
	NodeTypeProjection
	NodeTypeGather
	NodeTypeSortMerge
	NodeTypeFilter
	NodeTypeLimit
)

func (t NodeType) String() string {
	switch t {
	case NodeTypeIndexScan:
		return "IndexScan"
	case NodeTypeDataObjScan:
		return "DataObjScan"
	case NodeTypeChunkScan:
		return "ChunkScan"
	case NodeTypeProjection:
		return "Projection"
	case NodeTypeGather:
		return "Gather"
	case NodeTypeSortMerge:
		return "SortMerge"
	case NodeTypeFilter:
		return "Filter"
	case NodeTypeLimit:
		return "Limit"
	default:
		return "Undefined"
	}
}

type Node interface {
	ID() NodeType
	Schema() schema.Schema
	Children() []Node

	isPlan()
}

var _ Node = (*DataObjScan)(nil)
var _ Node = (*ChunkScan)(nil)
var _ Node = (*IndexScan)(nil)
var _ Node = (*Projection)(nil)
var _ Node = (*Limit)(nil)
var _ Node = (*Filter)(nil)
var _ Node = (*SortMerge)(nil)
var _ Node = (*Gather)(nil)
