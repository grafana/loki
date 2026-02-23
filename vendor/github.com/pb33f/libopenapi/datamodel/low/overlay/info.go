// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package overlay

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Info represents a low-level Overlay Info Object.
// https://spec.openapis.org/overlay/v1.1.0#info-object
type Info struct {
	Title       low.NodeReference[string]
	Version     low.NodeReference[string]
	Description low.NodeReference[string]
	Extensions  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode     *yaml.Node
	RootNode    *yaml.Node
	index       *index.SpecIndex
	context     context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Info object
func (i *Info) GetIndex() *index.SpecIndex {
	return i.index
}

// GetContext returns the context.Context instance used when building the Info object
func (i *Info) GetContext() context.Context {
	return i.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (i *Info) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, i.Extensions)
}

// GetRootNode returns the root yaml node of the Info object
func (i *Info) GetRootNode() *yaml.Node {
	return i.RootNode
}

// GetKeyNode returns the key yaml node of the Info object
func (i *Info) GetKeyNode() *yaml.Node {
	return i.KeyNode
}

// Build will extract extensions for the Info object.
func (i *Info) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	i.KeyNode = keyNode
	root = utils.NodeAlias(root)
	i.RootNode = root
	utils.CheckForMergeNodes(root)
	i.Reference = new(low.Reference)
	i.Nodes = low.ExtractNodes(ctx, root)
	i.Extensions = low.ExtractExtensions(root)
	i.index = idx
	i.context = ctx
	low.ExtractExtensionNodes(ctx, i.Extensions, i.Nodes)
	return nil
}

// GetExtensions returns all Info extensions and satisfies the low.HasExtensions interface.
func (i *Info) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return i.Extensions
}

// Hash will return a consistent Hash of the Info object
func (inf *Info) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !inf.Title.IsEmpty() {
			h.WriteString(inf.Title.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !inf.Version.IsEmpty() {
			h.WriteString(inf.Version.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !inf.Description.IsEmpty() {
			h.WriteString(inf.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(inf.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
