// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io
// MIT License

package low

import (
	"context"
	"slices"
	"sync"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/orderedmap"
)

// HasNodes is an interface that defines a method to get a map of nodes
type HasNodes interface {
	GetNodes() map[int][]*yaml.Node
}

// AddNodes is an interface that defined a method to add nodes.
type AddNodes interface {
	AddNode(key int, node *yaml.Node)
}

// NodeLines holds the yaml nodes of a model object by line number. A line holds a *yaml.Node, or a
// []*yaml.Node when several nodes share it (as they do in JSON).
//
// Node maps are filled for every object built but read only by tools that map lines back to objects, so
// writes are recorded as they happen and the line index is built when the nodes are first read.
// NodeLines is safe for concurrent use, and its zero value is ready to use.
type NodeLines struct {
	mu      sync.Mutex
	pending []nodeLineWrite
	lines   map[int]any
}

type nodeLineWrite struct {
	line  int
	value any
	add   bool // add the node to the line rather than replace the line's value
}

// Store sets the value held for a line.
func (n *NodeLines) Store(line int, value any) {
	n.write(nodeLineWrite{line: line, value: value})
}

// Load returns the value held for a line, and whether the line holds one.
func (n *NodeLines) Load(line int) (any, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	value, ok := n.index()[line]
	return value, ok
}

// Range calls f for each line and its value, in line order, until f returns false. The lines are read
// before f is first called, so f may write to the nodes.
func (n *NodeLines) Range(f func(line int, value any) bool) {
	n.mu.Lock()
	index := n.index()
	lines := make([]int, 0, len(index))
	for line := range index {
		lines = append(lines, line)
	}
	slices.Sort(lines)
	values := make([]any, len(lines))
	for i, line := range lines {
		values[i] = index[line]
	}
	n.mu.Unlock()
	for i, line := range lines {
		if !f(line, values[i]) {
			return
		}
	}
}

// add appends a node to a line.
func (n *NodeLines) add(line int, node *yaml.Node) {
	n.write(nodeLineWrite{line: line, value: node, add: true})
}

func (n *NodeLines) write(w nodeLineWrite) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.lines != nil {
		n.apply(w)
		return
	}
	if n.pending == nil {
		n.pending = make([]nodeLineWrite, 0, 8)
	}
	n.pending = append(n.pending, w)
}

// index builds the line index from the recorded writes, once. The caller holds the lock.
func (n *NodeLines) index() map[int]any {
	if n.lines == nil {
		n.lines = make(map[int]any, len(n.pending))
		for _, w := range n.pending {
			n.apply(w)
		}
		n.pending = nil
	}
	return n.lines
}

func (n *NodeLines) apply(w nodeLineWrite) {
	if !w.add {
		n.lines[w.line] = w.value
		return
	}
	node := w.value.(*yaml.Node)
	existing, ok := n.lines[w.line]
	if !ok {
		n.lines[w.line] = []*yaml.Node{node}
		return
	}
	switch ext := existing.(type) {
	case *yaml.Node:
		n.lines[w.line] = []*yaml.Node{ext, node}
	case []*yaml.Node:
		n.lines[w.line] = append(ext, node)
	}
}

// NodeMap represents a map of yaml nodes
type NodeMap struct {
	// Nodes holds the nodes of this object by line number. A line can contain many nodes (in JSON), so
	// a line's value is a *yaml.Node or a []*yaml.Node.
	Nodes *NodeLines `yaml:"-" json:"-"`
}

// AddNode will add a node to the NodeMap
func (nm *NodeMap) AddNode(key int, node *yaml.Node) {
	nm.Nodes.add(key, node)
}

// GetNodes will return the map of nodes
func (nm *NodeMap) GetNodes() map[int][]*yaml.Node {
	composed := make(map[int][]*yaml.Node)
	if nm.Nodes != nil {
		nm.Nodes.Range(func(line int, value any) bool {
			if v, ok := value.([]*yaml.Node); ok {
				composed[line] = v
			}
			if v, ok := value.(*yaml.Node); ok {
				composed[line] = []*yaml.Node{v}
			}

			return true
		})
	}
	if len(composed) <= 0 {
		composed[0] = []*yaml.Node{} // return an empty slice if there are no nodes
	}
	return composed
}

