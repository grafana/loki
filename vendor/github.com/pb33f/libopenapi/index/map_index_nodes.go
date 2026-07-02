// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"go.yaml.in/yaml/v4"
)

// NodeOrigin represents where a node has come from within a specification. This is not useful for single file specs,
// but becomes very, very important when dealing with exploded specifications, and we need to know where in the mass
// of files a node has come from.
type NodeOrigin struct {
	// Node is the node in question (defaults to key node)
	Node *yaml.Node `json:"-"`

	// ValueNode is the value node of the node in question, if has a different origin
	ValueNode *yaml.Node `json:"-"`

	// Line is the original line of where the node was found in the original file
	Line int `json:"line" yaml:"line"`

	// Column is the original column of where the node was found in the original file
	Column int `json:"column" yaml:"column"`

	// LineValue is the line of the value (if the origin of the key and value are different)
	LineValue int `json:"lineValue,omitempty" yaml:"lineValue,omitempty"`

	// ColumnValue is the column of the value (if the origin of the key and value are different)
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

// nodeLineEntry is a single (column, node) pair on one line of the spec. Lines hold very
// few nodes, so a small slice scanned linearly is far cheaper than a per-line map.
type nodeLineEntry struct {
	column int32
	node   *yaml.Node
}

// GetNode returns a node from the spec based on a line and column. The second return var bool is true
// if the node was found, false if not. Blocks until the node line index has been fully built.
func (index *SpecIndex) GetNode(line int, column int) (*yaml.Node, bool) {
	index.awaitNodeMap()
	index.nodeMapLock.RLock()
	defer index.nodeMapLock.RUnlock()
	node := lookupNodeLines(index.nodeLines, line, column)
	return node, node != nil
}

// awaitNodeMap blocks until MapNodes has published the node line index. It is a no-op
// once the index has been built or released (the completion channel is close-only).
func (index *SpecIndex) awaitNodeMap() {
	if ch := index.nodeMapCompleted; ch != nil {
		<-ch
	}
}

// lookupNodeLines returns the node stored at line/column, or nil if absent.
func lookupNodeLines(lines [][]nodeLineEntry, line, column int) *yaml.Node {
	if line < 0 || line >= len(lines) {
		return nil
	}
	for _, e := range lines[line] {
		if int(e.column) == column {
			return e.node
		}
	}
	return nil
}

// MapNodes maps all nodes in the document by line and column. The index is built into a
// local structure without locking, published under a single lock, and completion is
// signalled by closing nodeMapCompleted (close-only: supports any number of waiters).
func (index *SpecIndex) MapNodes(rootNode *yaml.Node) {
	sizeHint := 0
	if index.config != nil && index.config.SpecInfo != nil {
		sizeHint = index.config.SpecInfo.NumLines
	}
	// lines are 1-based; +1 so line NumLines is directly addressable.
	lines := make([][]nodeLineEntry, sizeHint+1)
	lines = mapNodesRecursive(rootNode, lines)
	index.nodeMapLock.Lock()
	index.nodeLines = lines
	index.nodeMapLock.Unlock()
	close(index.nodeMapCompleted)
}

func mapNodesRecursive(node *yaml.Node, lines [][]nodeLineEntry) [][]nodeLineEntry {
	if node.Kind == yaml.DocumentNode {
		node = node.Content[0]
	}
	for _, child := range node.Content {
		lines = addNodeLineEntry(lines, child)
		lines = mapNodesRecursive(child, lines)
	}
	return addNodeLineEntry(lines, node)
}

// addNodeLineEntry records node at its line/column, preserving the previous map
// semantics: a later write to the same line/column replaces the earlier one
// (parents are written after their children, so parents win collisions).
func addNodeLineEntry(lines [][]nodeLineEntry, node *yaml.Node) [][]nodeLineEntry {
	line := node.Line
	if line < 0 {
		return lines
	}
	if line >= len(lines) {
		grown := len(lines) * 2
		if grown <= line {
			grown = line + 1
		}
		expanded := make([][]nodeLineEntry, grown)
		copy(expanded, lines)
		lines = expanded
	}
	entries := lines[line]
	for i := range entries {
		if int(entries[i].column) == node.Column {
			entries[i].node = node
			return lines
		}
	}
	lines[line] = append(entries, nodeLineEntry{column: int32(node.Column), node: node})
	return lines
}
