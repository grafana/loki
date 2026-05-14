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

// Arazzo represents a low-level Arazzo document.
// https://spec.openapis.org/arazzo/v1.0.1
type Arazzo struct {
	Arazzo             low.NodeReference[string]
	Info               low.NodeReference[*Info]
	SourceDescriptions low.NodeReference[[]low.ValueReference[*SourceDescription]]
	Workflows          low.NodeReference[[]low.ValueReference[*Workflow]]
	Components         low.NodeReference[*Components]
	Extensions         *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode            *yaml.Node
	RootNode           *yaml.Node
	index              *index.SpecIndex
	context            context.Context
	*low.Reference
	low.NodeMap
}

var extractArazzoSourceDescriptions = extractArray[SourceDescription]

// GetIndex returns the index.SpecIndex instance attached to the Arazzo object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (a *Arazzo) GetIndex() *index.SpecIndex {
	return a.index
}

// GetContext returns the context.Context instance used when building the Arazzo object.
func (a *Arazzo) GetContext() context.Context {
	return a.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (a *Arazzo) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, a.Extensions)
}

// GetRootNode returns the root yaml node of the Arazzo object.
func (a *Arazzo) GetRootNode() *yaml.Node {
	return a.RootNode
}

// GetKeyNode returns the key yaml node of the Arazzo object.
func (a *Arazzo) GetKeyNode() *yaml.Node {
	return a.KeyNode
}

// Build will extract all properties of the Arazzo document.
func (a *Arazzo) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &a.KeyNode,
		RootNode:   &a.RootNode,
		Reference:  &a.Reference,
		NodeMap:    &a.NodeMap,
		Extensions: &a.Extensions,
		Index:      &a.index,
		Context:    &a.context,
	}, ctx, keyNode, root, idx)

	info, err := low.ExtractObject[*Info](ctx, InfoLabel, root, idx)
	if err != nil {
		return err
	}
	a.Info = info

	sourceDescs, err := extractArazzoSourceDescriptions(ctx, SourceDescriptionsLabel, root, idx)
	if err != nil {
		return err
	}
	a.SourceDescriptions = sourceDescs

	workflows, err := extractArray[Workflow](ctx, WorkflowsLabel, root, idx)
	if err != nil {
		return err
	}
	a.Workflows = workflows

	components, err := low.ExtractObject[*Components](ctx, ComponentsLabel, root, idx)
	if err != nil {
		return err
	}
	a.Components = components

	return nil
}

// GetExtensions returns all Arazzo extensions and satisfies the low.HasExtensions interface.
func (a *Arazzo) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return a.Extensions
}

// Hash will return a consistent hash of the Arazzo object.
func (a *Arazzo) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !a.Arazzo.IsEmpty() {
			h.WriteString(a.Arazzo.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !a.Info.IsEmpty() {
			low.HashUint64(h, a.Info.Value.Hash())
		}
		if !a.SourceDescriptions.IsEmpty() {
			for _, sd := range a.SourceDescriptions.Value {
				low.HashUint64(h, sd.Value.Hash())
			}
		}
		if !a.Workflows.IsEmpty() {
			for _, w := range a.Workflows.Value {
				low.HashUint64(h, w.Value.Hash())
			}
		}
		if !a.Components.IsEmpty() {
			low.HashUint64(h, a.Components.Value.Hash())
		}
		hashExtensionsInto(h, a.Extensions)
		return h.Sum64()
	})
}
