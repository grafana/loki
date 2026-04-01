// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// arazzoBase bundles the common fields found in every Arazzo low-level struct
// so they can be initialized in a single helper call.
type arazzoBase struct {
	KeyNode    **yaml.Node
	RootNode   **yaml.Node
	Reference  **low.Reference
	NodeMap    *low.NodeMap
	Extensions **orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	Index      **index.SpecIndex
	Context    *context.Context
}

// initBuild performs the common preamble shared by every Arazzo low-level Build method.
// It returns the resolved root node (after alias/merge processing) for further extraction.
func initBuild(b *arazzoBase, ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) *yaml.Node {
	*b.KeyNode = keyNode
	root = utils.NodeAlias(root)
	*b.RootNode = root
	utils.CheckForMergeNodes(root)
	*b.Reference = new(low.Reference)
	b.NodeMap.Nodes = low.ExtractNodes(ctx, root)
	ext := low.ExtractExtensions(root)
	*b.Extensions = ext
	*b.Index = idx
	*b.Context = ctx
	low.ExtractExtensionNodes(ctx, ext, b.NodeMap.Nodes)
	return root
}

// findLabeledNode searches root's Content pairs for a key matching label.
// Returns the key node, value node, and whether the label was found.
func findLabeledNode(label string, root *yaml.Node) (key, value *yaml.Node, found bool) {
	for i := 0; i < len(root.Content); i += 2 {
		if i+1 >= len(root.Content) {
			break
		}
		if root.Content[i].Value == label {
			return root.Content[i], root.Content[i+1], true
		}
	}
	return nil, nil, false
}

// assignNodeReference centralizes the common "if err return; set field" pattern
// used by Build methods when extracting nested NodeReferences.
func assignNodeReference[T any](
	ref low.NodeReference[T],
	err error,
	assign func(low.NodeReference[T]),
) error {
	if err != nil {
		return err
	}
	assign(ref)
	return nil
}

// extractArray extracts a YAML sequence node into a slice of ValueReferences for the given label.
func extractArray[N any, T interface {
	*N
	Build(context.Context, *yaml.Node, *yaml.Node, *index.SpecIndex) error
}](
	ctx context.Context, label string, root *yaml.Node, idx *index.SpecIndex,
) (low.NodeReference[[]low.ValueReference[T]], error) {
	var result low.NodeReference[[]low.ValueReference[T]]
	key, value, found := findLabeledNode(label, root)
	if !found {
		return result, nil
	}
	result.KeyNode = key
	result.ValueNode = value
	if value.Kind != yaml.SequenceNode {
		return result, nil
	}
	items := make([]low.ValueReference[T], 0, len(value.Content))
	for _, itemNode := range value.Content {
		obj := T(new(N))
		if err := low.BuildModel(itemNode, obj); err != nil {
			return result, err
		}
		if err := obj.Build(ctx, nil, itemNode, idx); err != nil {
			return result, err
		}
		items = append(items, low.ValueReference[T]{
			Value:     obj,
			ValueNode: itemNode,
		})
	}
	result.Value = items
	return result, nil
}

// extractObjectMap extracts a YAML mapping node into an ordered map of string keys to built objects.
func extractObjectMap[N any, T interface {
	*N
	Build(context.Context, *yaml.Node, *yaml.Node, *index.SpecIndex) error
}](
	ctx context.Context, label string, root *yaml.Node, idx *index.SpecIndex,
) (low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[T]]], error) {
	var result low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[T]]]
	key, value, found := findLabeledNode(label, root)
	if !found {
		return result, nil
	}
	result.KeyNode = key
	result.ValueNode = value
	if value.Kind != yaml.MappingNode {
		return result, nil
	}
	m := orderedmap.New[low.KeyReference[string], low.ValueReference[T]]()
	for j := 0; j < len(value.Content); j += 2 {
		if j+1 >= len(value.Content) {
			break
		}
		mapKey := value.Content[j]
		mapVal := value.Content[j+1]
		obj := T(new(N))
		if err := low.BuildModel(mapVal, obj); err != nil {
			return result, err
		}
		if err := obj.Build(ctx, mapKey, mapVal, idx); err != nil {
			return result, err
		}
		m.Set(low.KeyReference[string]{
			Value:   mapKey.Value,
			KeyNode: mapKey,
		}, low.ValueReference[T]{
			Value:     obj,
			ValueNode: mapVal,
		})
	}
	result.Value = m
	return result, nil
}

