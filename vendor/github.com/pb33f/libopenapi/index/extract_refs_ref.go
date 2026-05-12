// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (index *SpecIndex) extractReferenceAt(
	node, parent *yaml.Node,
	keyIndex int,
	seenPath []string,
	scope *SchemaIdScope,
	poly bool,
	pName string,
) *Reference {
	keyNode := node.Content[keyIndex]
	if len(node.Content) <= keyIndex+1 {
		return nil
	}
	if underOpenAPIExamplePayloadPath(seenPath) {
		return nil
	}

	isExtensionPath := false
	for _, spi := range seenPath {
		if strings.HasPrefix(spi, "x-") {
			isExtensionPath = true
			break
		}
	}

	if index.config.ExcludeExtensionRefs && isExtensionPath {
		return nil
	}

	if len(node.Content) > keyIndex+1 {
		if !utils.IsNodeStringValue(node.Content[keyIndex+1]) {
			return nil
		}
		if utils.IsNodeArray(node) {
			return nil
		}
	}

	index.linesWithRefs[keyNode.Line] = true

	value := node.Content[keyIndex+1].Value
	schemaIdBase := ""
	if scope != nil && len(scope.Chain) > 0 {
		schemaIdBase = scope.BaseUri
	}

	lastSlash := strings.LastIndexByte(value, '/')
	name := value
	if lastSlash >= 0 {
		name = value[lastSlash+1:]
	}

	fullDefinitionPath, componentName := index.resolveReferenceTarget(value)
	_, path := utils.ConvertComponentIdIntoFriendlyPathSearch(componentName)

	siblingProps, siblingKeys := extractSiblingRefProperties(node)
	ref := &Reference{
		ParentNode:           parent,
		FullDefinition:       fullDefinitionPath,
		Definition:           componentName,
		RawRef:               value,
		SchemaIdBase:         schemaIdBase,
		Name:                 name,
		Node:                 node,
		KeyNode:              node.Content[keyIndex+1],
		Path:                 path,
		Index:                index,
		IsExtensionRef:       isExtensionPath,
		HasSiblingProperties: len(siblingProps) > 0,
		SiblingProperties:    siblingProps,
		SiblingKeys:          siblingKeys,
	}

	index.rawSequencedRefs = append(index.rawSequencedRefs, ref)
	index.recordReferenceByLine(value, keyNode.Line)

	if len(node.Content) > 2 {
		index.storeReferenceWithSiblings(node, parent, keyIndex, ref, isExtensionPath, path, value)
	}

	if poly {
		index.polymorphicRefs[value] = ref
		switch pName {
		case "anyOf":
			index.polymorphicAnyOfRefs = append(index.polymorphicAnyOfRefs, ref)
		case "allOf":
			index.polymorphicAllOfRefs = append(index.polymorphicAllOfRefs, ref)
		case "oneOf":
			index.polymorphicOneOfRefs = append(index.polymorphicOneOfRefs, ref)
		}
		return nil
	}

	if index.allRefs[value] != nil {
		return nil
	}

	if value == "" {
		fp := make([]string, len(seenPath))
		copy(fp, seenPath)

		completedPath := fmt.Sprintf("$.%s", strings.Join(fp, "."))
		c := keyNode
		if len(node.Content) > keyIndex+1 {
			c = node.Content[keyIndex+1]
		}

		index.refErrors = append(index.refErrors, &IndexingError{
			Err:     errors.New("schema reference is empty and cannot be processed"),
			Node:    c,
			KeyNode: keyNode,
			Path:    completedPath,
		})
		return nil
	}

	index.allRefs[fullDefinitionPath] = ref
	return ref
}

func (index *SpecIndex) registerSchemaIDAt(node *yaml.Node, keyIndex int, seenPath []string, parentBaseUri string) {
	if underOpenAPIExamplePath(seenPath) {
		return
	}
	if len(node.Content) <= keyIndex+1 || !utils.IsNodeStringValue(node.Content[keyIndex+1]) {
		return
	}

	idValue := node.Content[keyIndex+1].Value
	idNode := node.Content[keyIndex+1]

	definitionPath := "#"
	if len(seenPath) > 0 {
		definitionPath = "#/" + strings.Join(seenPath, "/")
	}

	if err := ValidateSchemaId(idValue); err != nil {
		index.errorLock.Lock()
		index.refErrors = append(index.refErrors, &IndexingError{
			Err:     fmt.Errorf("invalid $id value '%s': %w", idValue, err),
			Node:    idNode,
			KeyNode: node.Content[keyIndex],
			Path:    definitionPath,
		})
		index.errorLock.Unlock()
		return
	}

	baseUri := parentBaseUri
	if baseUri == "" {
		baseUri = index.specAbsolutePath
	}
	resolvedUri, resolveErr := ResolveSchemaId(idValue, baseUri)
	if resolveErr != nil {
		if index.logger != nil {
			index.logger.Warn("failed to resolve $id",
				"id", idValue,
				"base", baseUri,
				"definitionPath", definitionPath,
				"error", resolveErr.Error(),
				"line", idNode.Line)
		}
		resolvedUri = idValue
	}

	parentId := ""
	if parentBaseUri != index.specAbsolutePath && parentBaseUri != "" {
		parentId = parentBaseUri
	}
	entry := &SchemaIdEntry{
		Id:             idValue,
		ResolvedUri:    resolvedUri,
		SchemaNode:     node,
		ParentId:       parentId,
		Index:          index,
		DefinitionPath: definitionPath,
		Line:           idNode.Line,
		Column:         idNode.Column,
	}

	_ = index.RegisterSchemaId(entry)
}

