// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"go.yaml.in/yaml/v4"
)

type nodeMap struct {
	line   int
	column int
	node   *yaml.Node
}

// NodeOrigin represents where a node has come from within a specification. This is not useful for single file specs,
// but becomes very, very important when dealing with exploded specifications, and we need to know where in the mass
// of files a node has come from.
type NodeOrigin struct {
	// Node is the node in question (defaults to key node)
	Node *yaml.Node `json:"-"`

	// ValueNode is the value node of the node in question, if has a different origin
	ValueNode *yaml.Node `json:"-"`

	// Line is yhe original line of where the node was found in the original file
	Line int `json:"line" yaml:"line"`

	// Column is the original column of where the node was found in the original file
	Column int `json:"column" yaml:"column"`

	// LineValue is the line of the value (if the origin of the key and value are different)
	LineValue int `json:"lineValue,omitempty" yaml:"lineValue,omitempty"`

	// ColumnValue is the line of the value (if the origin of the key and value are different)
	ColumnValue int `json:"columnKey,omitempty" yaml:"columnKey,omitempty"`

	// AbsoluteLocation is the absolute path to the reference was extracted from.
	// This can either be an absolute path to a file, or a URL.
	AbsoluteLocation string `json:"absoluteLocation" yaml:"absoluteLocation"`

	// AbsoluteLocationValue is the absolute path to where the ValueNode was extracted from.
	// this only applies when keys and values have different origins.
	AbsoluteLocationValue string `json:"absoluteLocationValue,omitempty" yaml:"absoluteLocationValue,omitempty"`

	// Index is the index that contains the node that was located in.
	Index *SpecIndex `json:"-" yaml:"-"`
}

// GetNode returns a node from the spec based on a line and column. The second return var bool is true
// if the node was found, false if not.
func (index *SpecIndex) GetNode(line int, column int) (*yaml.Node, bool) {
	index.nodeMapLock.RLock()
	if index.nodeMap[line] == nil {
		return nil, false
	}
	node := index.nodeMap[line][column]
	index.nodeMapLock.RUnlock()
	return node, node != nil
}

// MapNodes maps all nodes in the document to a map of line/column to node.
func (index *SpecIndex) MapNodes(rootNode *yaml.Node) {
	cruising := make(chan struct{})
	nodeChan := make(chan *nodeMap)
	go func(nodeChan chan *nodeMap) {
		done := false
		for !done {
			node, ok := <-nodeChan
			if !ok {
				done = true
				cruising <- struct{}{}
				return
			}
			index.nodeMapLock.Lock()
			if index.nodeMap[node.line] == nil {
				index.nodeMap[node.line] = make(map[int]*yaml.Node)
			}
			index.nodeMap[node.line][node.column] = node.node
			index.nodeMapLock.Unlock()
		}
	}(nodeChan)
	go enjoyALuxuryCruise(rootNode, nodeChan, true)
	<-cruising
	close(cruising)
	index.nodeMapCompleted <- struct{}{}
	close(index.nodeMapCompleted)
}

func enjoyALuxuryCruise(node *yaml.Node, nodeChan chan *nodeMap, root bool) {
	if node.Kind == yaml.DocumentNode {
		node = node.Content[0]
	}
	if len(node.Content) > 0 {
		for _, child := range node.Content {
			nodeChan <- &nodeMap{
				line:   child.Line,
				column: child.Column,
				node:   child,
			}
			enjoyALuxuryCruise(child, nodeChan, false)
		}
	}
	nodeChan <- &nodeMap{
		line:   node.Line,
		column: node.Column,
		node:   node,
	}
	if root {
		close(nodeChan)
	}
}
