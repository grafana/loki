// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// GetPathCount returns the number of paths defined in the specification. Returns -1 if root is nil.
func (index *SpecIndex) GetPathCount() int {
	if index.root == nil {
		return -1
	}
	if index.pathCount > 0 {
		return index.pathCount
	}
	pc := 0
	for i, n := range index.root.Content[0].Content {
		if i%2 == 0 && n.Value == "paths" {
			pn := index.root.Content[0].Content[i+1].Content
			index.pathsNode = index.root.Content[0].Content[i+1]
			pc = len(pn) / 2
		}
	}
	index.pathCount = pc
	return pc
}

// ExtractExternalDocuments recursively searches the YAML tree for externalDocs objects and returns
// references to each one found.
func (index *SpecIndex) ExtractExternalDocuments(node *yaml.Node) []*Reference {
	if node == nil {
		return nil
	}
	var found []*Reference
	if len(node.Content) > 0 {
		for i, n := range node.Content {
			if utils.IsNodeMap(n) || utils.IsNodeArray(n) {
				found = append(found, index.ExtractExternalDocuments(n)...)
			}
			if i%2 == 0 && n.Value == "externalDocs" {
				docNode := node.Content[i+1]
				_, urlNode := utils.FindKeyNode("url", docNode.Content)
				if urlNode != nil {
					ref := &Reference{Definition: urlNode.Value, Name: urlNode.Value, Node: docNode}
					index.externalDocumentsRef = append(index.externalDocumentsRef, ref)
					found = append(found, ref)
				}
			}
		}
	}
	index.externalDocumentsCount = len(index.externalDocumentsRef)
	return found
}

// GetGlobalTagsCount returns the number of top-level tags and also extracts tag references
// and checks for circular parent references. Returns -1 if root is nil.
func (index *SpecIndex) GetGlobalTagsCount() int {
	if index.root == nil {
		return -1
	}
	if index.globalTagsCount > 0 {
		return index.globalTagsCount
	}
	for i, n := range index.root.Content[0].Content {
		if i%2 == 0 && n.Value == "tags" {
			tagsNode := index.root.Content[0].Content[i+1]
			if tagsNode != nil {
				index.tagsNode = tagsNode
				index.globalTagsCount = len(tagsNode.Content)
				for x, tagNode := range index.tagsNode.Content {
					_, name := utils.FindKeyNode("name", tagNode.Content)
					_, description := utils.FindKeyNode("description", tagNode.Content)
					desc := ""
					if description == nil {
						desc = ""
					}
					if name != nil {
						index.globalTagRefs[name.Value] = &Reference{
							Definition: desc,
							Name:       name.Value,
							Node:       tagNode,
							Path:       fmt.Sprintf("$.tags[%d]", x),
						}
					}
				}
				index.checkTagCircularReferences()
			}
		}
	}
	return index.globalTagsCount
}

func (index *SpecIndex) checkTagCircularReferences() {
	if index.tagsNode == nil {
		return
	}

	tagParentMap := make(map[string]string)
	tagRefs := make(map[string]*Reference)
	tagNodes := make(map[string]*yaml.Node)

	for x, tagNode := range index.tagsNode.Content {
		_, nameNode := utils.FindKeyNode("name", tagNode.Content)
		_, parentNode := utils.FindKeyNode("parent", tagNode.Content)
		if nameNode != nil {
			tagName := nameNode.Value
			tagNodes[tagName] = tagNode
			tagRefs[tagName] = &Reference{Name: tagName, Node: tagNode, Path: fmt.Sprintf("$.tags[%d]", x)}
			if parentNode != nil {
				tagParentMap[tagName] = parentNode.Value
			}
		}
	}

	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	for tagName := range tagRefs {
		if !visited[tagName] {
			if _, hasParent := tagParentMap[tagName]; hasParent {
				if path := index.detectTagCircularHelper(tagName, tagParentMap, tagRefs, visited, recStack, []string{}); len(path) > 0 {
					journey := make([]*Reference, len(path))
					for i, name := range path {
						journey[i] = tagRefs[name]
					}
					loopIndex := -1
					loopStart := path[len(path)-1]
					for i, name := range path {
						if name == loopStart {
							loopIndex = i
							break
						}
					}
					index.tagCircularReferences = append(index.tagCircularReferences, &CircularReferenceResult{
						Journey:        journey,
						Start:          tagRefs[path[0]],
						LoopIndex:      loopIndex,
						LoopPoint:      tagRefs[loopStart],
						ParentNode:     tagNodes[loopStart],
						IsInfiniteLoop: true,
					})
				}
			}
		}
	}
}

