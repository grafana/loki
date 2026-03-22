// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io
// MIT License

package low

import (
	"context"
	"sync"

	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// HasNodes is an interface that defines a method to get a map of nodes
type HasNodes interface {
	GetNodes() map[int][]*yaml.Node
}

// AddNodes is an interface that defined a method to add nodes.
type AddNodes interface {
	AddNode(key int, node *yaml.Node)
}

// NodeMap represents a map of yaml nodes
type NodeMap struct {
	// Nodes is a sync map of nodes for this object, and the key is the line number of the node
	// a line can contain many nodes (in JSON), so the value is a slice of *yaml.Node
	Nodes *sync.Map `yaml:"-" json:"-"`
}

// AddNode will add a node to the NodeMap
func (nm *NodeMap) AddNode(key int, node *yaml.Node) {
	if existing, ok := nm.Nodes.Load(key); ok {
		if ext, ko := existing.(*yaml.Node); ko {
			nm.Nodes.Store(key, []*yaml.Node{ext, node})
		}
		if ext, ko := existing.([]*yaml.Node); ko {
			ext = append(ext, node)
			nm.Nodes.Store(key, ext)
		}
	} else {
		nm.Nodes.Store(key, []*yaml.Node{node})
	}
}

// GetNodes will return the map of nodes
func (nm *NodeMap) GetNodes() map[int][]*yaml.Node {
	composed := make(map[int][]*yaml.Node)
	if nm.Nodes != nil {
		nm.Nodes.Range(func(key, value interface{}) bool {
			if v, ok := value.([]*yaml.Node); ok {
				composed[key.(int)] = v
			}
			if v, ok := value.(*yaml.Node); ok {
				composed[key.(int)] = []*yaml.Node{v}
			}

			return true
		})
	}
	if len(composed) <= 0 {
		composed[0] = []*yaml.Node{} // return an empty slice if there are no nodes
	}
	return composed
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
func ExtractNodes(_ context.Context, root *yaml.Node) *sync.Map {
	var syncMap sync.Map
	nm := &NodeMap{Nodes: &syncMap}
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
func ExtractNodesRecursive(_ context.Context, root *yaml.Node) *sync.Map {
	var syncMap sync.Map
	nm := &NodeMap{Nodes: &syncMap}
	nm.ExtractNodes(root, true)
	return nm.Nodes
}

// ExtractExtensionNodes will extract all extension nodes from a map of extensions, recursively.
func ExtractExtensionNodes(_ context.Context,
	extensionMap *orderedmap.Map[KeyReference[string],
		ValueReference[*yaml.Node]], nodeMap *sync.Map,
) {
	// range over the extension map and extract all nodes
	for k, v := range extensionMap.FromOldest() {
		results := []*yaml.Node{k.KeyNode}
		var newNodeMap sync.Map
		nm := &NodeMap{Nodes: &newNodeMap}
		if len(v.ValueNode.Content) > 0 {
			nm.ExtractNodes(v.ValueNode, true)
			nm.Nodes.Range(func(key, value interface{}) bool {
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
