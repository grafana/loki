// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package low

import (
	"sync"

	"go.yaml.in/yaml/v4"
)

// MergeRecursiveNodesIfLineAbsent walks a node tree and adds each discovered node to dst
// unless that line already exists in the destination map.
func MergeRecursiveNodesIfLineAbsent(dst *sync.Map, node *yaml.Node) {
	if dst == nil || node == nil {
		return
	}

	blocked := make(map[int]bool)
	known := make(map[int]bool)
	nodeMap := &NodeMap{Nodes: dst}
	walkRecursiveNodes(node, func(current *yaml.Node) {
		line := current.Line
		if !known[line] {
			_, blocked[line] = dst.Load(line)
			known[line] = true
		}
		if !blocked[line] {
			nodeMap.AddNode(line, current)
		}
	})
}

// AppendRecursiveNodes walks a node tree and appends each discovered node to dst.
func AppendRecursiveNodes(dst AddNodes, node *yaml.Node) {
	if dst == nil || node == nil {
		return
	}

	walkRecursiveNodes(node, func(current *yaml.Node) {
		dst.AddNode(current.Line, current)
	})
}

func walkRecursiveNodes(node *yaml.Node, visit func(*yaml.Node)) {
	if node == nil || visit == nil || node.Content == nil {
		return
	}

	for i := 0; i < len(node.Content); i++ {
		current := node.Content[i]
		if current.Line != 0 {
			visit(current)
		}
		if len(current.Content) > 0 {
			walkRecursiveNodes(current, visit)
		}
	}
}
