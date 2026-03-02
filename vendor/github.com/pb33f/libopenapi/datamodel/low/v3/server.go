// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Server represents a low-level OpenAPI 3+ Server object.
//   - https://spec.openapis.org/oas/v3.1.0#server-object
type Server struct {
	Name        low.NodeReference[string] // OpenAPI 3.2+ name field for documentation
	URL         low.NodeReference[string]
	Description low.NodeReference[string]
	Variables   low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*ServerVariable]]]
	Extensions  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode     *yaml.Node
	RootNode    *yaml.Node
	index       *index.SpecIndex
	context     context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Server object.
func (s *Server) GetIndex() *index.SpecIndex {
	return s.index
}

// GetContext returns the context.Context instance used when building the Server object.
func (s *Server) GetContext() context.Context {
	return s.context
}

// GetRootNode returns the root yaml node of the Server object.
func (s *Server) GetRootNode() *yaml.Node {
	return s.RootNode
}

// GetExtensions returns all Paths extensions and satisfies the low.HasExtensions interface.
func (s *Server) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return s.Extensions
}

// FindVariable attempts to locate a ServerVariable instance using the supplied key.
func (s *Server) FindVariable(serverVar string) *low.ValueReference[*ServerVariable] {
	return low.FindItemInOrderedMap[*ServerVariable](serverVar, s.Variables.Value)
}

// Build will extract server variables from the supplied node.
func (s *Server) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	s.KeyNode = keyNode
	root = utils.NodeAlias(root)
	s.RootNode = root
	utils.CheckForMergeNodes(root)
	s.Reference = new(low.Reference)
	s.Nodes = low.ExtractNodes(ctx, root)
	s.Extensions = low.ExtractExtensions(root)
	s.context = ctx
	s.index = idx

	low.ExtractExtensionNodes(ctx, s.Extensions, s.Nodes)

	kn, vars := utils.FindKeyNode(VariablesLabel, root.Content)
	if vars == nil {
		return nil
	}
	variablesMap := orderedmap.New[low.KeyReference[string], low.ValueReference[*ServerVariable]]()
	if utils.IsNodeMap(vars) {
		var currentNode string
		var localKeyNode *yaml.Node
		for i, varNode := range vars.Content {
			if i%2 == 0 {
				currentNode = varNode.Value
				localKeyNode = varNode
				continue
			}
			variable := ServerVariable{}
			variable.Reference = new(low.Reference)
			_ = low.BuildModel(varNode, &variable)
			variable.Nodes = low.ExtractNodesRecursive(ctx, varNode)
			variable.Extensions = low.ExtractExtensions(varNode)
			if localKeyNode != nil {
				variable.Nodes.Store(localKeyNode.Line, localKeyNode)
			}
			variable.RootNode = varNode
			variable.KeyNode = localKeyNode
			variablesMap.Set(
				low.KeyReference[string]{
					Value:   currentNode,
					KeyNode: localKeyNode,
				},
				low.ValueReference[*ServerVariable]{
					ValueNode: varNode,
					Value:     &variable,
				},
			)
		}
		s.Variables = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*ServerVariable]]]{
			KeyNode:   kn,
			ValueNode: vars,
			Value:     variablesMap,
		}
	}
	return nil
}

// Hash will return a consistent Hash of the Server object
func (s *Server) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !s.Name.IsEmpty() {
			h.WriteString(s.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if s.Variables.Value != nil {
			for v := range orderedmap.SortAlpha(s.Variables.Value).ValuesFromOldest() {
				h.WriteString(low.GenerateHashString(v.Value))
				h.WriteByte(low.HASH_PIPE)
			}
		}
		if !s.URL.IsEmpty() {
			h.WriteString(s.URL.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !s.Description.IsEmpty() {
			h.WriteString(s.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(s.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
