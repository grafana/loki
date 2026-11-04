// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
)

// XML represents a low-level representation of an XML object defined by all versions of OpenAPI.
//
// A metadata object that allows for more fine-tuned XML model definitions.
//
// When using arrays, XML element names are not inferred (for singular/plural forms) and the name property SHOULD be
// used to add that information. See examples for expected behavior.
//
//	v2 - https://swagger.io/specification/v2/#xmlObject
//	v3 - https://swagger.io/specification/#xml-object
type XML struct {
	Name       low.NodeReference[string]
	Namespace  low.NodeReference[string]
	Prefix     low.NodeReference[string]
	Attribute  low.NodeReference[bool]
	NodeType   low.NodeReference[string] // OpenAPI 3.2+ nodeType field (replaces deprecated attribute field)
	Wrapped    low.NodeReference[bool]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	nodeStore  low.NodeLines
	reference  low.Reference
	*low.Reference
	low.NodeMap
}

// Build will extract extensions from the XML instance.
func (x *XML) Build(root *yaml.Node, idx *index.SpecIndex) error {
	x.reference = low.Reference{}
	x.Reference = &x.reference
	x.nodeStore = low.NodeLines{}
	x.Nodes = &x.nodeStore
	x.index = idx
	if root == nil {
		x.RootNode = nil
		x.Extensions = nil
		return nil
	}
	root = utils.ResolveMergeNodes(root)
	x.RootNode = root
	if len(root.Content) > 0 {
		x.NodeMap.ExtractNodes(root, false)
	} else {
		x.AddNode(root.Line, root)
	}
	x.Extensions = low.ExtractExtensions(root)
	return nil
}

// GetExtensions returns all Tag extensions and satisfies the low.HasExtensions interface.
func (x *XML) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return x.Extensions
}

// GetRootNode returns the root yaml node of the Tag object
func (x *XML) GetRootNode() *yaml.Node {
	return x.RootNode
}

// GetIndex returns the index of the XML object
func (x *XML) GetIndex() *index.SpecIndex {
	return x.index
}

// Hash generates a hash of the XML object using properties
func (x *XML) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !x.Name.IsEmpty() {
			low.HashString(h, "name", x.Name.Value)
		}
		if !x.Namespace.IsEmpty() {
			low.HashString(h, "namespace", x.Namespace.Value)
		}
		if !x.Prefix.IsEmpty() {
			low.HashString(h, "prefix", x.Prefix.Value)
		}
		if !x.Attribute.IsEmpty() {
			low.HashLabel(h, "attribute")
			low.HashBool(h, x.Attribute.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !x.NodeType.IsEmpty() {
			low.HashString(h, "nodeType", x.NodeType.Value)
		}
		if !x.Wrapped.IsEmpty() {
			low.HashLabel(h, "wrapped")
			low.HashBool(h, x.Wrapped.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(x.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
