// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// SiblingRefTransformer handles transformation of schemas with sibling properties alongside $ref
// into OpenAPI 3.1 compliant allOf structures
type SiblingRefTransformer struct {
	index *index.SpecIndex
}

// NewSiblingRefTransformer creates a new transformer instance
func NewSiblingRefTransformer(idx *index.SpecIndex) *SiblingRefTransformer {
	return &SiblingRefTransformer{
		index: idx,
	}
}

// TransformSiblingRef transforms a node with $ref and sibling properties into an allOf structure
// Example transformation:
//
//	Input:  {title: "MySchema", $ref: "#/components/schemas/Base"}
//	Output: {allOf: [{title: "MySchema"}, {$ref: "#/components/schemas/Base"}]}
func (srt *SiblingRefTransformer) TransformSiblingRef(node *yaml.Node) (*yaml.Node, error) {
	if !srt.ShouldTransform(node) {
		return node, nil // no transformation needed
	}

	siblings, refValue := srt.ExtractSiblingProperties(node)
	return srt.CreateAllOfStructure(refValue, siblings), nil
}

// CreateAllOfStructure creates an allOf node structure from ref value and sibling properties
func (srt *SiblingRefTransformer) CreateAllOfStructure(refValue string, siblings map[string]*yaml.Node) *yaml.Node {

	allOfNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "allOf"},
			{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{}},
		},
	}

	allOfArrayNode := allOfNode.Content[1]

	// first element: schema with sibling properties (excluding $ref)
	if len(siblings) > 0 {
		siblingSchemaNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for key, valueNode := range siblings {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			// create a copy of the value node to avoid modifying original
			copiedValueNode := srt.copyNode(valueNode)
			siblingSchemaNode.Content = append(siblingSchemaNode.Content, keyNode, copiedValueNode)
		}
		allOfArrayNode.Content = append(allOfArrayNode.Content, siblingSchemaNode)
	}

	// second element: the reference schema
	refSchemaNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "$ref"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: refValue},
		},
	}
	allOfArrayNode.Content = append(allOfArrayNode.Content, refSchemaNode)

	return allOfNode
}

// ExtractSiblingProperties extracts sibling properties from a node containing $ref
// returns a map of sibling properties and the $ref value
func (srt *SiblingRefTransformer) ExtractSiblingProperties(node *yaml.Node) (map[string]*yaml.Node, string) {
	if !utils.IsNodeMap(node) || len(node.Content) < 4 { // need at least $ref + one sibling
		return nil, ""
	}

	siblings := make(map[string]*yaml.Node)
	var refValue string

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 >= len(node.Content) {
			break
		}

		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		if keyNode.Value == "$ref" {
			refValue = valueNode.Value
		} else {
			siblings[keyNode.Value] = valueNode
		}
	}

	if refValue == "" || len(siblings) == 0 {
		return nil, ""
	}

	return siblings, refValue
}

// ShouldTransform determines if a node should be transformed based on configuration and content
func (srt *SiblingRefTransformer) ShouldTransform(node *yaml.Node) bool {
	if srt.index == nil || srt.index.GetConfig() == nil {
		return false
	}

	if !srt.index.GetConfig().TransformSiblingRefs {
		return false
	}

	siblings, refValue := srt.ExtractSiblingProperties(node)
	return len(siblings) > 0 && refValue != ""
}

// copyNode creates a deep copy of a yaml node to avoid modifying the original
func (srt *SiblingRefTransformer) copyNode(node *yaml.Node) *yaml.Node {
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
			copied.Content[i] = srt.copyNode(child)
		}
	}

	return copied
}
