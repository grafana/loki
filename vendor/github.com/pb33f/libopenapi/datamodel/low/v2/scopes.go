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

// Scopes is a low-level representation of a Swagger / OpenAPI 2 OAuth2 Scopes object.
//
// Scopes lists the available scopes for an OAuth2 security scheme.
//   - https://swagger.io/specification/v2/#scopesObject
type Scopes struct {
	Values     *orderedmap.Map[low.KeyReference[string], low.ValueReference[string]]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// GetExtensions returns all Scopes extensions and satisfies the low.HasExtensions interface.
func (s *Scopes) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return s.Extensions
}

// FindScope will attempt to locate a scope string using a key.
func (s *Scopes) FindScope(scope string) *low.ValueReference[string] {
	return low.FindItemInOrderedMap[string](scope, s.Values)
}

// Build will extract scope values and extensions from node.
func (s *Scopes) Build(_ context.Context, _, root *yaml.Node, _ *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	s.Extensions = low.ExtractExtensions(root)
	valueMap := orderedmap.New[low.KeyReference[string], low.ValueReference[string]]()
	if utils.IsNodeMap(root) {
		for k := range root.Content {
			if k%2 == 0 {
				if strings.Contains(root.Content[k].Value, "x-") {
					continue
				}
				valueMap.Set(
					low.KeyReference[string]{
						Value:   root.Content[k].Value,
						KeyNode: root.Content[k],
					},
					low.ValueReference[string]{
						Value:     root.Content[k+1].Value,
						ValueNode: root.Content[k+1],
					},
				)
			}
		}
		s.Values = valueMap
	}
	return nil
}

// Hash will return a consistent Hash of the Scopes object
func (s *Scopes) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for k, v := range orderedmap.SortAlpha(s.Values).FromOldest() {
			h.WriteString(fmt.Sprintf("%s-%s", k.Value, v.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(s.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