func (index *SpecIndex) detectTagCircularHelper(tagName string, parentMap map[string]string, tagRefs map[string]*Reference, visited map[string]bool, recStack map[string]bool, path []string) []string {
	if _, exists := tagRefs[tagName]; !exists {
		return []string{}
	}
	visited[tagName] = true
	recStack[tagName] = true
	path = append(path, tagName)

	if parentName, hasParent := parentMap[tagName]; hasParent {
		if _, parentExists := tagRefs[parentName]; !parentExists {
			recStack[tagName] = false
			return []string{}
		}
		if recStack[parentName] {
			return append(path, parentName)
		}
		if !visited[parentName] {
			if cyclePath := index.detectTagCircularHelper(parentName, parentMap, tagRefs, visited, recStack, path); len(cyclePath) > 0 {
				return cyclePath
			}
		}
	}

	recStack[tagName] = false
	return []string{}
}

// GetOperationTagsCount returns the number of unique tags referenced across all operations.
func (index *SpecIndex) GetOperationTagsCount() int {
	if index.root == nil {
		return -1
	}
	if index.operationTagsCount > 0 {
		return index.operationTagsCount
	}
	seen := make(map[string]bool)
	count := 0
	for _, path := range index.operationTagsRefs {
		for _, method := range path {
			for _, tag := range method {
				if !seen[tag.Name] {
					seen[tag.Name] = true
					count++
				}
			}
		}
	}
	index.operationTagsCount = count
	return count
}

// GetTotalTagsCount returns the combined count of unique global and operation tags.
func (index *SpecIndex) GetTotalTagsCount() int {
	if index.root == nil {
		return -1
	}
	if index.totalTagsCount > 0 {
		return index.totalTagsCount
	}
	seen := make(map[string]bool)
	count := 0
	for _, gt := range index.globalTagRefs {
		if !seen[gt.Name] {
			seen[gt.Name] = true
			count++
		}
	}
	for _, ot := range index.operationTagsRefs {
		for _, m := range ot {
			for _, t := range m {
				if !seen[t.Name] {
					seen[t.Name] = true
					count++
				}
			}
		}
	}
	index.totalTagsCount = count
	return count
}

// GetGlobalCallbacksCount returns the total number of callback objects found across all operations.
func (index *SpecIndex) GetGlobalCallbacksCount() int {
	if index.root == nil {
		return -1
	}
	if index.globalCallbacksCount > 0 {
		return index.globalCallbacksCount
	}
	index.pathRefsLock.RLock()
	for path, p := range index.pathRefs {
		for _, m := range p {
			index.globalCallbacksCount += index.collectOperationObjectReferences(path, m, "callbacks", index.callbacksRefs)
		}
	}
	index.pathRefsLock.RUnlock()
	return index.globalCallbacksCount
}

// GetGlobalLinksCount returns the total number of link objects found across all operations.
func (index *SpecIndex) GetGlobalLinksCount() int {
	if index.root == nil {
		return -1
	}
	if index.globalLinksCount > 0 {
		return index.globalLinksCount
	}
	for path, p := range index.pathRefs {
		for _, m := range p {
			index.globalLinksCount += index.collectOperationObjectReferences(path, m, "links", index.linksRefs)
		}
	}
	return index.globalLinksCount
}

func (index *SpecIndex) collectOperationObjectReferences(path string, operation *Reference, key string, target map[string]map[string][]*Reference) int {
	var count int
	for _, container := range findNestedObjectContainers(operation.Node, key) {
		for _, node := range container.Content {
			if !utils.IsNodeMap(node) {
				continue
			}
			if target[path] == nil {
				target[path] = make(map[string][]*Reference)
			}
			target[path][operation.Name] = append(target[path][operation.Name], &Reference{
				Definition: operation.Name,
				Name:       operation.Name,
				Node:       node,
			})
			count++
		}
	}
	return count
}

