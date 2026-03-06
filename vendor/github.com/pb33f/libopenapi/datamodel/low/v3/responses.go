// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"fmt"
	"hash/maphash"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Responses represents a low-level OpenAPI 3+ Responses object.
//
// It's a container for the expected responses of an operation. The container maps an HTTP response code to the
// expected response.
//
// The specification is not necessarily expected to cover all possible HTTP response codes because they may not be
// known in advance. However, documentation is expected to cover a successful operation response and any known errors.
//
// The default MAY be used as a default response object for all HTTP codes that are not covered individually by
// the Responses Object.
//
// The Responses Object MUST contain at least one response code, and if only one response code is provided it SHOULD
// be the response for a successful operation call.
//   - https://spec.openapis.org/oas/v3.1.0#responses-object
//
// This structure is identical to the v2 version, however they use different response types, hence
// the duplication. Perhaps in the future we could use generics here, but for now to keep things
// simple, they are broken out into individual versions.
type Responses struct {
	Codes      *orderedmap.Map[low.KeyReference[string], low.ValueReference[*Response]]
	Default    low.NodeReference[*Response]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Responses object.
func (r *Responses) GetIndex() *index.SpecIndex {
	return r.index
}

// GetContext returns the context.Context instance used when building the Responses object.
func (r *Responses) GetContext() context.Context {
	return r.context
}

// GetRootNode returns the root yaml node of the Responses object.
func (r *Responses) GetRootNode() *yaml.Node {
	return r.RootNode
}

// GetKeyNode returns the key yaml node of the Responses object.
func (r *Responses) GetKeyNode() *yaml.Node {
	return r.KeyNode
}

// GetExtensions returns all Responses extensions and satisfies the low.HasExtensions interface.
func (r *Responses) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return r.Extensions
}

// Build will extract default response and all Response objects for each code
func (r *Responses) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	r.KeyNode = keyNode
	root = utils.NodeAlias(root)
	r.RootNode = root
	r.Reference = new(low.Reference)
	r.Nodes = low.ExtractNodes(ctx, root)
	r.Extensions = low.ExtractExtensions(root)
	r.index = idx
	r.context = ctx

	low.ExtractExtensionNodes(ctx, r.Extensions, r.Nodes)
	utils.CheckForMergeNodes(root)
	if utils.IsNodeMap(root) {
		codes, err := low.ExtractMapNoLookup[*Response](ctx, root, idx)
		if err != nil {
			return err
		}
		if codes != nil {
			r.Codes = codes
			for code := range codes.KeysFromOldest() {
				r.Nodes.Store(code.KeyNode.Line, code.KeyNode)
			}
		}

		def := r.getDefault()
		if def != nil {
			// default is bundled into codes, pull it out
			r.Default = *def
			r.Nodes.Store(def.KeyNode.Line, def.KeyNode)
			// remove default from codes
			r.deleteCode(DefaultLabel)
		}
	} else {
		return fmt.Errorf("responses build failed: vn node is not a map! line %d, col %d",
			root.Line, root.Column)
	}
	return nil
}

func (r *Responses) getDefault() *low.NodeReference[*Response] {
	for code, resp := range r.Codes.FromOldest() {
		if strings.ToLower(code.Value) == DefaultLabel {
			return &low.NodeReference[*Response]{
				ValueNode: resp.ValueNode,
				KeyNode:   code.KeyNode,
				Value:     resp.Value,
			}
		}
	}
	return nil
}

// used to remove default from codes extracted by Build()
func (r *Responses) deleteCode(code string) {
	var key *low.KeyReference[string]
	for pair := orderedmap.First(r.Codes); pair != nil; pair = pair.Next() {
		if pair.Key().Value == code {
			key = pair.KeyPtr()
			break
		}
	}
	// should never be nil, but, you never know... science and all that!
	if key != nil {
		r.Codes.Delete(*key)
	}
}

// FindResponseByCode will attempt to locate a Response using an HTTP response code.
func (r *Responses) FindResponseByCode(code string) *low.ValueReference[*Response] {
	return low.FindItemInOrderedMap[*Response](code, r.Codes)
}

// Hash will return a consistent Hash of the Responses object
func (r *Responses) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for _, hash := range low.AppendMapHashes(nil, r.Codes) {
			h.WriteString(hash)
			h.WriteByte(low.HASH_PIPE)
		}
		if !r.Default.IsEmpty() {
			h.WriteString(low.GenerateHashString(r.Default.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(r.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
