// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
	"golang.org/x/sync/singleflight"
)

// preserveLegacyRefOrder allows opt-out of deterministic ordering if issues arise.
// Set LIBOPENAPI_LEGACY_REF_ORDER=true to use the old non-deterministic ordering.
var preserveLegacyRefOrder = os.Getenv("LIBOPENAPI_LEGACY_REF_ORDER") == "true"

// indexedRef pairs a resolved reference with its original input position for deterministic ordering.
type indexedRef struct {
	ref *Reference
	pos int
}

// ExtractRefs will return a deduplicated slice of references for every unique ref found in the document.
// The total number of refs, will generally be much higher, you can extract those from GetRawReferenceCount()
func (index *SpecIndex) ExtractRefs(ctx context.Context, node, parent *yaml.Node, seenPath []string, level int, poly bool, pName string) []*Reference {
	if node == nil {
		return nil
	}

	// Initialize $id scope if not present (uses document base as initial scope)
	scope := GetSchemaIdScope(ctx)
	if scope == nil {
		scope = NewSchemaIdScope(index.specAbsolutePath)
		ctx = WithSchemaIdScope(ctx, scope)
	}

	// Capture the parent's base URI BEFORE any $id in this node is processed
	// This is used for registering any $id found in this node
	parentBaseUri := scope.BaseUri

	// Check if THIS node has a $id and update scope for processing children
	// This must happen before iterating children so they see the updated scope
	if node.Kind == yaml.MappingNode {
		if nodeId := FindSchemaIdInNode(node); nodeId != "" {
			resolvedNodeId, _ := ResolveSchemaId(nodeId, parentBaseUri)
			if resolvedNodeId == "" {
				resolvedNodeId = nodeId
			}
			// Update scope for children of this node
			scope = scope.Copy()
			scope.PushId(resolvedNodeId)
			ctx = WithSchemaIdScope(ctx, scope)
		}
	}

	var found []*Reference
	if len(node.Content) > 0 {
		var prev, polyName string
		for i, n := range node.Content {
			if utils.IsNodeMap(n) || utils.IsNodeArray(n) {
				level++
				// check if we're using  polymorphic values. These tend to create rabbit warrens of circular
				// references if every single link is followed. We don't resolve polymorphic values.
				isPoly, _ := index.checkPolymorphicNode(prev)
				polyName = pName
				if isPoly {
					poly = true
					if prev != "" {
						polyName = prev
					}
				}

				found = append(found, index.ExtractRefs(ctx, n, node, seenPath, level, poly, polyName)...)
			}

			// check if we're dealing with an inline schema definition, that isn't part of an array
			// (which means it's being used as a value in an array, and it's not a label)
			// https://github.com/pb33f/libopenapi/issues/76
			schemaContainingNodes := []string{"schema", "items", "additionalProperties", "contains", "not", "unevaluatedItems", "unevaluatedProperties"}
			if i%2 == 0 && slices.Contains(schemaContainingNodes, n.Value) && !utils.IsNodeArray(node) && (i+1 < len(node.Content)) {

				var jsonPath, definitionPath, fullDefinitionPath string

				if len(seenPath) > 0 || n.Value != "" {
					loc := append(seenPath, n.Value)
					// create definition and full definition paths
					locPath := strings.Join(loc, "/")
					definitionPath = "#/" + locPath
					fullDefinitionPath = index.specAbsolutePath + "#/" + locPath
					_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
				}

				ref := &Reference{
					ParentNode:     parent,
					FullDefinition: fullDefinitionPath,
					Definition:     definitionPath,
					Node:           node.Content[i+1],
					KeyNode:        node.Content[i],
					Path:           jsonPath,
					Index:          index,
				}

				isRef, _, _ := utils.IsNodeRefValue(node.Content[i+1])
				if isRef {
					// record this reference
					index.allRefSchemaDefinitions = append(index.allRefSchemaDefinitions, ref)
					continue
				}

				if n.Value == "additionalProperties" || n.Value == "unevaluatedProperties" {
					if utils.IsNodeBoolValue(node.Content[i+1]) {
						continue
					}
				}

				index.allInlineSchemaDefinitions = append(index.allInlineSchemaDefinitions, ref)

				// check if the schema is an object or an array,
				// and if so, add it to the list of inline schema object definitions.
				k, v := utils.FindKeyNodeTop("type", node.Content[i+1].Content)
				if k != nil && v != nil {
					if v.Value == "object" || v.Value == "array" {
						index.allInlineSchemaObjectDefinitions = append(index.allInlineSchemaObjectDefinitions, ref)
					}
				}
			}

			// Perform the same check for all maps of schemas like properties and patternProperties
			// https://github.com/pb33f/libopenapi/issues/76
			mapOfSchemaContainingNodes := []string{"properties", "patternProperties"}
			if i%2 == 0 && slices.Contains(mapOfSchemaContainingNodes, n.Value) && !utils.IsNodeArray(node) && (i+1 < len(node.Content)) {

				// if 'examples' or 'example' exists in the seenPath, skip this 'properties' node.
				// https://github.com/pb33f/libopenapi/issues/160
				if len(seenPath) > 0 {
					skip := false

					// iterate through the path and look for an item named 'examples' or 'example'
					for _, p := range seenPath {
						if p == "examples" || p == "example" {
							skip = true
							break
						}
						// look for any extension in the path and ignore it
						if strings.HasPrefix(p, "x-") {
							skip = true
							break
						}
					}
					if skip {
						continue
					}
				}

				// for each property add it to our schema definitions
				label := ""
				for h, prop := range node.Content[i+1].Content {

					if h%2 == 0 {
						label = prop.Value
						continue
					}
					var jsonPath, definitionPath, fullDefinitionPath string
					if len(seenPath) > 0 || n.Value != "" && label != "" {
						loc := append(seenPath, n.Value, label)
						locPath := strings.Join(loc, "/")
						definitionPath = "#/" + locPath
						fullDefinitionPath = index.specAbsolutePath + "#/" + locPath
						_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
					}
					ref := &Reference{
						ParentNode:     parent,
						FullDefinition: fullDefinitionPath,
						Definition:     definitionPath,
						Node:           prop,
						KeyNode:        node.Content[i],
						Path:           jsonPath,
						Index:          index,
					}

					isRef, _, _ := utils.IsNodeRefValue(prop)
					if isRef {
						// record this reference
						index.allRefSchemaDefinitions = append(index.allRefSchemaDefinitions, ref)
						continue
					}

					index.allInlineSchemaDefinitions = append(index.allInlineSchemaDefinitions, ref)

					// check if the schema is an object or an array,
					// and if so, add it to the list of inline schema object definitions.
					k, v := utils.FindKeyNodeTop("type", prop.Content)
					if k != nil && v != nil {
						if v.Value == "object" || v.Value == "array" {
							index.allInlineSchemaObjectDefinitions = append(index.allInlineSchemaObjectDefinitions, ref)
						}
					}
				}
			}

			// Perform the same check for all arrays of schemas like allOf, anyOf, oneOf
			arrayOfSchemaContainingNodes := []string{"allOf", "anyOf", "oneOf", "prefixItems"}
			if i%2 == 0 && slices.Contains(arrayOfSchemaContainingNodes, n.Value) && !utils.IsNodeArray(node) && (i+1 < len(node.Content)) {
				// for each element in the array, add it to our schema definitions
				for h, element := range node.Content[i+1].Content {

					var jsonPath, definitionPath, fullDefinitionPath string
					if len(seenPath) > 0 {
						loc := append(seenPath, n.Value, strconv.Itoa(h))
						locPath := strings.Join(loc, "/")
						definitionPath = "#/" + locPath
						fullDefinitionPath = index.specAbsolutePath + "#/" + locPath
						_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
					} else {
						definitionPath = "#/" + n.Value
						fullDefinitionPath = index.specAbsolutePath + "#/" + n.Value
						_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
					}

					ref := &Reference{
						ParentNode:     parent,
						FullDefinition: fullDefinitionPath,
						Definition:     definitionPath,
						Node:           element,
						KeyNode:        node.Content[i],
						Path:           jsonPath,
						Index:          index,
					}

					isRef, _, _ := utils.IsNodeRefValue(element)
					if isRef { // record this reference
						index.allRefSchemaDefinitions = append(index.allRefSchemaDefinitions, ref)
						continue
					}
					index.allInlineSchemaDefinitions = append(index.allInlineSchemaDefinitions, ref)

					// check if the schema is an object or an array,
					// and if so, add it to the list of inline schema object definitions.
					k, v := utils.FindKeyNodeTop("type", element.Content)
					if k != nil && v != nil {
						if v.Value == "object" || v.Value == "array" {
							index.allInlineSchemaObjectDefinitions = append(index.allInlineSchemaObjectDefinitions, ref)
						}
					}
				}
			}

			if i%2 == 0 && n.Value == "$ref" {

				// Check if this reference is under an extension path (x-* field).
				// Always compute this so we can mark refs with IsExtensionRef.
				isExtensionPath := false
				for _, spi := range seenPath {
					if strings.HasPrefix(spi, "x-") {
						isExtensionPath = true
						break
					}
				}

				// If configured to exclude extension refs, skip it entirely.
				if index.config.ExcludeExtensionRefs && isExtensionPath {
					continue
				}

				// only look at scalar values, not maps (looking at you k8s)
				if len(node.Content) > i+1 {
					if !utils.IsNodeStringValue(node.Content[i+1]) {
						continue
					}
					// issue #481, don't look at refs in arrays, the next node isn't the value.
					if utils.IsNodeArray(node) {
						continue
					}
				}

				index.linesWithRefs[n.Line] = true

				fp := make([]string, len(seenPath))
				copy(fp, seenPath)

				if len(node.Content) > i+1 {

					value := node.Content[i+1].Value
					schemaIdBase := ""
					if scope != nil && len(scope.Chain) > 0 {
						schemaIdBase = scope.BaseUri
					}
					// extract last path segment without allocating a full slice
					lastSlash := strings.LastIndexByte(value, '/')
					var name string
					if lastSlash >= 0 {
						name = value[lastSlash+1:]
					} else {
						name = value
					}
					uri := strings.Split(value, "#/")

					// determine absolute path to this definition
					var defRoot string
					if strings.HasPrefix(index.specAbsolutePath, "http") {
						defRoot = index.specAbsolutePath
					} else {
						defRoot = filepath.Dir(index.specAbsolutePath)
					}

					var componentName string
					var fullDefinitionPath string
					if len(uri) == 2 {
						// Check if we are dealing with a ref to a local definition.
						if uri[0] == "" {
							fullDefinitionPath = fmt.Sprintf("%s#/%s", index.specAbsolutePath, uri[1])
							componentName = value
						} else {
							if strings.HasPrefix(uri[0], "http") {
								fullDefinitionPath = value
								componentName = fmt.Sprintf("#/%s", uri[1])
							} else {
								if filepath.IsAbs(uri[0]) {
									fullDefinitionPath = value
									componentName = fmt.Sprintf("#/%s", uri[1])
								} else {
									// if the index has a base URL, use that to resolve the path.
									if index.config.BaseURL != nil && !filepath.IsAbs(defRoot) {
										var u url.URL
										if strings.HasPrefix(defRoot, "http") {
											up, _ := url.Parse(defRoot)
											up.Path = utils.ReplaceWindowsDriveWithLinuxPath(filepath.Dir(up.Path))
											u = *up
										} else {
											u = *index.config.BaseURL
										}
										// abs, _ := filepath.Abs(filepath.Join(u.Path, uri[0]))
										// abs, _ := filepath.Abs(utils.CheckPathOverlap(u.Path, uri[0], string(os.PathSeparator)))
										abs := utils.CheckPathOverlap(u.Path, uri[0], string(os.PathSeparator))
										u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
										fullDefinitionPath = fmt.Sprintf("%s#/%s", u.String(), uri[1])
										componentName = fmt.Sprintf("#/%s", uri[1])

									} else {
										abs := index.resolveRelativeFilePath(defRoot, uri[0])
										fullDefinitionPath = fmt.Sprintf("%s#/%s", abs, uri[1])
										componentName = fmt.Sprintf("#/%s", uri[1])
									}
								}
							}
						}
					} else {
						if strings.HasPrefix(uri[0], "http") {
							fullDefinitionPath = value
						} else {
							// is it a relative file include?
							if !strings.Contains(uri[0], "#") {
								if strings.HasPrefix(defRoot, "http") {
									if !filepath.IsAbs(uri[0]) {
										u, _ := url.Parse(defRoot)
										pathDir := filepath.Dir(u.Path)
										// pathAbs, _ := filepath.Abs(filepath.Join(pathDir, uri[0]))
										pathAbs, _ := filepath.Abs(utils.CheckPathOverlap(pathDir, uri[0], string(os.PathSeparator)))
										pathAbs = utils.ReplaceWindowsDriveWithLinuxPath(pathAbs)
										u.Path = pathAbs
										fullDefinitionPath = u.String()
									}
								} else {
									if !filepath.IsAbs(uri[0]) {
										// if the index has a base URL, use that to resolve the path.
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
							}
						}
					}

					if fullDefinitionPath == "" && value != "" {
						fullDefinitionPath = value
					}
					if componentName == "" {
						componentName = value
					}

					_, p := utils.ConvertComponentIdIntoFriendlyPathSearch(componentName)

					// check for sibling properties
					siblingProps := make(map[string]*yaml.Node)
					var siblingKeys []*yaml.Node
					hasSiblings := len(node.Content) > 2

					if hasSiblings {
						for j := 0; j < len(node.Content); j += 2 {
							if j+1 < len(node.Content) && node.Content[j].Value != "$ref" {
								siblingProps[node.Content[j].Value] = node.Content[j+1]
								siblingKeys = append(siblingKeys, node.Content[j])
							}
						}
					}

					ref := &Reference{
						ParentNode:           parent,
						FullDefinition:       fullDefinitionPath,
						Definition:           componentName,
						RawRef:               value,
						SchemaIdBase:         schemaIdBase,
						Name:                 name,
						Node:                 node,
						KeyNode:              node.Content[i+1],
						Path:                 p,
						Index:                index,
						IsExtensionRef:       isExtensionPath,
						HasSiblingProperties: len(siblingProps) > 0,
						SiblingProperties:    siblingProps,
						SiblingKeys:          siblingKeys,
					}

					// add to raw sequenced refs
					index.rawSequencedRefs = append(index.rawSequencedRefs, ref)

					// add ref by line number
					refNameIndex := strings.LastIndex(value, "/")
					refName := value[refNameIndex+1:]
					if len(index.refsByLine[refName]) > 0 {
						index.refsByLine[refName][n.Line] = true
					} else {
						v := make(map[int]bool)
						v[n.Line] = true
						index.refsByLine[refName] = v
					}

					// if this ref value has any siblings (node.Content is larger than two elements)
					// then add to refs with siblings
					if len(node.Content) > 2 {
						copiedNode := *node

						// extract sibling properties and keys
						siblingProps := make(map[string]*yaml.Node)
						var siblingKeys []*yaml.Node

						for j := 0; j < len(node.Content); j += 2 {
							if j+1 < len(node.Content) && node.Content[j].Value != "$ref" {
								siblingProps[node.Content[j].Value] = node.Content[j+1]
								siblingKeys = append(siblingKeys, node.Content[j])
							}
						}

						copied := Reference{
							ParentNode:           parent,
							FullDefinition:       fullDefinitionPath,
							Definition:           ref.Definition,
							RawRef:               ref.RawRef,
							SchemaIdBase:         ref.SchemaIdBase,
							Name:                 ref.Name,
							Node:                 &copiedNode,
							KeyNode:              node.Content[i],
							Path:                 p,
							Index:                index,
							IsExtensionRef:       isExtensionPath,
							HasSiblingProperties: len(siblingProps) > 0,
							SiblingProperties:    siblingProps,
							SiblingKeys:          siblingKeys,
						}
						// protect this data using a copy, prevent the resolver from destroying things.
						index.refsWithSiblings[value] = copied
					}

					// if this is a polymorphic reference, we're going to leave it out
					// allRefs. We don't ever want these resolved, so instead of polluting
					// the timeline, we will keep each poly ref in its own collection for later
					// analysis.
					if poly {
						index.polymorphicRefs[value] = ref

						// index each type
						switch pName {
						case "anyOf":
							index.polymorphicAnyOfRefs = append(index.polymorphicAnyOfRefs, ref)
						case "allOf":
							index.polymorphicAllOfRefs = append(index.polymorphicAllOfRefs, ref)
						case "oneOf":
							index.polymorphicOneOfRefs = append(index.polymorphicOneOfRefs, ref)
						}
						continue
					}

					// check if this is a dupe, if so, skip it, we don't care now.
					if index.allRefs[value] != nil { // seen before, skip.
						continue
					}

					if value == "" {

						completedPath := fmt.Sprintf("$.%s", strings.Join(fp, "."))
						c := node.Content[i]
						if len(node.Content) > i+1 { // if the next node exists, use that.
							c = node.Content[i+1]
						}

						indexError := &IndexingError{
							Err:     errors.New("schema reference is empty and cannot be processed"),
							Node:    c,
							KeyNode: node.Content[i],
							Path:    completedPath,
						}

						index.refErrors = append(index.refErrors, indexError)
						continue
					}

					// This sets the ref in the path using the full URL and sub-path.
					index.allRefs[fullDefinitionPath] = ref
					found = append(found, ref)
				}
			}

			// Detect and register JSON Schema 2020-12 $id declarations
			if i%2 == 0 && n.Value == "$id" {
				if len(node.Content) > i+1 && utils.IsNodeStringValue(node.Content[i+1]) {
					idValue := node.Content[i+1].Value
					idNode := node.Content[i+1]

					// Build the definition path for this schema
					var definitionPath string
					if len(seenPath) > 0 {
						definitionPath = "#/" + strings.Join(seenPath, "/")
					} else {
						definitionPath = "#"
					}

					// Validate the $id (must not contain fragment)
					if err := ValidateSchemaId(idValue); err != nil {
						index.errorLock.Lock()
						index.refErrors = append(index.refErrors, &IndexingError{
							Err:     fmt.Errorf("invalid $id value '%s': %w", idValue, err),
							Node:    idNode,
							KeyNode: node.Content[i],
							Path:    definitionPath,
						})
						index.errorLock.Unlock()
						continue
					}

					// Resolve the $id against the PARENT scope's base URI (nearest ancestor $id)
					// This implements JSON Schema 2020-12 hierarchical $id resolution
					// We use parentBaseUri which was captured before this node's $id was pushed
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
						resolvedUri = idValue // Use original as fallback
					}

					// Create and register the schema ID entry
					// ParentId is the parent scope's base URI (if it differs from document base)
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

					// Register in the index (validation already done above)
					_ = index.RegisterSchemaId(entry)
				}
			}

			// Skip $ref and $id from path building - they are keywords, not schema properties
			if i%2 == 0 && n.Value != "$ref" && n.Value != "$id" && n.Value != "" {

				v := n.Value
				if strings.HasPrefix(v, "/") {
					v = strings.Replace(v, "/", "~1", 1)
				}

				loc := append(seenPath, v)
				definitionPath := "#/" + strings.Join(loc, "/")
				_, jsonPath := utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)

				// capture descriptions and summaries
				if n.Value == "description" {

					// if the parent is a sequence, ignore.
					if utils.IsNodeArray(node) {
						continue
					}
					// Skip if "description" is a property name inside schema properties
					// We check if the previous element in seenPath is "properties" and this is at an even index
					// (property names are at even indices, values at odd)
					if len(seenPath) > 0 && (seenPath[len(seenPath)-1] == "properties" || seenPath[len(seenPath)-1] == "patternProperties") {
						// This means "description" is a property name, not a description field, skip extraction
						seenPath = append(seenPath, strings.ReplaceAll(n.Value, "/", "~1"))
						prev = n.Value
						continue
					}
					if !slices.Contains(seenPath, "example") && !slices.Contains(seenPath, "examples") {
						ref := &DescriptionReference{
							ParentNode: parent,
							Content:    node.Content[i+1].Value,
							Path:       jsonPath,
							Node:       node.Content[i+1],
							KeyNode:    node.Content[i],
							IsSummary:  false,
						}

						if !utils.IsNodeMap(ref.Node) {
							index.allDescriptions = append(index.allDescriptions, ref)
							index.descriptionCount++
						}
					}
				}

				if n.Value == "summary" {

					// Skip if "summary" is a property name inside schema properties
					// We check if the previous element in seenPath is "properties" and this is at an even index
					// (property names are at even indices, values at odd)
					if len(seenPath) > 0 && (seenPath[len(seenPath)-1] == "properties" || seenPath[len(seenPath)-1] == "patternProperties") {
						// This means "summary" is a property name, not a summary field, skip extraction
						seenPath = append(seenPath, strings.ReplaceAll(n.Value, "/", "~1"))
						prev = n.Value
						continue
					}

					if slices.Contains(seenPath, "example") || slices.Contains(seenPath, "examples") {
						continue
					}

					var b *yaml.Node
					if len(node.Content) == i+1 {
						b = node.Content[i]
					} else {
						b = node.Content[i+1]
					}
					ref := &DescriptionReference{
						ParentNode: parent,
						Content:    b.Value,
						Path:       jsonPath,
						Node:       b,
						KeyNode:    n,
						IsSummary:  true,
					}

					index.allSummaries = append(index.allSummaries, ref)
					index.summaryCount++
				}

				// capture security requirement references (these are not traditional references, but they
				// are used as a look-up. This is the only exception to the design.
				if n.Value == "security" {
					var b *yaml.Node
					if len(node.Content) == i+1 {
						b = node.Content[i]
					} else {
						b = node.Content[i+1]
					}
					if utils.IsNodeArray(b) {
						var secKey string
						for k := range b.Content {
							if utils.IsNodeMap(b.Content[k]) {
								for g := range b.Content[k].Content {
									if g%2 == 0 {
										secKey = b.Content[k].Content[g].Value
										continue
									}
									if utils.IsNodeArray(b.Content[k].Content[g]) {
										var refMap map[string][]*Reference
										if index.securityRequirementRefs[secKey] == nil {
											index.securityRequirementRefs[secKey] = make(map[string][]*Reference)
											refMap = index.securityRequirementRefs[secKey]
										} else {
											refMap = index.securityRequirementRefs[secKey]
										}
										for r := range b.Content[k].Content[g].Content {
											var refs []*Reference
											if refMap[b.Content[k].Content[g].Content[r].Value] != nil {
												refs = refMap[b.Content[k].Content[g].Content[r].Value]
											}

											refs = append(refs, &Reference{
												Definition: b.Content[k].Content[g].Content[r].Value,
												Path:       fmt.Sprintf("%s.security[%d].%s[%d]", jsonPath, k, secKey, r),
												Node:       b.Content[k].Content[g].Content[r],
												KeyNode:    b.Content[k].Content[g],
											})

											index.securityRequirementRefs[secKey][b.Content[k].Content[g].Content[r].Value] = refs
										}
									}
								}
							}
						}
					}
				}
				// capture enums
				if n.Value == "enum" {

					if len(seenPath) > 0 {
						lastItem := seenPath[len(seenPath)-1]
						if lastItem == "properties" {
							seenPath = append(seenPath, strings.ReplaceAll(n.Value, "/", "~1"))
							prev = n.Value
							continue
						}
					}

					// all enums need to have a type, extract the type from the node where the enum was found.
					_, enumKeyValueNode := utils.FindKeyNodeTop("type", node.Content)

					if enumKeyValueNode != nil {
						ref := &EnumReference{
							ParentNode: parent,
							Path:       jsonPath,
							Node:       node.Content[i+1],
							KeyNode:    node.Content[i],
							Type:       enumKeyValueNode,
							SchemaNode: node,
						}

						index.allEnums = append(index.allEnums, ref)
						index.enumCount++
					}
				}
				// capture all objects with properties
				if n.Value == "properties" {
					_, typeKeyValueNode := utils.FindKeyNodeTop("type", node.Content)

					if typeKeyValueNode != nil {
						isObject := false

						if typeKeyValueNode.Value == "object" {
							isObject = true
						}

						for _, v := range typeKeyValueNode.Content {
							if v.Value == "object" {
								isObject = true
							}
						}

						if isObject {
							index.allObjectsWithProperties = append(index.allObjectsWithProperties, &ObjectReference{
								Path:       jsonPath,
								Node:       node,
								KeyNode:    n,
								ParentNode: parent,
							})
						}
					}
				}

				seenPath = append(seenPath, strings.ReplaceAll(n.Value, "/", "~1"))
				// seenPath = append(seenPath, n.Value)
				prev = n.Value
			}

			// if next node is map, don't add segment.
			if i < len(node.Content)-1 {
				next := node.Content[i+1]

				if i%2 != 0 && next != nil && !utils.IsNodeArray(next) && !utils.IsNodeMap(next) && len(seenPath) > 0 {
					seenPath = seenPath[:len(seenPath)-1]
				}
			}
		}
	}

	index.refCount = len(index.allRefs)

	return found
}

