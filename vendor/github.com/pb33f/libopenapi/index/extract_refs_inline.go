// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// buildDefinitionPath assembles absPath + "#/" + seenPath + extra segments joined by
// '/' in a single allocation. The "#/..." definition is a zero-copy suffix slice of
// the result, so callers derive both strings from one build.
func buildDefinitionPath(absPath string, seenPath []string, extra ...string) string {
	size := len(absPath) + 2
	for _, s := range seenPath {
		size += len(s) + 1
	}
	for _, s := range extra {
		size += len(s) + 1
	}
	var b strings.Builder
	b.Grow(size)
	b.WriteString(absPath)
	b.WriteString("#/")
	first := true
	for _, s := range seenPath {
		if !first {
			b.WriteByte('/')
		}
		b.WriteString(s)
		first = false
	}
	for _, s := range extra {
		if !first {
			b.WriteByte('/')
		}
		b.WriteString(s)
		first = false
	}
	return b.String()
}

func (index *SpecIndex) collectInlineSchemaDefinition(parent, node *yaml.Node, seenPath []string, keyIndex int) {
	if keyIndex+1 >= len(node.Content) {
		return
	}

	keyNode := node.Content[keyIndex]
	valueNode := node.Content[keyIndex+1]

	var jsonPath, definitionPath, fullDefinitionPath string
	if len(seenPath) > 0 || keyNode.Value != "" {
		fullDefinitionPath = buildDefinitionPath(index.specAbsolutePath, seenPath, keyNode.Value)
		definitionPath = fullDefinitionPath[len(index.specAbsolutePath):]
		if !index.skipMetadataCollection() {
			_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
		}
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
	prefix := ""
	for h, prop := range propertiesNode.Content {
		if h%2 == 0 {
			label = prop.Value
			continue
		}

		var jsonPath, definitionPath, fullDefinitionPath string
		if len(seenPath) > 0 || keyNode.Value != "" && label != "" {
			if prefix == "" {
				prefix = buildDefinitionPath(index.specAbsolutePath, seenPath, keyNode.Value)
			}
			fullDefinitionPath = prefix + "/" + label
			definitionPath = fullDefinitionPath[len(index.specAbsolutePath):]
			if !index.skipMetadataCollection() {
				_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
			}
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

	prefix := ""
	for h, element := range arrayNode.Content {
		var jsonPath, definitionPath, fullDefinitionPath string
		if len(seenPath) > 0 {
			if prefix == "" {
				prefix = buildDefinitionPath(index.specAbsolutePath, seenPath, keyNode.Value)
			}
			fullDefinitionPath = prefix + "/" + strconv.Itoa(h)
			definitionPath = fullDefinitionPath[len(index.specAbsolutePath):]
		} else {
			definitionPath = "#/" + keyNode.Value
			fullDefinitionPath = index.specAbsolutePath + "#/" + keyNode.Value
		}
		if !index.skipMetadataCollection() {
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
