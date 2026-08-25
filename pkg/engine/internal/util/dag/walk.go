package dag

import "errors"

// WalkOrder defined the order in which current vertex and its children are
// visited.
type WalkOrder uint8

const (
	// PreOrderWalk processes the current vertex before visiting any of its
	// children.
	PreOrderWalk WalkOrder = iota

	// PostOrderWalk processes the current vertex after visiting all of its
	// children.
	PostOrderWalk
)

// WalkFunc is a function that gets invoked when walking a Graph. Walking will
// stop if WalkFunc returns a non-nil error.
type WalkFunc[NodeType Node] func(n NodeType) error

// Walk performs a depth-first walk of outgoing edges for all nodes in start,
// invoking the provided fn for each node. Walk returns the error returned by
// fn.
//
// Nodes unreachable from start will not be passed to fn.
func (g *Graph[NodeType]) Walk(n NodeType, f WalkFunc[NodeType], order WalkOrder) error {
	visited := make(nodeSet[NodeType])
	switch order {
	case PreOrderWalk:
		return g.preOrderWalk(n, f, visited)
	case PostOrderWalk:
		return g.postOrderWalk(n, f, visited)
	default:
		return errors.New("unsupported walk order. must be one of PreOrderWalk and PostOrderWalk")
	}
}

func (g *Graph[NodeType]) preOrderWalk(n NodeType, f WalkFunc[NodeType], visited nodeSet[NodeType]) error {
	if visited.Contains(n) {
		return nil
	}
	visited.Add(n)

	if err := f(n); err != nil {
		return err
	}

	for _, child := range g.children[n] {
		if err := g.preOrderWalk(child, f, visited); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph[NodeType]) postOrderWalk(n NodeType, f WalkFunc[NodeType], visited nodeSet[NodeType]) error {
	if visited.Contains(n) {
		return nil
	}
	visited.Add(n)

	for _, child := range g.children[n] {
		if err := g.postOrderWalk(child, f, visited); err != nil {
			return err
		}
	}

	return f(n)
}