// ExtractComponentsFromRefs returns located components from references. The returned nodes from here
// can be used for resolving as they contain the actual object properties.
//
// This function uses singleflight to deduplicate concurrent lookups for the same reference,
// channel-based collection to avoid mutex contention during resolution, and sorts results
// by input position for deterministic ordering.
func (index *SpecIndex) ExtractComponentsFromRefs(ctx context.Context, refs []*Reference) []*Reference {
	if len(refs) == 0 {
		return nil
	}

	refsToCheck := refs
	mappedRefsInSequence := make([]*ReferenceMapped, len(refsToCheck))

	// Sequential mode: process refs one at a time (used for bundling)
	if index.config.ExtractRefsSequentially {
		found := make([]*Reference, 0, len(refsToCheck))
		for i, ref := range refsToCheck {
			located := index.locateRef(ctx, ref)
			if located != nil {
				index.refLock.Lock()
				if index.allMappedRefs[located.FullDefinition] == nil {
					index.allMappedRefs[located.FullDefinition] = located
					found = append(found, located)
				}
				mappedRefsInSequence[i] = &ReferenceMapped{
					OriginalReference: ref,
					Reference:         located,
					Definition:        located.Definition,
					FullDefinition:    located.FullDefinition,
				}
				index.refLock.Unlock()
			} else {
				// If SkipExternalRefResolution is enabled, don't record errors for external refs
				if index.config != nil && index.config.SkipExternalRefResolution && utils.IsExternalRef(ref.Definition) {
					continue
				}
				// Record error for definitive failure
				_, path := utils.ConvertComponentIdIntoFriendlyPathSearch(ref.Definition)
				index.errorLock.Lock()
				index.refErrors = append(index.refErrors, &IndexingError{
					Err:     fmt.Errorf("component `%s` does not exist in the specification", ref.Definition),
					Node:    ref.Node,
					Path:    path,
					KeyNode: ref.KeyNode,
				})
				index.errorLock.Unlock()
			}
		}
		// Collect sequenced results
		for _, rm := range mappedRefsInSequence {
			if rm != nil {
				index.allMappedRefsSequenced = append(index.allMappedRefsSequenced, rm)
			}
		}
		return found
	}

	// Async mode: use singleflight for deduplication and channel-based collection
	var wg sync.WaitGroup
	var sfGroup singleflight.Group // Local to this call - no cross-index coupling

	// Channel-based collection - no mutex needed during resolution
	resultsChan := make(chan indexedRef, len(refsToCheck))

	// Concurrency control
	maxConcurrency := runtime.GOMAXPROCS(0)
	if maxConcurrency < 4 {
		maxConcurrency = 4
	}
	sem := make(chan struct{}, maxConcurrency)

	for i, ref := range refsToCheck {
		i, ref := i, ref // capture loop variables
		wg.Add(1)

		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			defer wg.Done()

			// Singleflight deduplication - one lookup per FullDefinition
			result, _, _ := sfGroup.Do(ref.FullDefinition, func() (interface{}, error) {
				// Fast path: already mapped
				index.refLock.RLock()
				if existing := index.allMappedRefs[ref.FullDefinition]; existing != nil {
					index.refLock.RUnlock()
					return existing, nil
				}
				index.refLock.RUnlock()

				// Do the actual lookup (only one goroutine per FullDefinition)
				return index.locateRef(ctx, ref), nil
			})

			// Type assert and check for nil - interface containing nil pointer is not nil
			located := result.(*Reference)
			if located != nil {
				resultsChan <- indexedRef{ref: located, pos: i}
			} else {
				resultsChan <- indexedRef{ref: nil, pos: i} // Track failures for reconciliation
			}
		}()
	}

	// Close channel after all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results - single consumer, no lock needed
	collected := make([]indexedRef, 0, len(refsToCheck))
	for r := range resultsChan {
		collected = append(collected, r)
	}

	// Sort by input position for deterministic ordering
	if !preserveLegacyRefOrder {
		sort.Slice(collected, func(i, j int) bool {
			return collected[i].pos < collected[j].pos
		})
	}

	// RECONCILIATION PHASE: Build final results with minimal locking
	found := make([]*Reference, 0, len(collected))

	for _, c := range collected {
		ref := refsToCheck[c.pos]
		located := c.ref

		// Reconcile nil results - check if another goroutine succeeded.
		// We use ref.FullDefinition here because that's the singleflight key,
		// and located.FullDefinition should match ref.FullDefinition for the
		// same reference (FindComponent returns the component at that definition).
		if located == nil {
			index.refLock.RLock()
			located = index.allMappedRefs[ref.FullDefinition]
			index.refLock.RUnlock()
		}

		if located != nil {
			// Add to allMappedRefs if not present
			index.refLock.Lock()
			if index.allMappedRefs[located.FullDefinition] == nil {
				index.allMappedRefs[located.FullDefinition] = located
				found = append(found, located)
			}
			mappedRefsInSequence[c.pos] = &ReferenceMapped{
				OriginalReference: ref,
				Reference:         located,
				Definition:        located.Definition,
				FullDefinition:    located.FullDefinition,
			}
			index.refLock.Unlock()
		} else {
			// If SkipExternalRefResolution is enabled, don't record errors for external refs
			if index.config != nil && index.config.SkipExternalRefResolution && utils.IsExternalRef(ref.Definition) {
				continue
			}
			// Definitive failure - record error
			_, path := utils.ConvertComponentIdIntoFriendlyPathSearch(ref.Definition)
			index.errorLock.Lock()
			index.refErrors = append(index.refErrors, &IndexingError{
				Err:     fmt.Errorf("component `%s` does not exist in the specification", ref.Definition),
				Node:    ref.Node,
				Path:    path,
				KeyNode: ref.KeyNode,
			})
			index.errorLock.Unlock()
		}
	}

	// Collect sequenced results in input order
	for _, rm := range mappedRefsInSequence {
		if rm != nil {
			index.allMappedRefsSequenced = append(index.allMappedRefsSequenced, rm)
		}
	}

	return found
}