// extractStringArray extracts a YAML sequence of scalar strings into a NodeReference.
func extractStringArray(label string, root *yaml.Node) low.NodeReference[[]low.ValueReference[string]] {
	var result low.NodeReference[[]low.ValueReference[string]]
	key, value, found := findLabeledNode(label, root)
	if !found {
		return result
	}
	result.KeyNode = key
	result.ValueNode = value
	if value.Kind != yaml.SequenceNode {
		return result
	}
	items := make([]low.ValueReference[string], 0, len(value.Content))
	for _, itemNode := range value.Content {
		items = append(items, low.ValueReference[string]{
			Value:     itemNode.Value,
			ValueNode: itemNode,
		})
	}
	result.Value = items
	return result
}

// extractRawNode extracts a raw *yaml.Node for a given label without further processing.
func extractRawNode(label string, root *yaml.Node) low.NodeReference[*yaml.Node] {
	var result low.NodeReference[*yaml.Node]
	key, value, found := findLabeledNode(label, root)
	if !found {
		return result
	}
	result.KeyNode = key
	result.ValueNode = value
	result.Value = value
	return result
}

// extractExpressionsMap extracts a YAML mapping node into an ordered map of string keys to string values.
func extractExpressionsMap(label string, root *yaml.Node) low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[string]]] {
	var result low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[string]]]
	key, value, found := findLabeledNode(label, root)
	if !found {
		return result
	}
	result.KeyNode = key
	result.ValueNode = value
	if value.Kind != yaml.MappingNode {
		return result
	}
	m := orderedmap.New[low.KeyReference[string], low.ValueReference[string]]()
	for j := 0; j < len(value.Content); j += 2 {
		if j+1 >= len(value.Content) {
			break
		}
		mapKey := value.Content[j]
		mapVal := value.Content[j+1]
		m.Set(low.KeyReference[string]{
			Value:   mapKey.Value,
			KeyNode: mapKey,
		}, low.ValueReference[string]{
			Value:     mapVal.Value,
			ValueNode: mapVal,
		})
	}
	result.Value = m
	return result
}

// extractRawNodeMap extracts a YAML mapping node into an ordered map of string keys to raw *yaml.Node values.
func extractRawNodeMap(label string, root *yaml.Node) low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]] {
	var result low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]]
	key, value, found := findLabeledNode(label, root)
	if !found {
		return result
	}
	result.KeyNode = key
	result.ValueNode = value
	if value.Kind != yaml.MappingNode {
		return result
	}
	m := orderedmap.New[low.KeyReference[string], low.ValueReference[*yaml.Node]]()
	for j := 0; j < len(value.Content); j += 2 {
		if j+1 >= len(value.Content) {
			break
		}
		mapKey := value.Content[j]
		mapVal := value.Content[j+1]
		m.Set(low.KeyReference[string]{
			Value:   mapKey.Value,
			KeyNode: mapKey,
		}, low.ValueReference[*yaml.Node]{
			Value:     mapVal,
			ValueNode: mapVal,
		})
	}
	result.Value = m
	return result
}

// extractComponentRef extracts a string field from root.Content by label, returning it as a NodeReference.
// Used for the 'reference' field which is renamed to ComponentRef in structs to avoid collision
// with the embedded *low.Reference.
func extractComponentRef(label string, root *yaml.Node) low.NodeReference[string] {
	key, value, found := findLabeledNode(label, root)
	if !found {
		return low.NodeReference[string]{}
	}
	return low.NodeReference[string]{
		Value:     value.Value,
		KeyNode:   key,
		ValueNode: value,
	}
}

// hashYAMLNode writes a yaml.Node tree directly into a maphash.Hash for efficient hashing.
func hashYAMLNode(h *maphash.Hash, node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.ScalarNode:
		h.WriteString(node.Value)
		h.WriteByte(low.HASH_PIPE)
	case yaml.MappingNode, yaml.SequenceNode:
		for _, child := range node.Content {
			hashYAMLNode(h, child)
		}
	case yaml.DocumentNode:
		for _, child := range node.Content {
			hashYAMLNode(h, child)
		}
	case yaml.AliasNode:
		if node.Alias != nil {
			hashYAMLNode(h, node.Alias)
		}
	}
}

// hashExtensionsInto writes extension hashes directly into the hasher without intermediate allocations.
func hashExtensionsInto(h *maphash.Hash, ext *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]) {
	if ext == nil {
		return
	}
	for pair := ext.First(); pair != nil; pair = pair.Next() {
		h.WriteString(pair.Key().Value)
		h.WriteByte(low.HASH_PIPE)
		hashYAMLNode(h, pair.Value().Value)
	}
}
