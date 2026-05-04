// Package dag provides utilities for working with directed acyclic graphs
// (DAGs).
package dag

import (
	"errors"
	"fmt"
	"iter"
	"maps"
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

// Eliminate removes the node n from the graph and reconnects n's parents to
// n's children, maintaining connectivity across the graph.
//
// If n does not have a parent, all of its children will be promoted to root
// nodes.
func (g *Graph[NodeType]) Eliminate(n NodeType) {
	g.EliminateBatch([]NodeType{n})
}

// EliminateBatch removes all nodes in toRemove from the graph in a single pass,
// reconnecting each removed node's surviving ancestors to its surviving
// descendants.
//
// When a chain of removed nodes separates a surviving ancestor from a surviving
// descendant, EliminateBatch reconnects them directly. E.g.
//
// In: Task A → Task B → Task C → Task D -- (remove tasks B and C)
// Output: Task A → Task D
//
// This also applies when a surviving node has no descendants through the removed
// subgraph, in which case the surviving ancestor simply loses the removed children from its
// edge list. E.g.
//
// In: Task A → Task B → Task C → Task D -- (remove tasks B, C, and D)
// Output: Task A
func (g *Graph[NodeType]) EliminateBatch(toRemove []NodeType) {
	if len(toRemove) == 0 {
		return
	}

	removeSet := make(map[NodeType]struct{}, len(toRemove))
	for _, n := range toRemove {
		removeSet[n] = struct{}{}
	}

	// Auxiliary func to find all surviving ancestors & descendants
	// reachable only by traversing through other removed nodes
	ancestorsCache := make(map[NodeType][]NodeType)
	descendantsCache := make(map[NodeType][]NodeType)
	var survivingAncestors, survivingDescendants func(n NodeType) []NodeType
	survivingAncestors = func(n NodeType) []NodeType {
		if res, ok := ancestorsCache[n]; ok {
			return res
		}
		seen := make(map[NodeType]struct{})
		for _, p := range g.parents[n] {
			if _, removed := removeSet[p]; !removed {
				seen[p] = struct{}{}
			} else {
				for _, a := range survivingAncestors(p) {
					seen[a] = struct{}{}
				}
			}
		}
		res := make([]NodeType, 0, len(seen))
		for a := range seen {
			res = append(res, a)
		}
		ancestorsCache[n] = res
		return res
	}
	survivingDescendants = func(n NodeType) []NodeType {
		if res, ok := descendantsCache[n]; ok {
			return res
		}
		seen := make(map[NodeType]struct{})
		for _, c := range g.children[n] {
			if _, removed := removeSet[c]; !removed {
				seen[c] = struct{}{}
			} else {
				for _, d := range survivingDescendants(c) {
					seen[d] = struct{}{}
				}
			}
		}
		res := make([]NodeType, 0, len(seen))
		for d := range seen {
			res = append(res, d)
		}
		descendantsCache[n] = res
		return res
	}

	// childSets[p]  = new children set for surviving parent p.
	// parentSets[c] = new parents set for surviving child c.
	childSets := make(map[NodeType]map[NodeType]struct{})
	parentSets := make(map[NodeType]map[NodeType]struct{})

	// childSets/parentSets stage the new edge lists for surviving nodes adjacent
	// to removed ones. Sets give free deduplication across multiple removed paths.
	// initChildSet/initParentSet seed each set lazily so each node is scanned once.
	initChildSet := func(p NodeType) {
		if _, ok := childSets[p]; ok {
			return
		}
		s := make(map[NodeType]struct{})
		for _, c := range g.children[p] {
			if _, removed := removeSet[c]; !removed {
				s[c] = struct{}{}
			}
		}
		childSets[p] = s
	}
	initParentSet := func(c NodeType) {
		if _, ok := parentSets[c]; ok {
			return
		}
		s := make(map[NodeType]struct{})
		for _, p := range g.parents[c] {
			if _, removed := removeSet[p]; !removed {
				s[p] = struct{}{}
			}
		}
		parentSets[c] = s
	}

	// For each removed node, bridge its surviving ancestors to its surviving
	// descendants, closing the gap left by the removal.
	for _, n := range toRemove {
		ancs := survivingAncestors(n)
		descs := survivingDescendants(n)

		for _, a := range ancs {
			initChildSet(a)
			for _, d := range descs {
				childSets[a][d] = struct{}{}
			}
		}
		for _, d := range descs {
			initParentSet(d)
			for _, a := range ancs {
				parentSets[d][a] = struct{}{}
			}
		}
	}

	// Write rebuilt slices back.
	for node, set := range childSets {
		children := make([]NodeType, 0, len(set))
		for c := range set {
			children = append(children, c)
		}
		g.children[node] = children
	}
	for node, set := range parentSets {
		parents := make([]NodeType, 0, len(set))
		for p := range set {
			parents = append(parents, p)
		}
		g.parents[node] = parents
	}

	// Delete removed nodes.
	for _, n := range toRemove {
		delete(g.parents, n)
		delete(g.children, n)
		g.nodes.Remove(n)
	}
}

// Inject injects a new node between a parent and its children:
//
// * The children of parent become children of node.
// * The child of parent becomes node.
//
// Inject panics if given a node that already exists in the plan.
//
// For convenience, Inject returns node without modification.
func (g *Graph[NodeType]) Inject(parent, node NodeType) NodeType {
	if g.nodes.Contains(node) {
		panic("injectNode: target node already exists in plan")
	}
	g.Add(node)

	// Update parent's children so that their parent is node.
	g.children[node] = g.children[parent]
	for _, child := range g.children[node] {
		g.parents[child] = slices.DeleteFunc(g.parents[child], func(check NodeType) bool { return check == parent })

		// We don't have to check for the presence of node because we guarantee
		// that it doesn't already exist in the graph with any edges.
		g.parents[child] = append(g.parents[child], node)
	}

	// Add an edge between parent and node.
	g.parents[node] = []NodeType{parent}
	g.children[parent] = []NodeType{node}
	return node
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

// Leaves returns all nodes that have no children.
func (g *Graph[NodeType]) Leaves() []NodeType {
	if len(g.nodes) == 0 {
		return nil
	}

	var leaves []NodeType
	for node := range g.nodes {
		if len(g.children[node]) == 0 {
			leaves = append(leaves, node)
		}
	}
	return leaves
}

// Clone returns a shallow clone of the graph: nodes in the graph are
// transferred using ordinary assignment.
func (g *Graph[NodeType]) Clone() *Graph[NodeType] {
	// We want to copy the children slice so we can't just use [maps.Clone] for
	// g.children.
	newChildren := make(map[NodeType][]NodeType, len(g.children))
	for node, children := range g.children {
		newChildren[node] = slices.Clone(children)
	}

	return &Graph[NodeType]{
		nodes:    maps.Clone(g.nodes),
		parents:  maps.Clone(g.parents),
		children: newChildren,
	}
}
