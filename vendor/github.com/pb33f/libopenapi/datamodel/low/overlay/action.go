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

// Action represents a low-level Overlay Action Object.
// https://spec.openapis.org/overlay/v1.1.0#action-object
type Action struct {
	Target      low.NodeReference[string]
	Description low.NodeReference[string]
	Update      low.NodeReference[*yaml.Node]
	Remove      low.NodeReference[bool]
	Copy        low.NodeReference[string]
	Extensions  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode     *yaml.Node
	RootNode    *yaml.Node
	index       *index.SpecIndex
	context     context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Action object
func (a *Action) GetIndex() *index.SpecIndex {
	return a.index
}

// GetContext returns the context.Context instance used when building the Action object
func (a *Action) GetContext() context.Context {
	return a.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (a *Action) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, a.Extensions)
}

// GetRootNode returns the root yaml node of the Action object
func (a *Action) GetRootNode() *yaml.Node {
	return a.RootNode
}

// GetKeyNode returns the key yaml node of the Action object
func (a *Action) GetKeyNode() *yaml.Node {
	return a.KeyNode
}

// Build will extract extensions for the Action object.
func (a *Action) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	a.KeyNode = keyNode
	root = utils.NodeAlias(root)
	a.RootNode = root
	utils.CheckForMergeNodes(root)
	a.Reference = new(low.Reference)
	a.Nodes = low.ExtractNodes(ctx, root)
	a.Extensions = low.ExtractExtensions(root)
	a.index = idx
	a.context = ctx
	low.ExtractExtensionNodes(ctx, a.Extensions, a.Nodes)

	// Extract the update node directly if present
	for i := 0; i < len(root.Content); i += 2 {
		if i+1 < len(root.Content) && root.Content[i].Value == UpdateLabel {
			a.Update = low.NodeReference[*yaml.Node]{
				Value:     root.Content[i+1],
				KeyNode:   root.Content[i],
				ValueNode: root.Content[i+1],
			}
			break
		}
	}
	return nil
}

// GetExtensions returns all Action extensions and satisfies the low.HasExtensions interface.
func (a *Action) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return a.Extensions
}

// Hash will return a consistent Hash of the Action object
func (a *Action) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !a.Target.IsEmpty() {
			h.WriteString(a.Target.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !a.Description.IsEmpty() {
			h.WriteString(a.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !a.Update.IsEmpty() {
			h.WriteString(low.GenerateHashString(a.Update.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !a.Remove.IsEmpty() {
			low.HashBool(h, a.Remove.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !a.Copy.IsEmpty() {
			h.WriteString(a.Copy.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(a.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
