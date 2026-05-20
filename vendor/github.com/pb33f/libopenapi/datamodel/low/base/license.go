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

// License is a low-level representation of a License object as defined by OpenAPI 2 and OpenAPI 3
//
//	v2 - https://swagger.io/specification/v2/#licenseObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#license-object
type License struct {
	Name       low.NodeReference[string]
	URL        low.NodeReference[string]
	Identifier low.NodeReference[string]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// Build out a license, complain if both a URL and identifier are present as they are mutually exclusive
func (l *License) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	l.KeyNode = keyNode
	root = utils.NodeAlias(root)
	l.RootNode = root
	utils.CheckForMergeNodes(root)
	l.Reference = new(low.Reference)
	no := low.ExtractNodes(ctx, root)
	l.Extensions = low.ExtractExtensions(root)
	l.Nodes = no
	l.context = ctx
	l.index = idx
	return nil
}

// GetIndex will return the index.SpecIndex instance attached to the License object
func (l *License) GetIndex() *index.SpecIndex {
	return l.index
}

// GetContext will return the context.Context instance used when building the License object
func (l *License) GetContext() context.Context {
	return l.context
}

// GetRootNode will return the root yaml node of the License object
func (l *License) GetRootNode() *yaml.Node {
	return l.RootNode
}

// GetKeyNode will return the key yaml node of the License object
func (l *License) GetKeyNode() *yaml.Node {
	return l.KeyNode
}

// Hash will return a consistent hash of the License object
func (l *License) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !l.Name.IsEmpty() {
			h.WriteString(l.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !l.URL.IsEmpty() {
			h.WriteString(l.URL.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !l.Identifier.IsEmpty() {
			h.WriteString(l.Identifier.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		// Note: Extensions are not included in the hash for License
		return h.Sum64()
	})
}

// GetExtensions returns all extensions for License
func (l *License) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return l.Extensions
}
