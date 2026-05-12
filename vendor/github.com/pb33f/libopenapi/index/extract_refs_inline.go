// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (index *SpecIndex) collectInlineSchemaDefinition(parent, node *yaml.Node, seenPath []string, keyIndex int) {
	if keyIndex+1 >= len(node.Content) {
		return
	}

	keyNode := node.Content[keyIndex]
	valueNode := node.Content[keyIndex+1]

	var jsonPath, definitionPath, fullDefinitionPath string
	if len(seenPath) > 0 || keyNode.Value != "" {
		loc := append(seenPath, keyNode.Value)
		locPath := strings.Join(loc, "/")
		definitionPath = "#/" + locPath
		fullDefinitionPath = index.specAbsolutePath + "#/" + locPath
		_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
	}

	ref := &Reference{
		ParentNode:     parent,
		FullDefinition: fullDefinitionPath,
		Definition:     definitionPath,
		Node:           valueNode,
		KeyNode:        keyNode,
		Path:           jsonPath,
		Index:          index,
	}

	isRef, _, _ := utils.IsNodeRefValue(valueNode)
	if isRef {
		index.allRefSchemaDefinitions = append(index.allRefSchemaDefinitions, ref)
		return
	}

	if (keyNode.Value == "additionalProperties" || keyNode.Value == "unevaluatedProperties") && utils.IsNodeBoolValue(valueNode) {
		return
	}

	index.appendInlineSchemaDefinition(ref)
}

func (index *SpecIndex) collectMapSchemaDefinitions(parent, node *yaml.Node, seenPath []string, keyIndex int) {
	if keyIndex+1 >= len(node.Content) {
		return
	}

	keyNode := node.Content[keyIndex]
	propertiesNode := node.Content[keyIndex+1]

	if len(seenPath) > 0 {
		for _, p := range seenPath {
			if p == "examples" || p == "example" || strings.HasPrefix(p, "x-") {
				return
			}
		}
	}

	label := ""
	for h, prop := range propertiesNode.Content {
		if h%2 == 0 {
			label = prop.Value
			continue
		}

		var jsonPath, definitionPath, fullDefinitionPath string
		if len(seenPath) > 0 || keyNode.Value != "" && label != "" {
			loc := append(seenPath, keyNode.Value, label)
			locPath := strings.Join(loc, "/")
			definitionPath = "#/" + locPath
			fullDefinitionPath = index.specAbsolutePath + "#/" + locPath
			_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
		}

		ref := &Reference{
			ParentNode:     parent,
			FullDefinition: fullDefinitionPath,
			Definition:     definitionPath,
			Node:           prop,
			KeyNode:        keyNode,
			Path:           jsonPath,
			Index:          index,
		}

		isRef, _, _ := utils.IsNodeRefValue(prop)
		if isRef {
			index.allRefSchemaDefinitions = append(index.allRefSchemaDefinitions, ref)
			continue
		}

		index.appendInlineSchemaDefinition(ref)
	}
}

func (index *SpecIndex) collectArraySchemaDefinitions(parent, node *yaml.Node, seenPath []string, keyIndex int) {
	if keyIndex+1 >= len(node.Content) {
		return
	}

	keyNode := node.Content[keyIndex]
	arrayNode := node.Content[keyIndex+1]

	for h, element := range arrayNode.Content {
		var jsonPath, definitionPath, fullDefinitionPath string
		if len(seenPath) > 0 {
			loc := append(seenPath, keyNode.Value, strconv.Itoa(h))
			locPath := strings.Join(loc, "/")
			definitionPath = "#/" + locPath
			fullDefinitionPath = index.specAbsolutePath + "#/" + locPath
			_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
		} else {
			definitionPath = "#/" + keyNode.Value
			fullDefinitionPath = index.specAbsolutePath + "#/" + keyNode.Value
			_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
		}

		ref := &Reference{
			ParentNode:     parent,
			FullDefinition: fullDefinitionPath,
			Definition:     definitionPath,
			Node:           element,
			KeyNode:        keyNode,
			Path:           jsonPath,
			Index:          index,
		}

		isRef, _, _ := utils.IsNodeRefValue(element)
		if isRef {
			index.allRefSchemaDefinitions = append(index.allRefSchemaDefinitions, ref)
			continue
		}

		index.appendInlineSchemaDefinition(ref)
	}
}

func (index *SpecIndex) appendInlineSchemaDefinition(ref *Reference) {
	index.allInlineSchemaDefinitions = append(index.allInlineSchemaDefinitions, ref)
	if inlineSchemaIsObjectOrArray(ref.Node) {
		index.allInlineSchemaObjectDefinitions = append(index.allInlineSchemaObjectDefinitions, ref)
	}
}

func inlineSchemaIsObjectOrArray(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	k, v := utils.FindKeyNodeTop("type", node.Content)
	if k == nil || v == nil {
		return false
	}
	return v.Value == "object" || v.Value == "array"
}
