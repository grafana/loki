// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

type extractRefsState struct {
	ctx           context.Context
	scope         *SchemaIdScope
	parentBaseURI string
	seenPath      []string
	lastAppended  bool
	level         int
	poly          bool
	polyName      string
	prev          string
}

func (index *SpecIndex) initializeExtractRefsState(
	ctx context.Context,
	node *yaml.Node,
	seenPath []string,
	level int,
	poly bool,
	pName string,
) extractRefsState {
	scope := GetSchemaIdScope(ctx)
	if scope == nil {
		scope = NewSchemaIdScope(index.specAbsolutePath)
		ctx = WithSchemaIdScope(ctx, scope)
	}

	parentBaseURI := scope.BaseUri
	if node != nil && node.Kind == yaml.MappingNode && !underOpenAPIExamplePath(seenPath) {
		if nodeID := FindSchemaIdInNode(node); nodeID != "" {
			resolvedNodeID, _ := ResolveSchemaId(nodeID, parentBaseURI)
			if resolvedNodeID == "" {
				resolvedNodeID = nodeID
			}
			scope = scope.Copy()
			scope.PushId(resolvedNodeID)
			ctx = WithSchemaIdScope(ctx, scope)
		}
	}

	return extractRefsState{
		ctx:           ctx,
		scope:         scope,
		parentBaseURI: parentBaseURI,
		seenPath:      seenPath,
		level:         level,
		poly:          poly,
		polyName:      pName,
	}
}

func (index *SpecIndex) walkExtractRefs(node, parent *yaml.Node, state *extractRefsState) []*Reference {
	if node == nil || len(node.Content) == 0 {
		return nil
	}

	var found []*Reference
	for i, n := range node.Content {
		// In YAML mapping nodes, Content alternates key-value: even indices (0, 2, 4...)
		// are keys, odd indices (1, 3, 5...) are values.
		if i%2 == 0 {
			if stop := index.handleExtractRefsKey(node, parent, state, i, &found); stop {
				continue
			}
		}

		if utils.IsNodeMap(n) || utils.IsNodeArray(n) {
			found = append(found, index.walkChildExtractRefs(n, node, state)...)
		}

		index.unwindExtractRefsPath(node, state, i)
	}

	return found
}

func (index *SpecIndex) walkChildExtractRefs(node, parent *yaml.Node, state *extractRefsState) []*Reference {
	if underOpenAPIExamplePayloadPath(state.seenPath) {
		return nil
	}
	if isDirectOpenAPIExampleValuePath(state.seenPath) && !isDirectOpenAPIExampleRefNode(node) {
		return nil
	}
	state.level++
	if isPoly, _ := index.checkPolymorphicNode(state.prev); isPoly {
		state.poly = true
		if state.prev != "" {
			state.polyName = state.prev
		}
	}
	return index.ExtractRefs(state.ctx, node, parent, state.seenPath, state.level, state.poly, state.polyName)
}

func isDirectOpenAPIExampleRefNode(node *yaml.Node) bool {
	return utils.IsNodeMap(node) && utils.GetRefValueNode(node) != nil && len(node.Content) == 2
}

func (index *SpecIndex) handleExtractRefsKey(
	node, parent *yaml.Node,
	state *extractRefsState,
	keyIndex int,
	found *[]*Reference,
) bool {
	keyNode := node.Content[keyIndex]
	state.lastAppended = false
	if keyNode == nil {
		return false
	}

	if isSchemaContainingNode(keyNode.Value) && !utils.IsNodeArray(node) && keyIndex+1 < len(node.Content) {
		index.collectInlineSchemaDefinition(parent, node, state.seenPath, keyIndex)
	}

	if isMapOfSchemaContainingNode(keyNode.Value) && !utils.IsNodeArray(node) && keyIndex+1 < len(node.Content) {
		if shouldSkipMapSchemaCollection(state.seenPath) {
			return true
		}
		index.collectMapSchemaDefinitions(parent, node, state.seenPath, keyIndex)
	}

	if isArrayOfSchemaContainingNode(keyNode.Value) && !utils.IsNodeArray(node) && keyIndex+1 < len(node.Content) {
		index.collectArraySchemaDefinitions(parent, node, state.seenPath, keyIndex)
	}

	if keyNode.Value == "$ref" {
		if ref := index.extractReferenceAt(node, parent, keyIndex, state.seenPath, state.scope, state.poly, state.polyName); ref != nil {
			*found = append(*found, ref)
		}
	}

	if keyNode.Value == "$id" {
		index.registerSchemaIDAt(node, keyIndex, state.seenPath, state.parentBaseURI)
	}

	if keyNode.Value != "$ref" && keyNode.Value != "$id" && keyNode.Value != "" {
		action := index.extractNodeMetadata(node, parent, state.seenPath, keyIndex)
		state.lastAppended = action.appendSegment
		if action.appendSegment {
			state.seenPath = append(state.seenPath, strings.ReplaceAll(keyNode.Value, "/", "~1"))
			state.prev = keyNode.Value
		}
		return action.stop
	}

	return false
}

func shouldSkipMapSchemaCollection(seenPath []string) bool {
	if len(seenPath) == 0 {
		return false
	}
	for _, p := range seenPath {
		if strings.HasPrefix(p, "x-") {
			return true
		}
	}
	return underOpenAPIExamplePath(seenPath)
}

func (index *SpecIndex) unwindExtractRefsPath(node *yaml.Node, state *extractRefsState, currentIndex int) {
	if currentIndex >= len(node.Content)-1 {
		return
	}
	next := node.Content[currentIndex+1]
	if currentIndex%2 != 0 && state.lastAppended &&
		next != nil && !utils.IsNodeArray(next) && !utils.IsNodeMap(next) && len(state.seenPath) > 0 {
		state.seenPath = state.seenPath[:len(state.seenPath)-1]
		state.lastAppended = false
	}
}
