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

// Components represents a low-level Arazzo Components Object.
// https://spec.openapis.org/arazzo/v1.0.1#components-object
type Components struct {
	Inputs         low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]]
	Parameters     low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Parameter]]]
	SuccessActions low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SuccessAction]]]
	FailureActions low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*FailureAction]]]
	Extensions     *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode        *yaml.Node
	RootNode       *yaml.Node
	index          *index.SpecIndex
	context        context.Context
	*low.Reference
	low.NodeMap
}

var extractComponentsParametersMap = extractObjectMap[Parameter]
var extractComponentsSuccessActionsMap = extractObjectMap[SuccessAction]

// GetIndex returns the index.SpecIndex instance attached to the Components object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (c *Components) GetIndex() *index.SpecIndex {
	return c.index
}

// GetContext returns the context.Context instance used when building the Components object.
func (c *Components) GetContext() context.Context {
	return c.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (c *Components) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, c.Extensions)
}

// GetRootNode returns the root yaml node of the Components object.
func (c *Components) GetRootNode() *yaml.Node {
	return c.RootNode
}

// GetKeyNode returns the key yaml node of the Components object.
func (c *Components) GetKeyNode() *yaml.Node {
	return c.KeyNode
}

// Build will extract all properties of the Components object.
func (c *Components) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &c.KeyNode,
		RootNode:   &c.RootNode,
		Reference:  &c.Reference,
		NodeMap:    &c.NodeMap,
		Extensions: &c.Extensions,
		Index:      &c.index,
		Context:    &c.context,
	}, ctx, keyNode, root, idx)

	// Extract inputs as raw node map (JSON Schema objects keyed by name)
	c.Inputs = extractRawNodeMap(InputsLabel, root)

	// Extract parameters map
	params, err := extractComponentsParametersMap(ctx, ParametersLabel, root, idx)
	if err != nil {
		return err
	}
	c.Parameters = params

	// Extract successActions map
	successActions, err := extractComponentsSuccessActionsMap(ctx, SuccessActionsLabel, root, idx)
	if err != nil {
		return err
	}
	c.SuccessActions = successActions

	// Extract failureActions map
	failureActions, err := extractObjectMap[FailureAction](ctx, FailureActionsLabel, root, idx)
	if err != nil {
		return err
	}
	c.FailureActions = failureActions

	return nil
}

// GetExtensions returns all Components extensions and satisfies the low.HasExtensions interface.
func (c *Components) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return c.Extensions
}

// Hash will return a consistent hash of the Components object.
func (c *Components) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !c.Inputs.IsEmpty() && c.Inputs.Value != nil {
			for pair := c.Inputs.Value.First(); pair != nil; pair = pair.Next() {
				h.WriteString(pair.Key().Value)
				h.WriteByte(low.HASH_PIPE)
				hashYAMLNode(h, pair.Value().Value)
			}
		}
		if !c.Parameters.IsEmpty() && c.Parameters.Value != nil {
			for pair := c.Parameters.Value.First(); pair != nil; pair = pair.Next() {
				h.WriteString(pair.Key().Value)
				h.WriteByte(low.HASH_PIPE)
				low.HashUint64(h, pair.Value().Value.Hash())
			}
		}
		if !c.SuccessActions.IsEmpty() && c.SuccessActions.Value != nil {
			for pair := c.SuccessActions.Value.First(); pair != nil; pair = pair.Next() {
				h.WriteString(pair.Key().Value)
				h.WriteByte(low.HASH_PIPE)
				low.HashUint64(h, pair.Value().Value.Hash())
			}
		}
		if !c.FailureActions.IsEmpty() && c.FailureActions.Value != nil {
			for pair := c.FailureActions.Value.First(); pair != nil; pair = pair.Next() {
				h.WriteString(pair.Key().Value)
				h.WriteByte(low.HASH_PIPE)
				low.HashUint64(h, pair.Value().Value.Hash())
			}
		}
		hashExtensionsInto(h, c.Extensions)
		return h.Sum64()
	})
}
