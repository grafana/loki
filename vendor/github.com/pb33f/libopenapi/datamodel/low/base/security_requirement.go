// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"hash/maphash"
	"sort"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// SecurityRequirement is a low-level representation of a Swagger / OpenAPI 3 SecurityRequirement object.
//
// SecurityRequirement lists the required security schemes to execute this operation. The object can have multiple
// security schemes declared in it which are all required (that is, there is a logical AND between the schemes).
//
// The name used for each property MUST correspond to a security scheme declared in the Security Definitions
//   - https://swagger.io/specification/v2/#securityDefinitionsObject
//   - https://swagger.io/specification/#security-requirement-object
type SecurityRequirement struct {
	Requirements             low.ValueReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]low.ValueReference[string]]]]
	KeyNode                  *yaml.Node
	RootNode                 *yaml.Node
	ContainsEmptyRequirement bool // if a requirement is empty (this means it's optional)
	index                    *index.SpecIndex
	context                  context.Context
	*low.Reference
	low.NodeMap
}

// GetContext will return the context.Context instance used when building the SecurityRequirement object
func (s *SecurityRequirement) GetContext() context.Context {
	return s.context
}

// GetIndex will return the index.SpecIndex instance attached to the SecurityRequirement object
func (s *SecurityRequirement) GetIndex() *index.SpecIndex {
	return s.index
}

// Build will extract security requirements from the node (the structure is odd, to be honest)
func (s *SecurityRequirement) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	s.KeyNode = keyNode
	root = utils.NodeAlias(root)
	s.RootNode = root
	utils.CheckForMergeNodes(root)
	s.Reference = new(low.Reference)
	s.Nodes = low.ExtractNodes(ctx, root)
	s.context = ctx
	s.index = idx

	var labelNode *yaml.Node
	valueMap := orderedmap.New[low.KeyReference[string], low.ValueReference[[]low.ValueReference[string]]]()
	var arr []low.ValueReference[string]
	for i := range root.Content {
		if i%2 == 0 {
			labelNode = root.Content[i]
			arr = []low.ValueReference[string]{} // reset roles.
			continue
		}
		for j := range root.Content[i].Content {
			if root.Content[i].Content[j].Value == "" {
				s.ContainsEmptyRequirement = true
			}
			arr = append(arr, low.ValueReference[string]{
				Value:     root.Content[i].Content[j].Value,
				ValueNode: root.Content[i].Content[j],
			})
			s.Nodes.Store(root.Content[i].Content[j].Line, root.Content[i].Content[j])
		}
		valueMap.Set(
			low.KeyReference[string]{
				Value:   labelNode.Value,
				KeyNode: labelNode,
			},
			low.ValueReference[[]low.ValueReference[string]]{
				Value:     arr,
				ValueNode: root.Content[i],
			},
		)
	}
	if len(root.Content) == 0 {
		s.ContainsEmptyRequirement = true
	}
	s.Requirements = low.ValueReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]low.ValueReference[string]]]]{
		Value:     valueMap,
		ValueNode: root,
	}

	return nil
}

// GetRootNode will return the root yaml node of the SecurityRequirement object
func (s *SecurityRequirement) GetRootNode() *yaml.Node {
	return s.RootNode
}

// GetKeyNode will return the key yaml node of the SecurityRequirement object
func (s *SecurityRequirement) GetKeyNode() *yaml.Node {
	return s.KeyNode
}

// FindRequirement will attempt to locate a security requirement string from a supplied name.
func (s *SecurityRequirement) FindRequirement(name string) []low.ValueReference[string] {
	for k, v := range s.Requirements.Value.FromOldest() {
		if k.Value == name {
			return v.Value
		}
	}
	return nil
}

// GetKeys returns a string slice of all the keys used in the requirement.
func (s *SecurityRequirement) GetKeys() []string {
	keys := make([]string, orderedmap.Len(s.Requirements.Value))
	z := 0
	for k := range s.Requirements.Value.KeysFromOldest() {
		keys[z] = k.Value
		z++
	}
	return keys
}

// Hash will return a consistent hash of the SecurityRequirement object
func (s *SecurityRequirement) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for k, v := range orderedmap.SortAlpha(s.Requirements.Value).FromOldest() {
			// Pre-allocate vals slice
			vals := make([]string, len(v.Value))
			for y := range v.Value {
				vals[y] = v.Value[y].Value
			}
			sort.Strings(vals)

			h.WriteString(k.Value)
			h.WriteByte('-')
			for i, val := range vals {
				if i > 0 {
					h.WriteByte(low.HASH_PIPE)
				}
				h.WriteString(val)
			}
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