func findNestedObjectContainers(node *yaml.Node, key string) []*yaml.Node {
	if node == nil {
		return nil
	}

	var found []*yaml.Node
	var visit func(*yaml.Node)
	visit = func(current *yaml.Node) {
		if current == nil {
			return
		}
		switch current.Kind {
		case yaml.DocumentNode:
			for _, child := range current.Content {
				visit(child)
			}
		case yaml.MappingNode:
			for i := 0; i < len(current.Content)-1; i += 2 {
				k := current.Content[i]
				v := current.Content[i+1]
				if k != nil && k.Value == key && v != nil && v.Kind == yaml.MappingNode {
					found = append(found, v)
				}
				visit(v)
			}
		case yaml.SequenceNode:
			for _, child := range current.Content {
				visit(child)
			}
		}
	}

	visit(node)
	return found
}

// GetRawReferenceCount returns the total number of raw (non-deduplicated) references found.
func (index *SpecIndex) GetRawReferenceCount() int { return len(index.rawSequencedRefs) }

// GetComponentSchemaCount extracts and counts all component schemas, parameters, request bodies,
// responses, security schemes, headers, examples, links, callbacks, and path items from the
// specification. Also handles Swagger 2.0 "definitions" and "securityDefinitions" sections.
func (index *SpecIndex) GetComponentSchemaCount() int {
	if index.root == nil || len(index.root.Content) == 0 {
		return -1
	}
	if index.schemaCount > 0 {
		return index.schemaCount
	}

	for i, n := range index.root.Content[0].Content {
		if i%2 != 0 {
			continue
		}
		if n.Value == "servers" {
			index.rootServersNode = index.root.Content[0].Content[i+1]
			if i+1 < len(index.root.Content[0].Content) {
				serverDefinitions := index.root.Content[0].Content[i+1]
				for x, def := range serverDefinitions.Content {
					index.serversRefs = append(index.serversRefs, &Reference{
						Definition: "servers",
						Name:       "server",
						Node:       def,
						Path:       fmt.Sprintf("$.servers[%d]", x),
						ParentNode: index.rootServersNode,
					})
				}
			}
		}
		if n.Value == "security" {
			index.rootSecurityNode = index.root.Content[0].Content[i+1]
			if i+1 < len(index.root.Content[0].Content) {
				securityDefinitions := index.root.Content[0].Content[i+1]
				for x, def := range securityDefinitions.Content {
					if len(def.Content) > 0 {
						name := def.Content[0]
						index.rootSecurity = append(index.rootSecurity, &Reference{
							Definition: name.Value,
							Name:       name.Value,
							Node:       def,
							Path:       fmt.Sprintf("$.security[%d]", x),
						})
					}
				}
			}
		}
		if n.Value == "components" {
			_, schemasNode := utils.FindKeyNode("schemas", index.root.Content[0].Content[i+1].Content)
			_, parametersNode := utils.FindKeyNode("parameters", index.root.Content[0].Content[i+1].Content)
			_, requestBodiesNode := utils.FindKeyNode("requestBodies", index.root.Content[0].Content[i+1].Content)
			_, responsesNode := utils.FindKeyNode("responses", index.root.Content[0].Content[i+1].Content)
			_, securitySchemesNode := utils.FindKeyNode("securitySchemes", index.root.Content[0].Content[i+1].Content)
			_, headersNode := utils.FindKeyNode("headers", index.root.Content[0].Content[i+1].Content)
			_, examplesNode := utils.FindKeyNode("examples", index.root.Content[0].Content[i+1].Content)
			_, linksNode := utils.FindKeyNode("links", index.root.Content[0].Content[i+1].Content)
			_, callbacksNode := utils.FindKeyNode("callbacks", index.root.Content[0].Content[i+1].Content)
			_, pathItemsNode := utils.FindKeyNode("pathItems", index.root.Content[0].Content[i+1].Content)

			if schemasNode != nil {
				index.extractDefinitionsAndSchemas(schemasNode, "#/components/schemas/")
				index.schemasNode = schemasNode
				index.schemaCount = len(schemasNode.Content) / 2
			}
			if parametersNode != nil {
				index.extractComponentParameters(parametersNode, "#/components/parameters/")
				index.componentLock.Lock()
				index.parametersNode = parametersNode
				index.componentLock.Unlock()
			}
			if requestBodiesNode != nil {
				index.extractComponentRequestBodies(requestBodiesNode, "#/components/requestBodies/")
				index.requestBodiesNode = requestBodiesNode
			}
			if responsesNode != nil {
				index.extractComponentResponses(responsesNode, "#/components/responses/")
				index.responsesNode = responsesNode
			}
			if securitySchemesNode != nil {
				index.extractComponentSecuritySchemes(securitySchemesNode, "#/components/securitySchemes/")
				index.securitySchemesNode = securitySchemesNode
			}
			if headersNode != nil {
				index.extractComponentHeaders(headersNode, "#/components/headers/")
				index.headersNode = headersNode
			}
			if examplesNode != nil {
				index.extractComponentExamples(examplesNode, "#/components/examples/")
				index.examplesNode = examplesNode
			}
			if linksNode != nil {
				index.extractComponentLinks(linksNode, "#/components/links/")
				index.linksNode = linksNode
			}
			if callbacksNode != nil {
				index.extractComponentCallbacks(callbacksNode, "#/components/callbacks/")
				index.callbacksNode = callbacksNode
			}
			if pathItemsNode != nil {
				index.extractComponentPathItems(pathItemsNode, "#/components/pathItems/")
				index.pathItemsNode = pathItemsNode
			}
		}
		if n.Value == "definitions" {
			schemasNode := index.root.Content[0].Content[i+1]
			if schemasNode != nil {
				index.extractDefinitionsAndSchemas(schemasNode, "#/definitions/")
				index.schemasNode = schemasNode
				index.schemaCount = len(schemasNode.Content) / 2
			}
		}
		if n.Value == "parameters" {
			parametersNode := index.root.Content[0].Content[i+1]
			if parametersNode != nil {
				index.extractComponentParameters(parametersNode, "#/parameters/")
				index.componentLock.Lock()
				index.parametersNode = parametersNode
				index.componentLock.Unlock()
			}
		}
		if n.Value == "responses" {
			responsesNode := index.root.Content[0].Content[i+1]
			if responsesNode != nil {
				index.extractComponentResponses(responsesNode, "#/responses/")
				index.responsesNode = responsesNode
			}
		}
		if n.Value == "securityDefinitions" {
			securityDefinitionsNode := index.root.Content[0].Content[i+1]
			if securityDefinitionsNode != nil {
				index.extractComponentSecuritySchemes(securityDefinitionsNode, "#/securityDefinitions/")
				index.securitySchemesNode = securityDefinitionsNode
			}
		}
	}
	return index.schemaCount
}

