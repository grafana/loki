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

// Overlay represents a low-level OpenAPI Overlay document.
// https://spec.openapis.org/overlay/v1.0.0
type Overlay struct {
	Overlay    low.NodeReference[string]
	Info       low.NodeReference[*Info]
	Extends    low.NodeReference[string]
	Actions    low.NodeReference[[]low.ValueReference[*Action]]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Overlay object
func (o *Overlay) GetIndex() *index.SpecIndex {
	return o.index
}

// GetContext returns the context.Context instance used when building the Overlay object
func (o *Overlay) GetContext() context.Context {
	return o.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (o *Overlay) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, o.Extensions)
}

// GetRootNode returns the root yaml node of the Overlay object
func (o *Overlay) GetRootNode() *yaml.Node {
	return o.RootNode
}

// GetKeyNode returns the key yaml node of the Overlay object
func (o *Overlay) GetKeyNode() *yaml.Node {
	return o.KeyNode
}

// Build will extract all properties of the Overlay document.
func (o *Overlay) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	o.KeyNode = keyNode
	root = utils.NodeAlias(root)
	o.RootNode = root
	utils.CheckForMergeNodes(root)
	o.Reference = new(low.Reference)
	o.Nodes = low.ExtractNodes(ctx, root)
	o.Extensions = low.ExtractExtensions(root)
	o.index = idx
	o.context = ctx
	low.ExtractExtensionNodes(ctx, o.Extensions, o.Nodes)

	// Extract info object
	info, err := low.ExtractObject[*Info](ctx, InfoLabel, root, idx)
	if err != nil {
		return err
	}
	o.Info = info

	// Extract actions array
	o.Actions = o.extractActions(ctx, root, idx)

	return nil
}

func (o *Overlay) extractActions(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) low.NodeReference[[]low.ValueReference[*Action]] {
	var result low.NodeReference[[]low.ValueReference[*Action]]

	for i := 0; i < len(root.Content); i += 2 {
		if i+1 >= len(root.Content) {
			break
		}
		key := root.Content[i]
		value := root.Content[i+1]

		if key.Value == ActionsLabel {
			result.KeyNode = key
			result.ValueNode = value

			if value.Kind != yaml.SequenceNode {
				continue
			}

			actions := make([]low.ValueReference[*Action], 0, len(value.Content))
			for _, actionNode := range value.Content {
				action := &Action{}
				_ = low.BuildModel(actionNode, action)
				_ = action.Build(ctx, nil, actionNode, idx)
				actions = append(actions, low.ValueReference[*Action]{
					Value:     action,
					ValueNode: actionNode,
				})
			}
			result.Value = actions
			break
		}
	}
	return result
}

// GetExtensions returns all Overlay extensions and satisfies the low.HasExtensions interface.
func (o *Overlay) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return o.Extensions
}

// Hash will return a consistent Hash of the Overlay object
func (o *Overlay) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !o.Overlay.IsEmpty() {
			h.WriteString(o.Overlay.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.Info.IsEmpty() {
			h.WriteString(low.GenerateHashString(o.Info.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.Extends.IsEmpty() {
			h.WriteString(o.Extends.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.Actions.IsEmpty() {
			for _, action := range o.Actions.Value {
				h.WriteString(low.GenerateHashString(action.Value))
				h.WriteByte(low.HASH_PIPE)
			}
		}
		for _, ext := range low.HashExtensions(o.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