func (index *SpecIndex) resolveReferenceTarget(value string) (string, string) {
	uri := strings.Split(value, "#/")

	var defRoot string
	if strings.HasPrefix(index.specAbsolutePath, "http") {
		defRoot = index.specAbsolutePath
	} else {
		defRoot = filepath.Dir(index.specAbsolutePath)
	}

	var componentName string
	var fullDefinitionPath string
	if len(uri) == 2 {
		// Reference contains a fragment (e.g. "file.yaml#/components/schemas/Foo" or "#/definitions/Bar").
		if uri[0] == "" {
			// Fragment-only local ref — prefix with the spec's absolute path.
			fullDefinitionPath = fmt.Sprintf("%s#/%s", index.specAbsolutePath, uri[1])
			componentName = value
		} else {
			if strings.HasPrefix(uri[0], "http") {
				// Absolute HTTP URL with fragment — use as-is.
				fullDefinitionPath = value
				componentName = fmt.Sprintf("#/%s", uri[1])
			} else if filepath.IsAbs(uri[0]) {
				// Absolute local file path with fragment — use as-is.
				fullDefinitionPath = value
				componentName = fmt.Sprintf("#/%s", uri[1])
			} else if index.config.BaseURL != nil && !filepath.IsAbs(defRoot) {
				// Relative path with a configured BaseURL — resolve against the base URL.
				var u url.URL
				if strings.HasPrefix(defRoot, "http") {
					up, _ := url.Parse(defRoot)
					up.Path = utils.ReplaceWindowsDriveWithLinuxPath(filepath.Dir(up.Path))
					u = *up
				} else {
					u = *index.config.BaseURL
				}
				abs := utils.CheckPathOverlap(u.Path, uri[0], string(os.PathSeparator))
				u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
				fullDefinitionPath = fmt.Sprintf("%s#/%s", u.String(), uri[1])
				componentName = fmt.Sprintf("#/%s", uri[1])
			} else {
				// Relative local file path — resolve against the spec's directory.
				abs := index.resolveRelativeFilePath(defRoot, uri[0])
				fullDefinitionPath = fmt.Sprintf("%s#/%s", abs, uri[1])
				componentName = fmt.Sprintf("#/%s", uri[1])
			}
		}
	} else if strings.HasPrefix(uri[0], "http") {
		// No fragment, absolute HTTP URL — use as-is.
		fullDefinitionPath = value
	} else if !strings.Contains(uri[0], "#") {
		// No fragment, not a bare anchor — whole-file reference or relative path.
		if strings.HasPrefix(defRoot, "http") {
			// Spec root is remote — resolve the relative path against the remote URL.
			if !filepath.IsAbs(uri[0]) {
				u, _ := url.Parse(defRoot)
				pathDir := filepath.Dir(u.Path)
				pathAbs, _ := filepath.Abs(utils.CheckPathOverlap(pathDir, uri[0], string(os.PathSeparator)))
				pathAbs = utils.ReplaceWindowsDriveWithLinuxPath(pathAbs)
				u.Path = pathAbs
				fullDefinitionPath = u.String()
			}
		} else if !filepath.IsAbs(uri[0]) {
			// Relative local file path — resolve against BaseURL if configured, else the spec's directory.
			if index.config.BaseURL != nil {
				u := *index.config.BaseURL
				abs := utils.CheckPathOverlap(u.Path, uri[0], string(os.PathSeparator))
				abs = utils.ReplaceWindowsDriveWithLinuxPath(abs)
				u.Path = abs
				fullDefinitionPath = u.String()
				componentName = uri[0]
			} else {
				abs := index.resolveRelativeFilePath(defRoot, uri[0])
				fullDefinitionPath = abs
				componentName = uri[0]
			}
		}
	}

	if fullDefinitionPath == "" && value != "" {
		fullDefinitionPath = value
	}
	if componentName == "" {
		componentName = value
	}

	return fullDefinitionPath, componentName
}

func (index *SpecIndex) recordReferenceByLine(value string, line int) {
	refNameIndex := strings.LastIndex(value, "/")
	refName := value[refNameIndex+1:]
	if len(index.refsByLine[refName]) > 0 {
		index.refsByLine[refName][line] = true
		return
	}
	v := make(map[int]bool)
	v[line] = true
	index.refsByLine[refName] = v
}

func extractSiblingRefProperties(node *yaml.Node) (map[string]*yaml.Node, []*yaml.Node) {
	siblingProps := make(map[string]*yaml.Node)
	var siblingKeys []*yaml.Node
	if len(node.Content) <= 2 {
		return siblingProps, siblingKeys
	}
	for j := 0; j < len(node.Content); j += 2 {
		if j+1 < len(node.Content) && node.Content[j].Value != "$ref" {
			siblingProps[node.Content[j].Value] = node.Content[j+1]
			siblingKeys = append(siblingKeys, node.Content[j])
		}
	}
	return siblingProps, siblingKeys
}

func (index *SpecIndex) storeReferenceWithSiblings(
	node, parent *yaml.Node,
	keyIndex int,
	ref *Reference,
	isExtensionPath bool,
	path, value string,
) {
	copiedNode := *node
	siblingProps, siblingKeys := extractSiblingRefProperties(node)

	copied := Reference{
		ParentNode:           parent,
		FullDefinition:       ref.FullDefinition,
		Definition:           ref.Definition,
		RawRef:               ref.RawRef,
		SchemaIdBase:         ref.SchemaIdBase,
		Name:                 ref.Name,
		Node:                 &copiedNode,
		KeyNode:              node.Content[keyIndex],
		Path:                 path,
		Index:                index,
		IsExtensionRef:       isExtensionPath,
		HasSiblingProperties: len(siblingProps) > 0,
		SiblingProperties:    siblingProps,
		SiblingKeys:          siblingKeys,
	}

	index.refsWithSiblings[value] = copied
}