// GetComponentParameterCount returns the number of component-level parameter definitions.
func (index *SpecIndex) GetComponentParameterCount() int {
	if index.root == nil {
		return -1
	}
	if index.componentParamCount > 0 {
		return index.componentParamCount
	}
	for i, n := range index.root.Content[0].Content {
		if i%2 == 0 {
			if n.Value == "components" {
				_, parametersNode := utils.FindKeyNode("parameters", index.root.Content[0].Content[i+1].Content)
				if parametersNode != nil {
					index.componentLock.Lock()
					index.parametersNode = parametersNode
					index.componentParamCount = len(parametersNode.Content) / 2
					index.componentLock.Unlock()
				}
			}
			if n.Value == "parameters" {
				parametersNode := index.root.Content[0].Content[i+1]
				if parametersNode != nil {
					index.componentLock.Lock()
					index.parametersNode = parametersNode
					index.componentParamCount = len(parametersNode.Content) / 2
					index.componentLock.Unlock()
				}
			}
		}
	}
	return index.componentParamCount
}

// GetOperationCount returns the total number of operations across all paths and extracts
// path-level and operation-level references (methods, tags, descriptions, summaries, servers).
func (index *SpecIndex) GetOperationCount() int {
	if index.root == nil || index.pathsNode == nil {
		return -1
	}
	if index.operationCount > 0 {
		return index.operationCount
	}
	opCount := 0
	locatedPathRefs := make(map[string]map[string]*Reference)
	for x, p := range index.pathsNode.Content {
		if x%2 == 0 {
			var method *yaml.Node
			if utils.IsNodeArray(index.pathsNode) {
				method = index.pathsNode.Content[x]
			} else {
				method = index.pathsNode.Content[x+1]
			}
			if isRef, _, ref := utils.IsNodeRefValue(method); isRef {
				ctx := context.WithValue(context.Background(), CurrentPathKey, index.specAbsolutePath)
				ctx = context.WithValue(ctx, RootIndexKey, index)
				pNode := seekRefEnd(ctx, index, ref)
				if pNode != nil {
					method = pNode.Node
				}
			}
			for y, m := range method.Content {
				if y%2 == 0 {
					valid := false
					for _, methodType := range methodTypes {
						if m.Value == methodType {
							valid = true
						}
					}
					if valid {
						ref := &Reference{
							Definition: m.Value,
							Name:       m.Value,
							Node:       method.Content[y+1],
							Path:       fmt.Sprintf("$.paths['%s'].%s", p.Value, m.Value),
							ParentNode: m,
						}
						if locatedPathRefs[p.Value] == nil {
							locatedPathRefs[p.Value] = make(map[string]*Reference)
						}
						locatedPathRefs[p.Value][ref.Name] = ref
						opCount++
					}
				}
			}
		}
	}
	for k, v := range locatedPathRefs {
		index.pathRefs[k] = v
	}
	index.operationCount = opCount
	return opCount
}

