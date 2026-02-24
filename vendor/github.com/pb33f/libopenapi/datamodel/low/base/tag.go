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

// Tag represents a low-level Tag instance that is backed by a low-level one.
//
// Adds metadata to a single tag that is used by the Operation Object. It is not mandatory to have a Tag Object per
// tag defined in the Operation Object instances.
//   - v2: https://swagger.io/specification/v2/#tagObject
//   - v3: https://swagger.io/specification/#tag-object
//   - v3.2: https://spec.openapis.org/oas/v3.2.0#tag-object
type Tag struct {
	Name         low.NodeReference[string]
	Summary      low.NodeReference[string]
	Description  low.NodeReference[string]
	ExternalDocs low.NodeReference[*ExternalDoc]
	Parent       low.NodeReference[string]
	Kind         low.NodeReference[string]
	Extensions   *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode      *yaml.Node
	RootNode     *yaml.Node
	index        *index.SpecIndex
	context      context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Tag object
func (t *Tag) GetIndex() *index.SpecIndex {
	return t.index
}

// GetContext returns the context.Context instance used when building the Tag object
func (t *Tag) GetContext() context.Context {
	return t.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (t *Tag) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, t.Extensions)
}

// GetRootNode returns the root yaml node of the Tag object
func (t *Tag) GetRootNode() *yaml.Node {
	return t.RootNode
}

// GetKeyNode returns the key yaml node of the Tag object
func (t *Tag) GetKeyNode() *yaml.Node {
	return t.KeyNode
}

// Build will extract extensions and external docs for the Tag.
func (t *Tag) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	t.KeyNode = keyNode
	root = utils.NodeAlias(root)
	t.RootNode = root
	utils.CheckForMergeNodes(root)
	t.Reference = new(low.Reference)
	t.Nodes = low.ExtractNodes(ctx, root)
	t.Extensions = low.ExtractExtensions(root)
	t.index = idx
	t.context = ctx

	low.ExtractExtensionNodes(ctx, t.Extensions, t.Nodes)

	// extract externalDocs
	extDocs, err := low.ExtractObject[*ExternalDoc](ctx, ExternalDocsLabel, root, idx)
	t.ExternalDocs = extDocs
	return err
}

// GetExtensions returns all Tag extensions and satisfies the low.HasExtensions interface.
func (t *Tag) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return t.Extensions
}

// Hash will return a consistent hash of the Tag object
func (t *Tag) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !t.Name.IsEmpty() {
			h.WriteString(t.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !t.Summary.IsEmpty() {
			h.WriteString(t.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !t.Description.IsEmpty() {
			h.WriteString(t.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !t.ExternalDocs.IsEmpty() {
			h.WriteString(low.GenerateHashString(t.ExternalDocs.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !t.Parent.IsEmpty() {
			h.WriteString(t.Parent.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !t.Kind.IsEmpty() {
			h.WriteString(t.Kind.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(t.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
