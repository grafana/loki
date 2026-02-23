// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"fmt"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// PropertyMerger handles merging of local properties with referenced schema properties
type PropertyMerger struct {
	strategy datamodel.PropertyMergeStrategy
}

// NewPropertyMerger creates a new property merger with the specified strategy
func NewPropertyMerger(strategy datamodel.PropertyMergeStrategy) *PropertyMerger {
	return &PropertyMerger{
		strategy: strategy,
	}
}

// MergeProperties merges local properties with referenced schema properties based on strategy
// localNode contains properties that should be preserved (e.g., examples, descriptions)
// referencedNode contains the resolved reference content
func (pm *PropertyMerger) MergeProperties(localNode, referencedNode *yaml.Node) (*yaml.Node, error) {
	if localNode == nil && referencedNode == nil {
		return nil, nil
	}
	if localNode == nil {
		return pm.copyNode(referencedNode), nil
	}
	if referencedNode == nil {
		return pm.copyNode(localNode), nil
	}

	// extract properties from both nodes
	localProps := pm.extractProperties(localNode)
	referencedProps := pm.extractProperties(referencedNode)

	// create merged node starting with referenced content
	merged := pm.copyNode(referencedNode)
	mergedProps := pm.extractProperties(merged)

	// apply merge strategy for each local property
	for key, localValue := range localProps {
		if _, exists := referencedProps[key]; exists {
			// property exists in both - apply strategy
			switch pm.strategy {
			case datamodel.PreserveLocal:
				mergedProps[key] = localValue
			case datamodel.OverwriteWithRemote:
				// keep referenced value (already in merged)
				continue
			case datamodel.RejectConflicts:
				return nil, fmt.Errorf("property conflict: '%s' exists in both local and referenced schema", key)
			}
		} else {
			// property only exists locally - always preserve
			mergedProps[key] = localValue
		}
	}

	// rebuild the merged node content
	return pm.rebuildNodeFromProperties(merged, mergedProps), nil
}

// extractProperties extracts key-value pairs from a yaml mapping node
func (pm *PropertyMerger) extractProperties(node *yaml.Node) map[string]*yaml.Node {
	props := make(map[string]*yaml.Node)
	if !utils.IsNodeMap(node) {
		return props
	}

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i].Value
			value := node.Content[i+1]
			props[key] = value
		}
	}
	return props
}

// rebuildNodeFromProperties reconstructs a yaml mapping node from property map
func (pm *PropertyMerger) rebuildNodeFromProperties(baseNode *yaml.Node, props map[string]*yaml.Node) *yaml.Node {
	result := &yaml.Node{
		Kind:        yaml.MappingNode,
		Style:       baseNode.Style,
		Tag:         baseNode.Tag,
		Line:        baseNode.Line,
		Column:      baseNode.Column,
		HeadComment: baseNode.HeadComment,
		LineComment: baseNode.LineComment,
		FootComment: baseNode.FootComment,
	}

	// rebuild content from properties
	for key, value := range props {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		result.Content = append(result.Content, keyNode, pm.copyNode(value))
	}

	return result
}

// copyNode creates a deep copy of a yaml node
func (pm *PropertyMerger) copyNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}

	copied := &yaml.Node{
		Kind:        node.Kind,
		Style:       node.Style,
		Tag:         node.Tag,
		Value:       node.Value,
		Anchor:      node.Anchor,
		Alias:       node.Alias,
		Line:        node.Line,
		Column:      node.Column,
		HeadComment: node.HeadComment,
		LineComment: node.LineComment,
		FootComment: node.FootComment,
	}

	if node.Content != nil {
		copied.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			copied.Content[i] = pm.copyNode(child)
		}
	}

	return copied
}

// ShouldMergeProperties determines if property merging should be applied based on configuration
func (pm *PropertyMerger) ShouldMergeProperties(localNode, referencedNode *yaml.Node, config *datamodel.DocumentConfiguration) bool {
	if config == nil || !config.MergeReferencedProperties {
		return false
	}

	// only merge if both nodes have properties to merge
	localProps := pm.extractProperties(localNode)
	referencedProps := pm.extractProperties(referencedNode)

	return len(localProps) > 0 && len(referencedProps) > 0
}
