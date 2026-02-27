// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Parameter represents a low-level Arazzo Parameter Object.
// A parameter can be a full parameter definition or a Reusable Object with a $components reference.
// https://spec.openapis.org/arazzo/v1.0.1#parameter-object
type Parameter struct {
	Name           low.NodeReference[string]
	In             low.NodeReference[string]
	Value          low.NodeReference[*yaml.Node]
	ComponentRef   low.NodeReference[string]
	Extensions     *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode        *yaml.Node
	RootNode       *yaml.Node
	index          *index.SpecIndex
	context        context.Context
	*low.Reference
	low.NodeMap
}

// IsReusable returns true if this parameter is a Reusable Object (has a reference field).
func (p *Parameter) IsReusable() bool {
	return !p.ComponentRef.IsEmpty()
}

// GetIndex returns the index.SpecIndex instance attached to the Parameter object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (p *Parameter) GetIndex() *index.SpecIndex {
	return p.index
}

// GetContext returns the context.Context instance used when building the Parameter object.
func (p *Parameter) GetContext() context.Context {
	return p.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (p *Parameter) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, p.Extensions)
}

// GetRootNode returns the root yaml node of the Parameter object.
func (p *Parameter) GetRootNode() *yaml.Node {
	return p.RootNode
}

// GetKeyNode returns the key yaml node of the Parameter object.
func (p *Parameter) GetKeyNode() *yaml.Node {
	return p.KeyNode
}

// Build will extract all properties of the Parameter object.
func (p *Parameter) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &p.KeyNode,
		RootNode:   &p.RootNode,
		Reference:  &p.Reference,
		NodeMap:    &p.NodeMap,
		Extensions: &p.Extensions,
		Index:      &p.index,
		Context:    &p.context,
	}, ctx, keyNode, root, idx)

	p.Value = extractRawNode(ValueLabel, root)
	p.ComponentRef = extractComponentRef(ReferenceLabel, root)
	return nil
}

// GetExtensions returns all Parameter extensions and satisfies the low.HasExtensions interface.
func (p *Parameter) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return p.Extensions
}

// Hash will return a consistent hash of the Parameter object.
func (p *Parameter) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !p.ComponentRef.IsEmpty() {
			h.WriteString(p.ComponentRef.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Name.IsEmpty() {
			h.WriteString(p.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.In.IsEmpty() {
			h.WriteString(p.In.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Value.IsEmpty() {
			hashYAMLNode(h, p.Value.Value)
		}
		hashExtensionsInto(h, p.Extensions)
		return h.Sum64()
	})
}
