// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
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
	root = utils.ResolveMergeNodes(root)
	s.RootNode = root
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
			effective := utils.ResolveMergeNodes(varNode)
			variable := ServerVariable{}
			variable.Reference = new(low.Reference)
			_ = low.BuildModel(effective, &variable)
			variable.Nodes = low.ExtractNodesRecursive(ctx, effective)
			variable.Extensions = low.ExtractExtensions(effective)
			if localKeyNode != nil {
				variable.Nodes.Store(localKeyNode.Line, localKeyNode)
			}
			variable.RootNode = effective
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
			low.HashString(h, "name", s.Name.Value)
		}
		low.HashMap(h, "variables", s.Variables.Value)
		if !s.URL.IsEmpty() {
			low.HashString(h, "url", s.URL.Value)
		}
		if !s.Description.IsEmpty() {
			low.HashString(h, "description", s.Description.Value)
		}
		for _, ext := range low.HashExtensions(s.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
