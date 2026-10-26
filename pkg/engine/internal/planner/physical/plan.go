package physical

import (
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// NodeType represents the type of a node in the physical execution plan.
type NodeType uint32

const (
	NodeTypeInvalid     NodeType = iota // NodeTypeInvalid is an illegal NodeType.
	NodeTypeIndexMerge                  // NodeTypeIndexMerge represents an [IndexMerge].
	NodeTypeLogMerge                    // NodeTypeLogMerge represents a [LogMerge].
	NodeTypeIndexFilter                 // NodeTypeIndexFilter represents an [IndexFilter].
)

// String returns a string representation of the NodeType.
func (t NodeType) String() string {
	switch t {
	case NodeTypeIndexMerge:
		return "IndexMerge"
	case NodeTypeLogMerge:
		return "LogMerge"
	case NodeTypeIndexFilter:
		return "IndexFilter"
	default:
		return "Invalid"
	}
}

// Node represents a single operation in a physical execution plan.
// Nodes can be connected to form a directed acyclic graph (DAG) representing
// the complete execution plan.
type Node interface {
	// ID returns the ULID that uniquely identifies a node in the plan.
	ID() ulid.ULID
	// Type returns the node type
	Type() NodeType
	// isNode is a marker interface to denote a node, and only allows it to be
	// implemented within this package
	isNode()
}

var _ Node = (*IndexMerge)(nil)
var _ Node = (*LogMerge)(nil)
var _ Node = (*IndexFilter)(nil)

func (*IndexMerge) isNode()  {}
func (*LogMerge) isNode()    {}
func (*IndexFilter) isNode() {}

// Plan represents a physical execution plan as a directed acyclic graph (DAG).
// It maintains the parent-child relationships between nodes.
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

// Children returns all child nodes of the given node.
func (p *Plan) Children(n Node) []Node { return p.graph.Children(n) }

// Root returns the root node that have no parents. It returns an error if the
// plan has no or multiple root nodes.
func (p *Plan) Root() (Node, error) { return p.graph.Root() }
