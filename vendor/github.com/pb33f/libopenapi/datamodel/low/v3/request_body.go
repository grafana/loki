// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// RequestBody represents a low-level OpenAPI 3+ RequestBody object.
//   - https://spec.openapis.org/oas/v3.1.0#request-body-object
type RequestBody struct {
	Description low.NodeReference[string]
	Content     low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*MediaType]]]
	Required    low.NodeReference[bool]
	Extensions  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode     *yaml.Node
	RootNode    *yaml.Node
	index       *index.SpecIndex
	context     context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the RequestBody object.
func (rb *RequestBody) GetIndex() *index.SpecIndex {
	return rb.index
}

// GetContext returns the context.Context instance used when building the RequestBody object.
func (rb *RequestBody) GetContext() context.Context {
	return rb.context
}

// GetRootNode returns the root yaml node of the RequestBody object.
func (rb *RequestBody) GetRootNode() *yaml.Node {
	return rb.RootNode
}

// GetKeyNode returns the key yaml node of the RequestBody object.
func (rb *RequestBody) GetKeyNode() *yaml.Node {
	return rb.KeyNode
}

// FindExtension attempts to locate an extension using the provided name.
func (rb *RequestBody) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, rb.Extensions)
}

// GetExtensions returns all RequestBody extensions and satisfies the low.HasExtensions interface.
func (rb *RequestBody) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return rb.Extensions
}

// FindContent attempts to find content/MediaType defined using a specified name.
func (rb *RequestBody) FindContent(cType string) *low.ValueReference[*MediaType] {
	return low.FindItemInOrderedMap[*MediaType](cType, rb.Content.Value)
}

// Build will extract extensions and MediaType objects from the node.
func (rb *RequestBody) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	rb.KeyNode = keyNode
	rb.Reference = new(low.Reference)
	if ok, _, ref := utils.IsNodeRefValue(root); ok {
		rb.SetReference(ref, root)
	}
	root = utils.NodeAlias(root)
	rb.RootNode = root
	utils.CheckForMergeNodes(root)
	rb.Nodes = low.ExtractNodes(ctx, root)
	rb.Extensions = low.ExtractExtensions(root)
	rb.index = idx
	rb.context = ctx

	low.ExtractExtensionNodes(ctx, rb.Extensions, rb.Nodes)

	// handle content, if set.
	con, cL, cN, cErr := low.ExtractMap[*MediaType](ctx, ContentLabel, root, idx)
	if cErr != nil {
		return cErr
	}
	if con != nil {
		rb.Content = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*MediaType]]]{
			Value:     con,
			KeyNode:   cL,
			ValueNode: cN,
		}
		rb.Nodes.Store(cL.Line, cL)
		for k, v := range con.FromOldest() {
			v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}
	return nil
}

// Hash will return a consistent Hash of the RequestBody object
func (rb *RequestBody) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if rb.Description.Value != "" {
			h.WriteString(rb.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !rb.Required.IsEmpty() {
			low.HashBool(h, rb.Required.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for v := range orderedmap.SortAlpha(rb.Content.Value).ValuesFromOldest() {
			h.WriteString(low.GenerateHashString(v.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(rb.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
