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

// Criterion represents a low-level Arazzo Criterion Object.
// https://spec.openapis.org/arazzo/v1.0.1#criterion-object
type Criterion struct {
	Context    low.NodeReference[string]
	Condition  low.NodeReference[string]
	Type       low.NodeReference[*yaml.Node]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Criterion object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (c *Criterion) GetIndex() *index.SpecIndex {
	return c.index
}

// GetContext returns the context.Context instance used when building the Criterion object.
func (c *Criterion) GetContext() context.Context {
	return c.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (c *Criterion) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, c.Extensions)
}

// GetRootNode returns the root yaml node of the Criterion object.
func (c *Criterion) GetRootNode() *yaml.Node {
	return c.RootNode
}

// GetKeyNode returns the key yaml node of the Criterion object.
func (c *Criterion) GetKeyNode() *yaml.Node {
	return c.KeyNode
}

// Build will extract all properties of the Criterion object.
// The Type field is a union: it can be a scalar string ("simple", "regex") or a mapping node
// (CriterionExpressionType). We store it as a raw *yaml.Node for the high-level to interpret.
func (c *Criterion) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &c.KeyNode,
		RootNode:   &c.RootNode,
		Reference:  &c.Reference,
		NodeMap:    &c.NodeMap,
		Extensions: &c.Extensions,
		Index:      &c.index,
		Context:    &c.context,
	}, ctx, keyNode, root, idx)

	// Extract type as raw node since it's a union type
	c.Type = extractRawNode(TypeLabel, root)
	return nil
}

// GetExtensions returns all Criterion extensions and satisfies the low.HasExtensions interface.
func (c *Criterion) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return c.Extensions
}

// Hash will return a consistent hash of the Criterion object.
func (c *Criterion) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !c.Context.IsEmpty() {
			h.WriteString(c.Context.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !c.Condition.IsEmpty() {
			h.WriteString(c.Condition.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !c.Type.IsEmpty() {
			hashYAMLNode(h, c.Type.Value)
		}
		hashExtensionsInto(h, c.Extensions)
		return h.Sum64()
	})
}
