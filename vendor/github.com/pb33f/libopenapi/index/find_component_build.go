// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"fmt"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func cloneFoundComponentReference(index *SpecIndex, found *Reference, componentID, absoluteFilePath string) *Reference {
	return buildResolvedComponentReference(index, found, componentID, absoluteFilePath, found.Name, found.Path, found.Node)
}

// buildResolvedComponentReference constructs a fully resolved Reference for a component.
// source is the original unresolved ref (may be nil for fresh lookups); componentID is the
// JSON Pointer fragment (e.g. "#/components/schemas/Pet"); absoluteFilePath is the file
// or URL where the component lives.
func buildResolvedComponentReference(
	index *SpecIndex,
	source *Reference,
	componentID, absoluteFilePath, name, path string,
	node *yaml.Node,
) *Reference {
	fullDef := fmt.Sprintf("%s%s", absoluteFilePath, componentID)
	if path == "" {
		_, path = utils.ConvertComponentIdIntoFriendlyPathSearch(componentID)
		if path == "$." {
			path = "$"
		}
	}

	var ref Reference
	if source != nil {
		ref = Reference{
			RawRef:               source.RawRef,
			SchemaIdBase:         source.SchemaIdBase,
			KeyNode:              source.KeyNode,
			ParentNodeSchemaType: source.ParentNodeSchemaType,
			Resolved:             source.Resolved,
			Circular:             source.Circular,
			Seen:                 source.Seen,
			IsRemote:             source.IsRemote,
			IsExtensionRef:       source.IsExtensionRef,
			HasSiblingProperties: source.HasSiblingProperties,
			In:                   source.In,
		}
		ref.ParentNodeTypes = append([]string(nil), source.ParentNodeTypes...)
		ref.SiblingKeys = append([]*yaml.Node(nil), source.SiblingKeys...)
		ref.SiblingProperties = cloneSiblingProperties(source.SiblingProperties)
		ref.RequiredRefProperties = cloneRequiredRefProperties(source.RequiredRefProperties)
	}

	ref.FullDefinition = fullDef
	ref.Definition = componentID
	ref.Name = name
	ref.Node = node
	ref.Path = path
	ref.RemoteLocation = absoluteFilePath
	ref.Index = index

	if source != nil && source.ParentNode != nil {
		ref.ParentNode = source.ParentNode
	} else {
		ref.ParentNode = lookupComponentParentNode(index, componentID, fullDef)
	}

	if ref.RequiredRefProperties == nil && node != nil {
		ref.RequiredRefProperties = extractDefinitionRequiredRefProperties(node, map[string][]string{}, fullDef, index)
	}

	return &ref
}

func lookupComponentParentNode(index *SpecIndex, componentID, fullDef string) *yaml.Node {
	if index == nil {
		return nil
	}
	if ref := index.allRefs[componentID]; ref != nil {
		return ref.ParentNode
	}
	if ref := index.allRefs[fullDef]; ref != nil {
		return ref.ParentNode
	}
	return nil
}

func cloneRequiredRefProperties(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string][]string, len(source))
	for key, values := range source {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func cloneSiblingProperties(source map[string]*yaml.Node) map[string]*yaml.Node {
	if source == nil {
		return nil
	}
	cloned := make(map[string]*yaml.Node, len(source))
	for key, node := range source {
		cloned[key] = node
	}
	return cloned
}
