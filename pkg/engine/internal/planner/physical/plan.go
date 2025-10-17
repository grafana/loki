package physical

import (
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
	// ID returns a string that uniquely identifies a node in the plan
	ID() string
	// Type returns the node type
	Type() NodeType
	// Clone creates a deep copy of the Node. Cloned nodes do not retain the
	// same ID.
	Clone() Node
	// Accept allows the object to be visited by a [Visitor] as part of the
	// visitor pattern. It typically calls back to the appropriate Visit method
	// on the Visitor for the concrete type being visited.
	Accept(Visitor) error
	// isNode is a marker interface to denote a node, and only allows it to be
	// implemented within this package
	isNode()
}

var _ Node = (*DataObjScan)(nil)
var _ Node = (*Projection)(nil)
var _ Node = (*Limit)(nil)
var _ Node = (*Filter)(nil)
var _ Node = (*RangeAggregation)(nil)
var _ Node = (*VectorAggregation)(nil)
var _ Node = (*ParseNode)(nil)
var _ Node = (*ColumnCompat)(nil)
var _ Node = (*TopK)(nil)
var _ Node = (*Parallelize)(nil)
var _ Node = (*ScanSet)(nil)

func (*DataObjScan) isNode()       {}
func (*Projection) isNode()        {}
func (*Limit) isNode()             {}
func (*Filter) isNode()            {}
func (*RangeAggregation) isNode()  {}
func (*VectorAggregation) isNode() {}
func (*ParseNode) isNode()         {}
func (*ColumnCompat) isNode()      {}
func (*TopK) isNode()              {}
func (*Parallelize) isNode()       {}
func (*ScanSet) isNode()           {}

// WalkOrder defines the order for how a node and its children are visited.
type WalkOrder uint8

const (
	// PreOrderWalk processes the current vertex before visiting any of its
	// children.
	PreOrderWalk WalkOrder = iota

	// PostOrderWalk processes the current vertex after visiting all of its
	// children.
	PostOrderWalk
)

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
// ([PreOrderWalk]) or after their children ([PostOrderWalk]).
func (p *Plan) DFSWalk(n Node, v Visitor, order WalkOrder) error {
	return p.graph.Walk(n, func(n Node) error { return n.Accept(v) }, dag.WalkOrder(order))
}
