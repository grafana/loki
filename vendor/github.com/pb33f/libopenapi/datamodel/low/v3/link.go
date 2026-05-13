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

// Link represents a low-level OpenAPI 3+ Link object.
//
// The Link object represents a possible design-time link for a response. The presence of a link does not guarantee the
// caller’s ability to successfully invoke it, rather it provides a known relationship and traversal mechanism between
// responses and other operations.
//
// Unlike dynamic links (i.e. links provided in the response payload), the OAS linking mechanism does not require
// link information in the runtime response.
//
// For computing links, and providing instructions to execute them, a runtime expression is used for accessing values
// in an operation and using them as parameters while invoking the linked operation.
//   - https://spec.openapis.org/oas/v3.1.0#link-object
type Link struct {
	OperationRef low.NodeReference[string]
	OperationId  low.NodeReference[string]
	Parameters   low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[string]]]
	RequestBody  low.NodeReference[string]
	Description  low.NodeReference[string]
	Server       low.NodeReference[*Server]
	Extensions   *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode      *yaml.Node
	RootNode     *yaml.Node
	index        *index.SpecIndex
	context      context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Link object
func (l *Link) GetIndex() *index.SpecIndex {
	return l.index
}

// GetContext returns the context.Context instance used when building the Link object
func (l *Link) GetContext() context.Context {
	return l.context
}

// GetExtensions returns all Link extensions and satisfies the low.HasExtensions interface.
func (l *Link) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return l.Extensions
}

// FindParameter will attempt to locate a parameter string value, using a parameter name input.
func (l *Link) FindParameter(pName string) *low.ValueReference[string] {
	return low.FindItemInOrderedMap[string](pName, l.Parameters.Value)
}

// FindExtension will attempt to locate an extension with a specific key
func (l *Link) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, l.Extensions)
}

// GetRootNode returns the root yaml node of the Link object
func (l *Link) GetRootNode() *yaml.Node {
	return l.RootNode
}

// GetKeyNode returns the key yaml node of the Link object
func (l *Link) GetKeyNode() *yaml.Node {
	return l.KeyNode
}

// Build will extract extensions and servers from the node.
func (l *Link) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	l.KeyNode = keyNode
	l.Reference = new(low.Reference)
	if ok, _, ref := utils.IsNodeRefValue(root); ok {
		l.SetReference(ref, root)
	}
	root = utils.NodeAlias(root)
	l.RootNode = root
	utils.CheckForMergeNodes(root)
	l.Nodes = low.ExtractNodes(ctx, root)
	l.Extensions = low.ExtractExtensions(root)
	l.index = idx
	l.context = ctx
	low.ExtractExtensionNodes(ctx, l.Extensions, l.Nodes)

	// extract parameter nodes.
	if l.Parameters.Value != nil && l.Parameters.Value.Len() > 0 {
		for k := range l.Parameters.Value.KeysFromOldest() {
			l.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}

	// extract server.
	ser, sErr := low.ExtractObject[*Server](ctx, ServerLabel, root, idx)
	if sErr != nil {
		return sErr
	}
	l.Server = ser
	return nil
}

// Hash will return a consistent Hash of the Link object
func (l *Link) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if l.Description.Value != "" {
			h.WriteString(l.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if l.OperationRef.Value != "" {
			h.WriteString(l.OperationRef.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if l.OperationId.Value != "" {
			h.WriteString(l.OperationId.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if l.RequestBody.Value != "" {
			h.WriteString(l.RequestBody.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if l.Server.Value != nil {
			h.WriteString(low.GenerateHashString(l.Server.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for v := range orderedmap.SortAlpha(l.Parameters.Value).ValuesFromOldest() {
			h.WriteString(v.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(l.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
