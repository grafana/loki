// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package overlay

import (
	"github.com/pb33f/jsonpath/pkg/jsonpath"
	"github.com/pb33f/jsonpath/pkg/jsonpath/config"
	highoverlay "github.com/pb33f/libopenapi/datamodel/high/overlay"
	"go.yaml.in/yaml/v4"
)

// Apply applies the given overlay to the target document bytes.
// It returns the modified document bytes and any warnings encountered.
func Apply(targetBytes []byte, overlay *highoverlay.Overlay) (*Result, error) {
	if overlay == nil {
		return nil, ErrInvalidOverlay
	}

	if err := validateOverlay(overlay); err != nil {
		return nil, err
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal(targetBytes, &rootNode); err != nil {
		return nil, err
	}

	// Parent index is built lazily and rebuilt after updates/copies to ensure
	// remove actions can target nodes created by earlier update/copy actions.
	var parentIdx parentIndex
	parentIdxStale := true

	var warnings []*Warning
	for _, action := range overlay.Actions {
		if action.Remove && parentIdxStale {
			parentIdx = newParentIndex(&rootNode)
			parentIdxStale = false
		}

		actionWarnings, err := applyAction(&rootNode, action, parentIdx)
		if err != nil {
			return nil, &OverlayError{Action: action, Cause: err}
		}
		warnings = append(warnings, actionWarnings...)

		// Mark parent index as stale after update or copy operations
		// (both can add new nodes that subsequent remove actions may target)
		if action.Update != nil || action.Copy != "" {
			parentIdxStale = true
		}
	}

	resultBytes, err := yaml.Marshal(&rootNode)
	if err != nil {
		return nil, err
	}

	return &Result{
		Bytes:    resultBytes,
		Warnings: warnings,
	}, nil
}

func applyAction(root *yaml.Node, action *highoverlay.Action, parentIdx parentIndex) ([]*Warning, error) {
	var warnings []*Warning

	if action.Target == "" {
		return warnings, nil
	}

	path, err := jsonpath.NewPath(action.Target, config.WithPropertyNameExtension())
	if err != nil {
		return nil, ErrInvalidJSONPath
	}

	nodes := path.Query(root)

	if len(nodes) == 0 {
		warnings = append(warnings, &Warning{
			Action:  action,
			Target:  action.Target,
			Message: "target matched zero nodes",
		})
		return warnings, nil
	}

	// Operation order per spec: copy → update → remove
	// This allows:
	// - Copy to populate the target first
	// - Update to override copied values
	// - Remove to clean up afterwards (move pattern)

	// 1. Copy (if present)
	if action.Copy != "" {
		copyWarnings, err := applyCopyAction(root, nodes, action.Copy)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, copyWarnings...)
	}

	// 2. Update (if present)
	// Validate targets for UPDATE actions (must be objects or arrays, not primitives).
	// Validation happens AFTER copy because copy may change the target node type.
	// REMOVE actions can target any node type.
	if action.Update != nil {
		for _, node := range nodes {
			if err := validateTarget(node); err != nil {
				return nil, err
			}
		}
		applyUpdateAction(nodes, action.Update)
	}

	// 3. Remove (if present)
	if action.Remove {
		applyRemoveAction(parentIdx, nodes)
	}

	return warnings, nil
}

func applyCopyAction(root *yaml.Node, targetNodes []*yaml.Node, copyPath string) ([]*Warning, error) {
	var warnings []*Warning

	path, err := jsonpath.NewPath(copyPath, config.WithPropertyNameExtension())
	if err != nil {
		return nil, ErrInvalidJSONPath
	}

	sourceNodes := path.Query(root)

	// Single-node constraint per spec: copy source must select exactly one node
	if len(sourceNodes) == 0 {
		return nil, ErrCopySourceNotFound
	}
	if len(sourceNodes) > 1 {
		return nil, ErrCopySourceMultiple
	}

	sourceNode := sourceNodes[0]

	// Type compatibility check per spec: "If the target expression and
	// copy expression do not return the same type, an error MUST be reported"
	for _, targetNode := range targetNodes {
		if sourceNode.Kind != targetNode.Kind {
			return nil, ErrCopyTypeMismatch
		}
		mergeNode(targetNode, sourceNode)
	}

	return warnings, nil
}

func applyRemoveAction(idx parentIndex, nodes []*yaml.Node) {
	for _, node := range nodes {
		removeNode(idx, node)
	}
}

func applyUpdateAction(nodes []*yaml.Node, update *yaml.Node) {
	if update.IsZero() {
		return
	}
	for _, node := range nodes {
		mergeNode(node, update)
	}
}

type parentIndex map[*yaml.Node]*yaml.Node

func newParentIndex(root *yaml.Node) parentIndex {
	index := parentIndex{}
	index.indexNodeRecursively(root)
	return index
}

func (index parentIndex) indexNodeRecursively(parent *yaml.Node) {
	for _, child := range parent.Content {
		index[child] = parent
		index.indexNodeRecursively(child)
	}
}

func (index parentIndex) getParent(child *yaml.Node) *yaml.Node {
	return index[child]
}

func removeNode(idx parentIndex, node *yaml.Node) {
	parent := idx.getParent(node)
	if parent == nil {
		return
	}

	for i, child := range parent.Content {
		if child == node {
			switch parent.Kind {
			case yaml.MappingNode:
				// JSONPath returns value nodes (odd indices), so remove both key and value
				parent.Content = append(parent.Content[:i-1], parent.Content[i+1:]...)
				return
			case yaml.SequenceNode:
				parent.Content = append(parent.Content[:i], parent.Content[i+1:]...)
				return
			}
		}
	}
}

func mergeNode(node *yaml.Node, merge *yaml.Node) {
	if node.Kind != merge.Kind {
		*node = *cloneNode(merge)
		return
	}
	switch node.Kind {
	default:
		node.Value = merge.Value
	case yaml.MappingNode:
		mergeMappingNode(node, merge)
	case yaml.SequenceNode:
		mergeSequenceNode(node, merge)
	}
}

func mergeMappingNode(node *yaml.Node, merge *yaml.Node) {
NextKey:
	for i := 0; i < len(merge.Content); i += 2 {
		mergeKey := merge.Content[i].Value
		mergeValue := merge.Content[i+1]

		for j := 0; j < len(node.Content); j += 2 {
			nodeKey := node.Content[j].Value
			if nodeKey == mergeKey {
				mergeNode(node.Content[j+1], mergeValue)
				continue NextKey
			}
		}

		node.Content = append(node.Content, merge.Content[i], cloneNode(mergeValue))
	}
}

func mergeSequenceNode(node *yaml.Node, merge *yaml.Node) {
	// clone each child individually to avoid wasteful intermediate allocation
	for _, child := range merge.Content {
		node.Content = append(node.Content, cloneNode(child))
	}
}

func cloneNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	newNode := &yaml.Node{
		Kind:        node.Kind,
		Style:       node.Style,
		Tag:         node.Tag,
		Value:       node.Value,
		Anchor:      node.Anchor,
		HeadComment: node.HeadComment,
		LineComment: node.LineComment,
		FootComment: node.FootComment,
	}
	if node.Alias != nil {
		newNode.Alias = cloneNode(node.Alias)
	}
	if node.Content != nil {
		newNode.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			newNode.Content[i] = cloneNode(child)
		}
	}
	return newNode
}
