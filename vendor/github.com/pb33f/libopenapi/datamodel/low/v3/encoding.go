// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"fmt"
	"hash/maphash"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
)

// Encoding represents a low-level OpenAPI 3+ Encoding object
//   - https://spec.openapis.org/oas/v3.1.0#encoding-object
type Encoding struct {
	ContentType   low.NodeReference[string]
	Headers       low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Header]]]
	Style         low.NodeReference[string]
	Explode       low.NodeReference[bool]
	AllowReserved low.NodeReference[bool]
	KeyNode       *yaml.Node
	RootNode      *yaml.Node
	index         *index.SpecIndex
	context       context.Context
	nodeStore     low.NodeLines
	reference     low.Reference
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Encoding object
func (en *Encoding) GetIndex() *index.SpecIndex {
	return en.index
}

// GetContext returns the context.Context instance used when building the Encoding object
func (en *Encoding) GetContext() context.Context {
	return en.context
}

// FindHeader attempts to locate a Header with the supplied name
func (en *Encoding) FindHeader(hType string) *low.ValueReference[*Header] {
	return low.FindItemInOrderedMap[*Header](hType, en.Headers.Value)
}

// GetRootNode returns the root yaml node of the Encoding object
func (en *Encoding) GetRootNode() *yaml.Node {
	return en.RootNode
}

// GetKeyNode returns the key yaml node of the Encoding object
func (en *Encoding) GetKeyNode() *yaml.Node {
	return en.KeyNode
}

// Hash will return a consistent Hash of the Encoding object
func (en *Encoding) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if en.ContentType.Value != "" {
			low.HashString(h, "contentType", en.ContentType.Value)
		}
		for k, v := range orderedmap.SortAlpha(en.Headers.Value).FromOldest() {
			h.WriteString(fmt.Sprintf("%s-%x", k.Value, v.Value.Hash()))
			h.WriteByte(low.HASH_PIPE)
		}
		if en.Style.Value != "" {
			low.HashString(h, "style", en.Style.Value)
		}
		low.HashBool(h, en.Explode.Value)
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, en.AllowReserved.Value)
		h.WriteByte(low.HASH_PIPE)
		return h.Sum64()
	})
}

// Build will extract all Header objects from supplied node.
func (en *Encoding) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	en.KeyNode = keyNode
	root = utils.ResolveMergeNodes(root)
	en.RootNode = root
	en.nodeStore = low.NodeLines{}
	en.Nodes = &en.nodeStore
	if len(root.Content) > 0 {
		en.NodeMap.ExtractNodes(root, false)
	} else {
		en.AddNode(root.Line, root)
	}
	en.reference = low.Reference{}
	en.Reference = &en.reference
	en.index = idx
	en.context = ctx

	headers, hL, hN, err := low.ExtractMap[*Header](ctx, HeadersLabel, root, idx)
	if err != nil {
		return err
	}
	if headers != nil {
		en.Headers = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Header]]]{
			Value:     headers,
			KeyNode:   hL,
			ValueNode: hN,
		}
		en.Nodes.Store(hL.Line, hL)
		for k, v := range headers.FromOldest() {
			v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}
	return nil
}
