// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Contact represents a low-level representation of the Contact definitions found at
//
//	v2 - https://swagger.io/specification/v2/#contactObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#contact-object
type Contact struct {
	Name       low.NodeReference[string]
	URL        low.NodeReference[string]
	Email      low.NodeReference[string]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

func (c *Contact) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	c.KeyNode = keyNode
	c.RootNode = root
	c.Reference = new(low.Reference)
	c.Nodes = low.ExtractNodes(ctx, root)
	c.Extensions = low.ExtractExtensions(root)
	c.context = ctx
	c.index = idx
	return nil
}

// GetIndex will return the index.SpecIndex instance attached to the Contact object
func (c *Contact) GetIndex() *index.SpecIndex {
	return c.index
}

// GetContext will return the context.Context instance used when building the Contact object
func (c *Contact) GetContext() context.Context {
	return c.context
}

// GetRootNode will return the root yaml node of the Contact object
func (c *Contact) GetRootNode() *yaml.Node {
	return c.RootNode
}

// GetKeyNode will return the key yaml node of the Contact object
func (c *Contact) GetKeyNode() *yaml.Node {
	return c.KeyNode
}

// Hash will return a consistent hash of the Contact object
func (c *Contact) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !c.Name.IsEmpty() {
			h.WriteString(c.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !c.URL.IsEmpty() {
			h.WriteString(c.URL.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !c.Email.IsEmpty() {
			h.WriteString(c.Email.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		// Note: Extensions are not included in the hash for Contact
		return h.Sum64()
	})
}

// GetExtensions returns all extensions for Contact
func (c *Contact) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return c.Extensions
}
