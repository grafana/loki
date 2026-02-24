// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

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

// Responses is a low-level representation of a Swagger / OpenAPI 2 Responses object.
type Responses struct {
	Codes      *orderedmap.Map[low.KeyReference[string], low.ValueReference[*Response]]
	Default    low.NodeReference[*Response]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// GetExtensions returns all Responses extensions and satisfies the low.HasExtensions interface.
func (r *Responses) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return r.Extensions
}

// Build will extract default value and extensions from node.
func (r *Responses) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	r.Extensions = low.ExtractExtensions(root)

	if utils.IsNodeMap(root) {
		codes, err := low.ExtractMapNoLookup[*Response](ctx, root, idx)
		if err != nil {
			return err
		}
		if codes != nil {
			r.Codes = codes
		}
		def := r.getDefault()
		if def != nil {
			// default is bundled into codes, pull it out
			r.Default = *def
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
	if r.Codes != nil {
		for pair := orderedmap.First(r.Codes); pair != nil; pair = pair.Next() {
			if pair.Key().Value == code {
				key = pair.KeyPtr()
				break
			}
		}
	}
	// should never be nil, but, you never know... science and all that!
	if key != nil {
		r.Codes.Delete(*key)
	}
}

// FindResponseByCode will attempt to locate a Response instance using an HTTP response code string.
func (r *Responses) FindResponseByCode(code string) *low.ValueReference[*Response] {
	return low.FindItemInOrderedMap[*Response](code, r.Codes)
}

// Hash will return a consistent Hash of the Responses object
func (r *Responses) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for _, hash := range low.AppendMapHashes(nil, orderedmap.SortAlpha(r.Codes)) {
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
