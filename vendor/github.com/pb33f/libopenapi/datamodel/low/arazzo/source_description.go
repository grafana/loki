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

// SourceDescription represents a low-level Arazzo Source Description Object.
// https://spec.openapis.org/arazzo/v1.0.1#source-description-object
type SourceDescription struct {
	Name       low.NodeReference[string]
	URL        low.NodeReference[string]
	Type       low.NodeReference[string]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the SourceDescription object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (s *SourceDescription) GetIndex() *index.SpecIndex {
	return s.index
}

// GetContext returns the context.Context instance used when building the SourceDescription object.
func (s *SourceDescription) GetContext() context.Context {
	return s.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (s *SourceDescription) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, s.Extensions)
}

// GetRootNode returns the root yaml node of the SourceDescription object.
func (s *SourceDescription) GetRootNode() *yaml.Node {
	return s.RootNode
}

// GetKeyNode returns the key yaml node of the SourceDescription object.
func (s *SourceDescription) GetKeyNode() *yaml.Node {
	return s.KeyNode
}

// Build will extract all properties of the SourceDescription object.
func (s *SourceDescription) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &s.KeyNode,
		RootNode:   &s.RootNode,
		Reference:  &s.Reference,
		NodeMap:    &s.NodeMap,
		Extensions: &s.Extensions,
		Index:      &s.index,
		Context:    &s.context,
	}, ctx, keyNode, root, idx)
	return nil
}

// GetExtensions returns all SourceDescription extensions and satisfies the low.HasExtensions interface.
func (s *SourceDescription) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return s.Extensions
}

// Hash will return a consistent hash of the SourceDescription object.
func (s *SourceDescription) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !s.Name.IsEmpty() {
			h.WriteString(s.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !s.URL.IsEmpty() {
			h.WriteString(s.URL.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !s.Type.IsEmpty() {
			h.WriteString(s.Type.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		hashExtensionsInto(h, s.Extensions)
		return h.Sum64()
	})
}