// locateRef finds a component for a reference, including KeyNode extraction.
// This is a helper used by ExtractComponentsFromRefs to isolate the lookup logic.
func (index *SpecIndex) locateRef(ctx context.Context, ref *Reference) *Reference {
	// External references require a full Lock (not RLock) during FindComponent because
	// FindComponent may trigger rolodex file loading which mutates index state.
	// Internal references can proceed without locking since they only read from
	// already-populated data structures.
	uri := strings.Split(ref.FullDefinition, "#/")
	isExternalRef := len(uri) == 2 && len(uri[0]) > 0
	if isExternalRef {
		index.refLock.Lock()
	}
	located := index.FindComponent(ctx, ref.FullDefinition)
	if isExternalRef {
		index.refLock.Unlock()
	}

	if located == nil {
		rawRef := ref.RawRef
		if rawRef == "" {
			rawRef = ref.FullDefinition
		}
		normalizedRef := resolveRefWithSchemaBase(rawRef, ref.SchemaIdBase)
		if resolved := index.ResolveRefViaSchemaId(normalizedRef); resolved != nil {
			located = resolved
		} else {
			return nil
		}
	}

	// Extract KeyNode - yamlpath API returns subnodes only, so we need to
	// rollback in the nodemap a line (if we can) to extract the keynode.
	if located.Node != nil {
		index.nodeMapLock.RLock()
		if located.Node.Line > 1 && len(index.nodeMap[located.Node.Line-1]) > 0 {
			for _, v := range index.nodeMap[located.Node.Line-1] {
				located.KeyNode = v
				break
			}
		}
		index.nodeMapLock.RUnlock()
	}

	return located
}
