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

// CriterionExpressionType represents a low-level Arazzo Criterion Expression Type Object.
// https://spec.openapis.org/arazzo/v1.0.1#criterion-expression-type-object
type CriterionExpressionType struct {
	Type       low.NodeReference[string]
	Version    low.NodeReference[string]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the CriterionExpressionType object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (c *CriterionExpressionType) GetIndex() *index.SpecIndex {
	return c.index
}

// GetContext returns the context.Context instance used when building the CriterionExpressionType object.
func (c *CriterionExpressionType) GetContext() context.Context {
	return c.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (c *CriterionExpressionType) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, c.Extensions)
}

// GetRootNode returns the root yaml node of the CriterionExpressionType object.
func (c *CriterionExpressionType) GetRootNode() *yaml.Node {
	return c.RootNode
}

// GetKeyNode returns the key yaml node of the CriterionExpressionType object.
func (c *CriterionExpressionType) GetKeyNode() *yaml.Node {
	return c.KeyNode
}

// Build will extract all properties of the CriterionExpressionType object.
func (c *CriterionExpressionType) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &c.KeyNode,
		RootNode:   &c.RootNode,
		Reference:  &c.Reference,
		NodeMap:    &c.NodeMap,
		Extensions: &c.Extensions,
		Index:      &c.index,
		Context:    &c.context,
	}, ctx, keyNode, root, idx)
	return nil
}

// GetExtensions returns all CriterionExpressionType extensions and satisfies the low.HasExtensions interface.
func (c *CriterionExpressionType) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return c.Extensions
}

// Hash will return a consistent hash of the CriterionExpressionType object.
func (c *CriterionExpressionType) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !c.Type.IsEmpty() {
			h.WriteString(c.Type.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !c.Version.IsEmpty() {
			h.WriteString(c.Version.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		hashExtensionsInto(h, c.Extensions)
		return h.Sum64()
	})
}