// GetOperationsParameterCount scans all path items and operations to count parameters,
// extract tags, descriptions, summaries, and servers. Also builds the inline parameter
// deduplication maps.
func (index *SpecIndex) GetOperationsParameterCount() int {
	if index.root == nil || index.pathsNode == nil {
		return -1
	}
	if index.operationParamCount > 0 {
		return index.operationParamCount
	}
	for x, pathItemNode := range index.pathsNode.Content {
		if x%2 == 0 {
			var pathPropertyNode *yaml.Node
			if utils.IsNodeArray(index.pathsNode) {
				pathPropertyNode = index.pathsNode.Content[x]
			} else {
				pathPropertyNode = index.pathsNode.Content[x+1]
			}
			if isRef, _, ref := utils.IsNodeRefValue(pathPropertyNode); isRef {
				ctx := context.WithValue(context.Background(), CurrentPathKey, index.specAbsolutePath)
				ctx = context.WithValue(ctx, RootIndexKey, index)
				pNode := seekRefEnd(ctx, index, ref)
				if pNode != nil {
					pathPropertyNode = pNode.Node
				}
			}
			for y, prop := range pathPropertyNode.Content {
				if y%2 == 0 {
					if prop.Value == "servers" {
						serversNode := pathPropertyNode.Content[y+1]
						if index.opServersRefs[pathItemNode.Value] == nil {
							index.opServersRefs[pathItemNode.Value] = make(map[string][]*Reference)
						}
						var serverRefs []*Reference
						for i, serverRef := range serversNode.Content {
							serverRefs = append(serverRefs, &Reference{
								Definition: serverRef.Value,
								Name:       serverRef.Value,
								Node:       serverRef,
								ParentNode: prop,
								Path:       fmt.Sprintf("$.paths['%s'].servers[%d]", pathItemNode.Value, i),
							})
						}
						index.opServersRefs[pathItemNode.Value]["top"] = serverRefs
					}
					if prop.Value == "parameters" {
						index.scanOperationParams(pathPropertyNode.Content[y+1].Content, pathPropertyNode.Content[y], pathItemNode, "top")
					}
					if isHttpMethod(prop.Value) {
						for z, httpMethodProp := range pathPropertyNode.Content[y+1].Content {
							if z%2 == 0 {
								if httpMethodProp.Value == "parameters" {
									index.scanOperationParams(pathPropertyNode.Content[y+1].Content[z+1].Content, pathPropertyNode.Content[y+1].Content[z], pathItemNode, prop.Value)
								}
								if httpMethodProp.Value == "tags" {
									tags := pathPropertyNode.Content[y+1].Content[z+1]
									if index.operationTagsRefs[pathItemNode.Value] == nil {
										index.operationTagsRefs[pathItemNode.Value] = make(map[string][]*Reference)
									}
									var tagRefs []*Reference
									for _, tagRef := range tags.Content {
										tagRefs = append(tagRefs, &Reference{Definition: tagRef.Value, Name: tagRef.Value, Node: tagRef})
									}
									index.operationTagsRefs[pathItemNode.Value][prop.Value] = tagRefs
								}
								if httpMethodProp.Value == "description" {
									desc := pathPropertyNode.Content[y+1].Content[z+1].Value
									if index.operationDescriptionRefs[pathItemNode.Value] == nil {
										index.operationDescriptionRefs[pathItemNode.Value] = make(map[string]*Reference)
									}
									index.operationDescriptionRefs[pathItemNode.Value][prop.Value] = &Reference{
										Definition: desc,
										Name:       "description",
										Node:       pathPropertyNode.Content[y+1].Content[z+1],
									}
								}
								if httpMethodProp.Value == "summary" {
									summary := pathPropertyNode.Content[y+1].Content[z+1].Value
									if index.operationSummaryRefs[pathItemNode.Value] == nil {
										index.operationSummaryRefs[pathItemNode.Value] = make(map[string]*Reference)
									}
									index.operationSummaryRefs[pathItemNode.Value][prop.Value] = &Reference{
										Definition: summary,
										Name:       "summary",
										Node:       pathPropertyNode.Content[y+1].Content[z+1],
									}
								}
								if httpMethodProp.Value == "servers" {
									serversNode := pathPropertyNode.Content[y+1].Content[z+1]
									var serverRefs []*Reference
									for i, serverRef := range serversNode.Content {
										serverRefs = append(serverRefs, &Reference{
											Definition: "servers",
											Name:       "servers",
											Node:       serverRef,
											ParentNode: httpMethodProp,
											Path:       fmt.Sprintf("$.paths['%s'].%s.servers[%d]", pathItemNode.Value, prop.Value, i),
										})
									}
									if index.opServersRefs[pathItemNode.Value] == nil {
										index.opServersRefs[pathItemNode.Value] = make(map[string][]*Reference)
									}
									index.opServersRefs[pathItemNode.Value][prop.Value] = serverRefs
								}
							}
						}
					}
				}
			}
		}
	}

	for key, component := range index.allMappedRefs {
		if strings.Contains(key, "/parameters/") {
			index.paramCompRefs[key] = component
			index.paramAllRefs[key] = component
		}
	}

	for path, params := range index.paramOpRefs {
		for mName, mValue := range params {
			for pName, pValue := range mValue {
				if !strings.HasPrefix(pName, "#") {
					index.paramInlineDuplicateNames[pName] = append(index.paramInlineDuplicateNames[pName], pValue...)
					for i := range pValue {
						if pValue[i] != nil {
							_, in := utils.FindKeyNodeTop("in", pValue[i].Node.Content)
							if in != nil {
								index.paramAllRefs[fmt.Sprintf("%s:::%s:::%s", path, mName, in.Value)] = pValue[i]
							} else {
								index.paramAllRefs[fmt.Sprintf("%s:::%s", path, mName)] = pValue[i]
							}
						}
					}
				}
			}
		}
	}

	index.operationParamCount = len(index.paramCompRefs) + len(index.paramInlineDuplicateNames)
	return index.operationParamCount
}

// GetInlineDuplicateParamCount returns the number of inline parameters that have duplicate names.
func (index *SpecIndex) GetInlineDuplicateParamCount() int {
	if index.componentsInlineParamDuplicateCount > 0 {
		return index.componentsInlineParamDuplicateCount
	}
	dCount := len(index.paramInlineDuplicateNames) - index.countUniqueInlineDuplicates()
	index.componentsInlineParamDuplicateCount = dCount
	return dCount
}

// GetInlineUniqueParamCount returns the number of unique inline parameter names.
func (index *SpecIndex) GetInlineUniqueParamCount() int {
	return index.countUniqueInlineDuplicates()
}

// GetAllDescriptionsCount returns the total number of description nodes found during indexing.
func (index *SpecIndex) GetAllDescriptionsCount() int { return len(index.allDescriptions) }

// GetAllSummariesCount returns the total number of summary nodes found during indexing.
func (index *SpecIndex) GetAllSummariesCount() int { return len(index.allSummaries) }
