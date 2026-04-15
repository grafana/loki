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

// Info represents a low-level Arazzo Info Object.
// https://spec.openapis.org/arazzo/v1.0.1#info-object
type Info struct {
	Title       low.NodeReference[string]
	Summary     low.NodeReference[string]
	Description low.NodeReference[string]
	Version     low.NodeReference[string]
	Extensions  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode     *yaml.Node
	RootNode    *yaml.Node
	index       *index.SpecIndex
	context     context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Info object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (i *Info) GetIndex() *index.SpecIndex {
	return i.index
}

// GetContext returns the context.Context instance used when building the Info object.
func (i *Info) GetContext() context.Context {
	return i.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (i *Info) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, i.Extensions)
}

// GetRootNode returns the root yaml node of the Info object.
func (i *Info) GetRootNode() *yaml.Node {
	return i.RootNode
}

// GetKeyNode returns the key yaml node of the Info object.
func (i *Info) GetKeyNode() *yaml.Node {
	return i.KeyNode
}

// Build will extract all properties of the Info object.
func (i *Info) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &i.KeyNode,
		RootNode:   &i.RootNode,
		Reference:  &i.Reference,
		NodeMap:    &i.NodeMap,
		Extensions: &i.Extensions,
		Index:      &i.index,
		Context:    &i.context,
	}, ctx, keyNode, root, idx)
	return nil
}

// GetExtensions returns all Info extensions and satisfies the low.HasExtensions interface.
func (i *Info) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return i.Extensions
}

// Hash will return a consistent hash of the Info object.
func (i *Info) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !i.Title.IsEmpty() {
			h.WriteString(i.Title.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Summary.IsEmpty() {
			h.WriteString(i.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Description.IsEmpty() {
			h.WriteString(i.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Version.IsEmpty() {
			h.WriteString(i.Version.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		hashExtensionsInto(h, i.Extensions)
		return h.Sum64()
	})
}
