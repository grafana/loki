// Package dag provides utilities for working with directed acyclic graphs
// (DAGs).
package dag

import (
	"errors"
	"fmt"
	"iter"
	"slices"

	"github.com/oklog/ulid/v2"
)

// Node represents an individual node in a Graph. The zero value of Node is
// reserved to indicate a nil node.
type Node interface {
	comparable

	ID() ulid.ULID
}

// Edge is a directed connection (parent-child relation) between two nodes.
type Edge[NodeType Node] struct {
	Parent, Child NodeType
}

// Graph is a directed acyclic graph (DAG).
type Graph[NodeType Node] struct {
	// nodes is a set containing all nodes in the plan.
	nodes nodeSet[NodeType]
	// parents maps each node to its parent nodes in the execution graph.
	parents map[NodeType][]NodeType
	// children maps each node to a set of its child nodes in the execution
	// graph.
	children map[NodeType][]NodeType
}

func (g *Graph[NodeType]) init() {
	if g.nodes == nil {
		g.nodes = make(nodeSet[NodeType])
	}
	if g.parents == nil {
		g.parents = make(map[NodeType][]NodeType)
	}
	if g.children == nil {
		g.children = make(map[NodeType][]NodeType)
	}
}

// Add adds a new node n to the graph if it doesn't already exist. For
// convenience, Add returns the input node without modification.
//
// Add is a no-op if n is the zero value.
func (g *Graph[NodeType]) Add(n NodeType) NodeType {
	g.init()
	if g.nodes.Contains(n) || isZero(n) {
		return n
	}
	g.nodes.Add(n)

	return n
}

func isZero[NodeType Node](n NodeType) bool {
	return n == zeroValue[NodeType]()
}

func zeroValue[NodeType Node]() NodeType {
	var zero NodeType
	return zero
}

// AddEdge creates a directed edge between two nodes in the graph, establishing
// a parent-child relationship between the nodes where e.Parent becomes a parent
// of e.Child.
//
// AddEdge returns an error if:
//
// * Either node is the zero value, or
// * either node doesn't exist in the graph.
//
// AddEdge preserves the order of addition of edges.
func (g *Graph[NodeType]) AddEdge(e Edge[NodeType]) error {
	if isZero(e.Parent) || isZero(e.Child) {
		return fmt.Errorf("parent and child nodes must not be zero values")
	}
	if !g.nodes.Contains(e.Parent) {
		return fmt.Errorf("parent node %s does not exist in graph", e.Parent.ID())
	}
	if !g.nodes.Contains(e.Child) {
		return fmt.Errorf("child node %s does not exist in graph", e.Child.ID())
	}

	// Uniquely add the edges.
	if !slices.Contains(g.children[e.Parent], e.Child) {
		g.children[e.Parent] = append(g.children[e.Parent], e.Child)
	}
	if !slices.Contains(g.parents[e.Child], e.Parent) {
		g.parents[e.Child] = append(g.parents[e.Child], e.Parent)
	}

	return nil
}

// Len returns the number of nodes in the graph.
func (g *Graph[NodeType]) Len() int {
	return len(g.nodes)
}

// Nodes returns all nodes in the graph in an unspecified order.
func (g *Graph[NodeType]) Nodes() iter.Seq[NodeType] {
	return func(yield func(NodeType) bool) {
		for node := range g.nodes {
			if !yield(node) {
				return
			}
		}
	}
}

// Parent returns the parent of the given node.
func (g *Graph[NodeType]) Parents(n NodeType) []NodeType {
	if _, ok := g.parents[n]; !ok {
		return nil
	}
	return g.parents[n]
}

// Children returns all child nodes of the given node.
func (g *Graph[NodeType]) Children(n NodeType) []NodeType {
	if _, ok := g.children[n]; !ok {
		return nil
	}
	return g.children[n]
}

// Roots returns all nodes that have no parents.
func (g *Graph[NodeType]) Roots() []NodeType {
	if len(g.nodes) == 0 {
		return nil
	}

	var roots []NodeType
	for node := range g.nodes {
		if len(g.parents[node]) == 0 {
			roots = append(roots, node)
		}
	}
	return roots
}

// Root returns the root node that have no parents. It returns an error if the
// plan has no or multiple root nodes.
func (g *Graph[NodeType]) Root() (NodeType, error) {
	roots := g.Roots()
	if len(roots) == 0 {
		return zeroValue[NodeType](), errors.New("plan has no root node")
	} else if len(roots) > 1 {
		return zeroValue[NodeType](), errors.New("plan has multiple root nodes")
	}
	return roots[0], nil
}
