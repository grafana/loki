package physical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

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
	graph dag.Graph[physicalpb.Node]
}

// Len returns the number of nodes in the graph.
func (p *Plan) Len() int { return p.graph.Len() }

// Parent returns the parents of the given node.
func (p *Plan) Parent(n physicalpb.Node) []physicalpb.Node { return p.graph.Parents(n) }

// Children returns all child nodes of the given node.
func (p *Plan) Children(n physicalpb.Node) []physicalpb.Node { return p.graph.Children(n) }

// Roots returns all nodes that have no parents.
func (p *Plan) Roots() []physicalpb.Node { return p.graph.Roots() }

// Root returns the root node that have no parents. It returns an error if the
// plan has no or multiple root nodes.
func (p *Plan) Root() (physicalpb.Node, error) { return p.graph.Root() }

// Leaves returns all nodes that have no children.
func (p *Plan) Leaves() []physicalpb.Node { return p.graph.Leaves() }

// DFSWalk performs a depth-first traversal of the plan starting from node n.
// It applies the visitor v to each node according to the specified walk order.
// The order parameter determines if nodes are visited before their children
// ([PreOrderWalk]) or after their children ([PostOrderWalk]).
func (p *Plan) DFSWalk(n physicalpb.Node, v physicalpb.Visitor, order WalkOrder) error {
	return p.graph.Walk(n, func(n physicalpb.Node) error { return n.Accept(v) }, dag.WalkOrder(order))
}
