// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package utils

import "go.yaml.in/yaml/v4"

// YAMLNodeCloneFlags configures CloneYAMLNodeWithFlags behavior.
type YAMLNodeCloneFlags uint8

const (
	// YAMLNodeCloneStripAnchors removes anchor names from every cloned node.
	YAMLNodeCloneStripAnchors YAMLNodeCloneFlags = 1 << iota

	// YAMLNodeCloneUnwrapDocument clones the document node's first child instead
	// of cloning the document node itself.
	YAMLNodeCloneUnwrapDocument
)

// CloneYAMLNode returns a deep copy of a YAML node graph.
func CloneYAMLNode(node *yaml.Node) *yaml.Node {
	return CloneYAMLNodeWithFlags(node, 0)
}

// CloneYAMLNodeWithFlags returns a deep copy of a YAML node graph, applying the
// supplied flags while cloning.
func CloneYAMLNodeWithFlags(node *yaml.Node, flags YAMLNodeCloneFlags) *yaml.Node {
	if node == nil {
		return nil
	}
	if flags&YAMLNodeCloneUnwrapDocument != 0 && node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	return cloneYAMLNode(node, flags, make(map[*yaml.Node]*yaml.Node, 8))
}

func cloneYAMLNode(node *yaml.Node, flags YAMLNodeCloneFlags, seen map[*yaml.Node]*yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if cloned, ok := seen[node]; ok {
		return cloned
	}

	cloned := *node
	if flags&YAMLNodeCloneStripAnchors != 0 {
		cloned.Anchor = ""
	}
	cloned.Alias = nil
	cloned.Content = nil
	seen[node] = &cloned

	if node.Alias != nil {
		cloned.Alias = cloneYAMLNode(node.Alias, flags, seen)
	}
	if len(node.Content) > 0 {
		cloned.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			cloned.Content[i] = cloneYAMLNode(child, flags, seen)
		}
	}
	return &cloned
}
