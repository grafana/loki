package physical

import (
	"fmt"
	"iter"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

type NodeType uint32

const (
	NodeTypeDataObjScan NodeType = iota
	NodeTypeSortMerge
	NodeTypeProjection
	NodeTypeFilter
	NodeTypeLimit
	NodeTypeRangeAggregation
	NodeTypeVectorAggregation
	NodeTypeMerge
	NodeTypeParse
	NodeTypeCompat
	NodeTypeTopK
	NodeTypeParallelize
	NodeTypeScanSet
	NodeTypeJoin
	NodeTypePointersScan
)

func (t NodeType) String() string {
	switch t {
	case NodeTypeDataObjScan:
		return "DataObjScan"
	case NodeTypeSortMerge:
		return "SortMerge"
	case NodeTypeMerge:
		return "Merge"
	case NodeTypeProjection:
		return "Projection"
	case NodeTypeFilter:
		return "Filter"
	case NodeTypeLimit:
		return "Limit"
	case NodeTypeRangeAggregation:
		return "RangeAggregation"
	case NodeTypeVectorAggregation:
		return "VectorAggregation"
	case NodeTypeParse:
		return "Parse"
	case NodeTypeCompat:
		return "Compat"
	case NodeTypeTopK:
		return "TopK"
	case NodeTypeParallelize:
		return "Parallelize"
	case NodeTypeScanSet:
		return "ScanSet"
	case NodeTypeJoin:
		return "Join"
	case NodeTypePointersScan:
		return "PointersScan"
	default:
		return "Undefined"
	}
}

// Node represents a single operation in a physical execution plan.
// It defines the core interface that all physical plan nodes must implement.
// Each node represents a specific operation like scanning, filtering, or
// transforming data.
// Nodes can be connected to form a directed acyclic graph (DAG) representing
// the complete execution plan.
type Node interface {
	// ID returns the ULID that uniquely identifies a node in the plan.
	ID() ulid.ULID
	// Type returns the node type
	Type() NodeType
	// Clone creates a deep copy of the Node. Cloned nodes do not retain the
	// same ID.
	Clone() Node
	// isNode is a marker interface to denote a node, and only allows it to be
	// implemented within this package
	isNode()
}

// ShardableNode is a Node that can be split into multiple smaller partitions.
type ShardableNode interface {
	Node

	// Shards produces a sequence of nodes that represent a fragment of the
	// original node. Returned nodes do not need to be the same type as the
	// original node.
	//
	// Implementations must produce unique values of Node in each call to
	// Shards.
	Shards() iter.Seq[Node]
}

var _ Node = (*DataObjScan)(nil)
var _ Node = (*Projection)(nil)
var _ Node = (*Limit)(nil)
var _ Node = (*Filter)(nil)
var _ Node = (*RangeAggregation)(nil)
var _ Node = (*VectorAggregation)(nil)
var _ Node = (*ColumnCompat)(nil)
var _ Node = (*TopK)(nil)
var _ Node = (*Parallelize)(nil)
var _ Node = (*ScanSet)(nil)
var _ Node = (*Join)(nil)
var _ Node = (*PointersScan)(nil)
var _ Node = (*Merge)(nil)

func (*DataObjScan) isNode()       {}
func (*Projection) isNode()        {}
func (*Limit) isNode()             {}
func (*Filter) isNode()            {}
func (*RangeAggregation) isNode()  {}
func (*VectorAggregation) isNode() {}
func (*ColumnCompat) isNode()      {}
func (*TopK) isNode()              {}
func (*Parallelize) isNode()       {}
func (*ScanSet) isNode()           {}
func (*Join) isNode()              {}
func (*PointersScan) isNode()      {}
func (*Merge) isNode()             {}

// Plan represents a physical execution plan as a directed acyclic graph (DAG).
// It maintains the relationships between nodes, tracking parent-child connections
// and providing methods for graph traversal and manipulation.
//
// The plan structure supports operations like adding nodes and edges,
// retrieving nodes by ID, retrieving parents and children of nodes, and
// walking the graph in different orders using the depth-first-search algorithm.
type Plan struct {
	graph dag.Graph[Node]
}

// FromGraph constructs a Plan from a given DAG.
func FromGraph(graph dag.Graph[Node]) *Plan {
	return &Plan{graph: graph}
}

// Graph returns the underlying graph of the plan. Modifications to the returned
// graph will affect the Plan.
func (p *Plan) Graph() *dag.Graph[Node] { return &p.graph }

// Len returns the number of nodes in the graph.
func (p *Plan) Len() int { return p.graph.Len() }

// Parent returns the parents of the given node.
func (p *Plan) Parent(n Node) []Node { return p.graph.Parents(n) }

// Children returns all child nodes of the given node.
func (p *Plan) Children(n Node) []Node { return p.graph.Children(n) }

// Roots returns all nodes that have no parents.
func (p *Plan) Roots() []Node { return p.graph.Roots() }

// Root returns the root node that have no parents. It returns an error if the
// plan has no or multiple root nodes.
func (p *Plan) Root() (Node, error) { return p.graph.Root() }

// Leaves returns all nodes that have no children.
func (p *Plan) Leaves() []Node { return p.graph.Leaves() }

// DFSWalk performs a depth-first traversal of the plan starting from node n.
// It applies the visitor v to each node according to the specified walk order.
// The order parameter determines if nodes are visited before their children
// ([dag.PreOrderWalk]) or after their children ([dag.PostOrderWalk]).
func (p *Plan) DFSWalk(n Node, f dag.WalkFunc[Node], order dag.WalkOrder) error {
	return p.graph.Walk(n, f, order)
}

// CalculateMaxTimeRange calculates max time boundaries for the plan. Boundaries are defined
// by either the topmost RangeAggregation or by data scans.
func (p *Plan) CalculateMaxTimeRange() TimeRange {
	timeRange := TimeRange{}

	for _, root := range p.Roots() {
		_ = p.DFSWalk(root, func(n Node) error {
			switch s := n.(type) {
			case *RangeAggregation:
				timeRange.Start = s.Start
				timeRange.End = s.End
				// Return here. Topmost RangeAggregation node is what defines max time boundaries for that plan, because
				// DataObjScan below it can cover different ranges overall.
				return fmt.Errorf("stop after RangeAggregation")
			case *ScanSet:
				for _, t := range s.Targets {
					switch t.Type {
					case ScanTypeDataObject:
						timeRange = timeRange.Merge(t.DataObject.MaxTimeRange)
					case ScanTypePointers:
						timeRange = timeRange.Merge(t.Pointers.MaxTimeRange)
					}

				}
			case *DataObjScan:
				timeRange = timeRange.Merge(s.MaxTimeRange)
			case *PointersScan:
				timeRange = timeRange.Merge(s.MaxTimeRange)
			}

			return nil
		}, dag.PreOrderWalk)
	}

	return timeRange
}