// GetNodeLines returns an owned snapshot of the line numbers in GetNodes, in
// unspecified order. Only *yaml.Node and []*yaml.Node values contribute lines,
// including typed nil values. If no such values exist, the result is []int{0}.
// Later writes to Nodes do not change the returned slice.
func (nm *NodeMap) GetNodeLines() []int {
	if nm.Nodes == nil {
		return []int{0}
	}
	nm.Nodes.mu.Lock()
	defer nm.Nodes.mu.Unlock()
	index := nm.Nodes.index()
	lines := make([]int, 0, len(index))
	for line, value := range index {
		switch value.(type) {
		case *yaml.Node, []*yaml.Node:
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, 0)
	}
	return lines
}

// ExtractNodes will iterate over a *yaml.Node and extract all nodes with a line number into a map
func (nm *NodeMap) ExtractNodes(node *yaml.Node, recurse bool) {
	if node == nil {
		return
	}
	// if the node has content, iterate over it and extract every top level line number
	if node.Content != nil {
		for i := 0; i < len(node.Content); i++ {
			if node.Content[i].Line != 0 && len(node.Content[i].Content) <= 0 {
				nm.AddNode(node.Content[i].Line, node.Content[i])
			}
			if node.Content[i].Line != 0 && len(node.Content[i].Content) > 0 {
				if recurse {
					nm.AddNode(node.Content[i].Line, node.Content[i])
					nm.ExtractNodes(node.Content[i], recurse)
				}
			}
		}
	}
}

// ContainsLine will return true if the NodeMap contains a node with the supplied line number
func (nm *NodeMap) ContainsLine(line int) bool {
	if _, ok := nm.Nodes.Load(line); ok {
		return true
	}
	return false
}

// ExtractNodes will extract all nodes from a yaml.Node and return them in a map
func ExtractNodes(_ context.Context, root *yaml.Node) *NodeLines {
	nm := &NodeMap{Nodes: &NodeLines{}}
	if root != nil && len(root.Content) > 0 {
		nm.ExtractNodes(root, false)
	} else {
		if root != nil {
			nm.AddNode(root.Line, root)
		}
	}
	return nm.Nodes
}

// ExtractNodesRecursive will extract all nodes from a yaml.Node and return them in a map, just like ExtractNodes
// however, this version will dive-down the tree and extract all nodes from all child nodes as well until the tree
// is done.
func ExtractNodesRecursive(_ context.Context, root *yaml.Node) *NodeLines {
	nm := &NodeMap{Nodes: &NodeLines{}}
	nm.ExtractNodes(root, true)
	return nm.Nodes
}

// ExtractExtensionNodes will extract all extension nodes from a map of extensions, recursively.
func ExtractExtensionNodes(_ context.Context,
	extensionMap *orderedmap.Map[KeyReference[string],
		ValueReference[*yaml.Node]], nodeMap *NodeLines,
) {
	// range over the extension map and extract all nodes
	for k, v := range extensionMap.FromOldest() {
		results := []*yaml.Node{k.KeyNode}
		nm := &NodeMap{Nodes: &NodeLines{}}
		if len(v.ValueNode.Content) > 0 {
			nm.ExtractNodes(v.ValueNode, true)
			nm.Nodes.Range(func(_ int, value any) bool {
				for _, n := range value.([]*yaml.Node) {
					results = append(results, n)
				}
				return true
			})
		} else {
			results = append(results, v.ValueNode)
		}
		if nodeMap != nil {
			if k.KeyNode.Line == v.ValueNode.Line {
				nodeMap.Store(k.KeyNode.Line, results)
			} else {
				nodeMap.Store(k.KeyNode.Line, results[0])
				for _, y := range results[1:] {
					nodeMap.Store(y.Line, []*yaml.Node{y})
				}
			}
		}
	}
}
