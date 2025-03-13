package physical

import (
	"errors"
	"fmt"
	"slices"
)

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
	// Accept allows the object to be visited by a [Visitor] as part of the
	// visitor pattern. It typically calls back to the appropriate Visit method
	// on the Visitor for the concrete type being visited.
	Accept(Visitor) error
	// isNode is a marker interface to denote a node, and only allows it to be
	// implemented within this package
	isNode()
}

var _ Node = (*DataObjScan)(nil)
var _ Node = (*SortMerge)(nil)
var _ Node = (*Projection)(nil)
var _ Node = (*Limit)(nil)
var _ Node = (*Filter)(nil)

func (*DataObjScan) isNode() {}
func (*SortMerge) isNode()   {}
func (*Projection) isNode()  {}
func (*Limit) isNode()       {}
func (*Filter) isNode()      {}

// Edge is a directed connection (parent-child relation) between a two nodes.
type Edge struct {
	Parent, Child Node
}

// WalkOrder defined the order in which current vertex and its children are
// visited.
// Pre-order: Process the current vertex before visiting any of its children.
// Post-order: Process the current vertex after visiting all of its children.
type WalkOrder uint8

const (
	PreOrderWalk WalkOrder = iota
	PostOrderWalk
)

type nodeSet map[Node]struct{}

func (s nodeSet) add(node Node) {
	if node == nil {
		return
	}
	s[node] = struct{}{}
}

func (s nodeSet) remove(node Node) {
	if s.contains(node) {
		delete(s, node)
	}
}

func (s nodeSet) contains(node Node) bool {
	if node == nil {
		return false
	}
	_, ok := s[node]
	return ok
}

func (s nodeSet) sorted() []Node {
	nodes := make([]Node, 0, len(s))
	for node := range s {
		nodes = append(nodes, node)
	}
	slices.SortFunc(nodes, func(a, b Node) int {
		if a.ID() > b.ID() {
			return 1
		}
		if a.ID() < b.ID() {
			return -1
		}
		return 0
	})
	return nodes
}

// Plan represents a physical execution plan as a directed acyclic graph (DAG).
// It maintains the relationships between nodes, tracking parent-child connections
// and providing methods for graph traversal and manipulation.
//
// The plan structure supports operations like adding nodes and edges,
// retrieving nodes by ID, retrieving parents and children of nodes, and
// walking the graph in different orders using the depth-first-search algorithm.
type Plan struct {
	// nodesByID maps node IDs to their corresponding Node instances for quick lookups
	nodesByID map[string]Node
	// nodes is a set containing all nodes in the plan
	nodes nodeSet
	// parents maps each node to a set of its parent nodes in the execution graph
	parents map[Node]nodeSet
	// children maps each node to a set of its child nodes in the execution graph
	children map[Node]nodeSet
}

func (p *Plan) init() {
	if p.nodesByID == nil {
		p.nodesByID = make(map[string]Node)
	}
	if p.nodes == nil {
		p.nodes = make(nodeSet)
	}
	if p.parents == nil {
		p.parents = make(map[Node]nodeSet)
	}
	if p.children == nil {
		p.children = make(map[Node]nodeSet)
	}
}

// addNode adds a new node to the plan if it doesn't already exist. For
// convenience, the function returns the input node without modification.
func (p *Plan) addNode(n Node) Node {
	p.init()
	if n == nil {
		return nil
	}
	if p.nodes.contains(n) {
		return n
	}
	p.nodes.add(n)
	p.nodesByID[n.ID()] = n

	if _, ok := p.parents[n]; !ok {
		p.parents[n] = make(nodeSet)
	}
	if _, ok := p.children[n]; !ok {
		p.children[n] = make(nodeSet)
	}
	return n
}

// addEdge creates a directed edge between two nodes in the plan.
// It establishes a parent-child relationship between the nodes where
// e.Parent becomes a parent of e.Child. Both nodes must already exist
// in the plan. Returns an error if either node is nil or doesn't exist
// in the plan.
// The order of addition of edges is not preserved.
func (p *Plan) addEdge(e Edge) error {
	if e.Parent == nil || e.Child == nil {
		return fmt.Errorf("parent and child nodes must not be nil")
	}
	if !p.nodes.contains(e.Parent) {
		return fmt.Errorf("node %s does not exist in graph", e.Parent.ID())
	}
	if !p.nodes.contains(e.Child) {
		return fmt.Errorf("node %s does not exist in graph", e.Child.ID())
	}

	p.children[e.Parent].add(e.Child)
	p.parents[e.Child].add(e.Parent)
	return nil
}

// Len returns the number of nodes in the graph
func (p *Plan) Len() int {
	return len(p.nodes)
}

// NodeByID returns the node with the given identifier
func (p *Plan) NodeByID(id string) Node {
	return p.nodesByID[id]
}

// Parents returns all parent nodes of the given node
func (p *Plan) Parents(n Node) []Node {
	if _, ok := p.parents[n]; !ok {
		return nil
	}
	return p.parents[n].sorted()
}

// Children returns all child nodes of the given node
func (p *Plan) Children(n Node) []Node {
	if _, ok := p.children[n]; !ok {
		return nil
	}
	return p.children[n].sorted()
}

// Roots returns all nodes that have no parents
func (p *Plan) Roots() []Node {
	if len(p.nodes) == 0 {
		return nil
	}

	var roots []Node
	for node := range p.nodes {
		if len(p.parents[node]) == 0 {
			roots = append(roots, node)
		}
	}
	return roots
}

// Leaves returns all nodes that have no children
func (p *Plan) Leaves() []Node {
	if len(p.nodes) == 0 {
		return nil
	}

	var leaves []Node
	for node := range p.nodes {
		if len(p.children[node]) == 0 {
			leaves = append(leaves, node)
		}
	}
	return leaves
}

// DFSWalk performs a depth-first traversal of the plan starting from node n.
// It applies the visitor v to each node according to the specified walk order.
// The order parameter determines if nodes are visited before their children (PreOrderWalk)
// or after their children (PostOrderWalk).
func (p *Plan) DFSWalk(n Node, v Visitor, order WalkOrder) error {
	visited := make(nodeSet)
	switch order {
	case PreOrderWalk:
		return p.preOrderWalk(n, v, visited)
	case PostOrderWalk:
		return p.postOrderWalk(n, v, visited)
	default:
		return errors.New("unsupported walk order. must be one of PreOrderWalk and PostOrderWalk")
	}
}

func (p *Plan) preOrderWalk(n Node, v Visitor, visited nodeSet) error {
	if visited.contains(n) {
		return nil
	}
	visited.add(n)

	if err := n.Accept(v); err != nil {
		return err
	}

	for _, child := range p.Children(n) {
		if err := p.preOrderWalk(child, v, visited); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) postOrderWalk(n Node, v Visitor, visited nodeSet) error {
	if visited.contains(n) {
		return nil
	}
	visited.add(n)

	for _, child := range p.Children(n) {
		if err := p.postOrderWalk(child, v, visited); err != nil {
			return err
		}
	}
	return n.Accept(v)
}
