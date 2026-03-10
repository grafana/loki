// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Example represents a low-level Example object as defined by OpenAPI 3+
//
//	v3 - https://spec.openapis.org/oas/v3.1.0#example-object
type Example struct {
	Summary         low.NodeReference[string]
	Description     low.NodeReference[string]
	Value           low.NodeReference[*yaml.Node]
	ExternalValue   low.NodeReference[string]
	DataValue       low.NodeReference[*yaml.Node] // OpenAPI 3.2+ dataValue field (mutually exclusive with value/externalValue)
	SerializedValue low.NodeReference[string]     // OpenAPI 3.2+ serializedValue field (mutually exclusive with value/externalValue)
	Extensions      *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode         *yaml.Node
	RootNode        *yaml.Node
	index           *index.SpecIndex
	context         context.Context
	*low.Reference
	low.NodeMap
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (ex *Example) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap[*yaml.Node](ext, ex.Extensions)
}

// GetRootNode will return the root yaml node of the Example object
func (ex *Example) GetRootNode() *yaml.Node {
	return ex.RootNode
}

// GetKeyNode will return the key yaml node of the Example object
func (ex *Example) GetKeyNode() *yaml.Node {
	return ex.KeyNode
}

// Hash will return a consistent hash of the Example object
func (ex *Example) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if ex.Summary.Value != "" {
			h.WriteString(ex.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if ex.Description.Value != "" {
			h.WriteString(ex.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if ex.Value.Value != nil && !ex.Value.Value.IsZero() {
			h.WriteString(low.GenerateHashString(ex.Value.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if ex.ExternalValue.Value != "" {
			h.WriteString(ex.ExternalValue.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if ex.DataValue.Value != nil && !ex.DataValue.Value.IsZero() {
			h.WriteString(low.GenerateHashString(ex.DataValue.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if ex.SerializedValue.Value != "" {
			h.WriteString(ex.SerializedValue.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(ex.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// Build extracts extensions and example value
func (ex *Example) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	ex.KeyNode = keyNode
	ex.Reference = new(low.Reference)
	if ok, _, ref := utils.IsNodeRefValue(root); ok {
		ex.SetReference(ref, root)
	}
	root = utils.NodeAlias(root)
	ex.RootNode = root
	utils.CheckForMergeNodes(root)
	ex.Nodes = low.ExtractNodes(ctx, root)
	ex.Extensions = low.ExtractExtensions(root)
	ex.context = ctx
	ex.index = idx

	_, ln, vn := utils.FindKeyNodeFull(ValueLabel, root.Content)
	_, dataLn, dataVn := utils.FindKeyNodeFull(DataValueLabel, root.Content)
	_, serializedLn, serializedVn := utils.FindKeyNodeFull(SerializedValueLabel, root.Content)

	if vn != nil {
		ex.Value = low.NodeReference[*yaml.Node]{
			Value:     vn,
			KeyNode:   ln,
			ValueNode: vn,
		}

		// extract nodes for all value nodes down the tree.
		expChildNodes := low.ExtractNodesRecursive(ctx, vn)
		expChildNodes.Range(func(k, v interface{}) bool {
			if arr, ko := v.([]*yaml.Node); ko {
				ex.Nodes.Store(k, arr)
			}
			return true
		})
	}

	// OpenAPI 3.2+ dataValue field
	if dataVn != nil {
		ex.DataValue = low.NodeReference[*yaml.Node]{
			Value:     dataVn,
			KeyNode:   dataLn,
			ValueNode: dataVn,
		}

		// extract nodes for all dataValue nodes down the tree.
		expChildNodes := low.ExtractNodesRecursive(ctx, dataVn)
		expChildNodes.Range(func(k, v interface{}) bool {
			if arr, ko := v.([]*yaml.Node); ko {
				ex.Nodes.Store(k, arr)
			}
			return true
		})
	}

	// OpenAPI 3.2+ serializedValue field
	if serializedVn != nil {
		ex.SerializedValue = low.NodeReference[string]{
			Value:     serializedVn.Value,
			KeyNode:   serializedLn,
			ValueNode: serializedVn,
		}
	}

	return nil
}

// GetExtensions will return Example extensions to satisfy the HasExtensions interface.
func (ex *Example) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return ex.Extensions
}

// GetIndex will return the index.SpecIndex instance attached to the Example object
func (ex *Example) GetIndex() *index.SpecIndex {
	return ex.index
}

// GetContext will return the context.Context instance used when building the Example object
func (ex *Example) GetContext() context.Context {
	return ex.context
}
