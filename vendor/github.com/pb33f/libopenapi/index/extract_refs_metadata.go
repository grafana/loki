// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

type metadataPathAction struct {
	appendSegment bool
	stop          bool
}

func (index *SpecIndex) extractNodeMetadata(node, parent *yaml.Node, seenPath []string, keyIndex int) metadataPathAction {
	keyNode := node.Content[keyIndex]
	if keyNode == nil || keyNode.Value == "" || keyNode.Value == "$ref" || keyNode.Value == "$id" {
		return metadataPathAction{}
	}

	segment := keyNode.Value
	if strings.HasPrefix(segment, "/") {
		segment = strings.Replace(segment, "/", "~1", 1)
	}

	var jsonPath string
	var jsonPathComputed bool
	computeJSONPath := func() string {
		if !jsonPathComputed {
			loc := append(seenPath, segment)
			definitionPath := "#/" + strings.Join(loc, "/")
			_, jsonPath = utils.ConvertComponentIdIntoFriendlyPathSearch(definitionPath)
			jsonPathComputed = true
		}
		return jsonPath
	}

	switch keyNode.Value {
	case "description":
		if utils.IsNodeArray(node) {
			return metadataPathAction{stop: true}
		}
		if isMetadataPropertyNamePath(seenPath) {
			return metadataPathAction{appendSegment: true, stop: true}
		}
		if !metadataPathContainsExamples(seenPath) {
			refNode := metadataValueNode(node, keyIndex)
			ref := &DescriptionReference{
				ParentNode: parent,
				Content:    refNode.Value,
				Path:       computeJSONPath(),
				Node:       refNode,
				KeyNode:    keyNode,
				IsSummary:  false,
			}
			if !utils.IsNodeMap(ref.Node) {
				index.allDescriptions = append(index.allDescriptions, ref)
				index.descriptionCount++
			}
		}
	case "summary":
		if isMetadataPropertyNamePath(seenPath) {
			return metadataPathAction{appendSegment: true, stop: true}
		}
		if metadataPathContainsExamples(seenPath) {
			return metadataPathAction{stop: true}
		}
		refNode := metadataValueNode(node, keyIndex)
		index.allSummaries = append(index.allSummaries, &DescriptionReference{
			ParentNode: parent,
			Content:    refNode.Value,
			Path:       computeJSONPath(),
			Node:       refNode,
			KeyNode:    keyNode,
			IsSummary:  true,
		})
		index.summaryCount++
	case "security":
		index.collectSecurityRequirementMetadata(node, keyIndex, computeJSONPath())
	case "enum":
		if len(seenPath) > 0 && seenPath[len(seenPath)-1] == "properties" {
			return metadataPathAction{appendSegment: true, stop: true}
		}
		index.collectEnumMetadata(node, parent, keyIndex, computeJSONPath())
	case "properties":
		index.collectObjectWithPropertiesMetadata(node, parent, keyNode, computeJSONPath())
	}

	return metadataPathAction{appendSegment: true}
}

func metadataValueNode(node *yaml.Node, keyIndex int) *yaml.Node {
	if len(node.Content) == keyIndex+1 {
		return node.Content[keyIndex]
	}
	return node.Content[keyIndex+1]
}

func isMetadataPropertyNamePath(seenPath []string) bool {
	if len(seenPath) == 0 {
		return false
	}
	last := seenPath[len(seenPath)-1]
	return last == "properties" || last == "patternProperties"
}

func metadataPathContainsExamples(seenPath []string) bool {
	return underOpenAPIExamplePath(seenPath)
}

func (index *SpecIndex) collectSecurityRequirementMetadata(node *yaml.Node, keyIndex int, basePath string) {
	if index.securityRequirementRefs == nil {
		index.securityRequirementRefs = make(map[string]map[string][]*Reference)
	}
	// Security requirements are an array of objects. Each object maps a security scheme
	// name (key) to an array of required scopes (value). For example:
	//   security:
	//     - oauth2: ["read", "write"]   <-- k=0, scheme="oauth2", scopes=["read","write"]
	//       apiKey: []                  <-- same k, scheme="apiKey", scopes=[]
	securityNode := metadataValueNode(node, keyIndex)
	if securityNode == nil || !utils.IsNodeArray(securityNode) {
		return
	}
	var secKey string
	for k := range securityNode.Content {
		// Outer loop: each security requirement object in the array.
		if !utils.IsNodeMap(securityNode.Content[k]) {
			continue
		}
		for g := range securityNode.Content[k].Content {
			// Inner loop: key-value pairs within a single requirement object.
			if g%2 == 0 {
				secKey = securityNode.Content[k].Content[g].Value
				continue
			}
			if !utils.IsNodeArray(securityNode.Content[k].Content[g]) {
				continue
			}
			var refMap map[string][]*Reference
			if index.securityRequirementRefs[secKey] == nil {
				index.securityRequirementRefs[secKey] = make(map[string][]*Reference)
				refMap = index.securityRequirementRefs[secKey]
			} else {
				refMap = index.securityRequirementRefs[secKey]
			}
			for r := range securityNode.Content[k].Content[g].Content {
				valueNode := securityNode.Content[k].Content[g].Content[r]
				var refs []*Reference
				if refMap[valueNode.Value] != nil {
					refs = refMap[valueNode.Value]
				}
				refs = append(refs, &Reference{
					Definition: valueNode.Value,
					Path:       fmt.Sprintf("%s.security[%d].%s[%d]", basePath, k, secKey, r),
					Node:       valueNode,
					KeyNode:    securityNode.Content[k].Content[g],
				})
				index.securityRequirementRefs[secKey][valueNode.Value] = refs
			}
		}
	}
}

func (index *SpecIndex) collectEnumMetadata(node, parent *yaml.Node, keyIndex int, jsonPath string) {
	if keyIndex+1 >= len(node.Content) {
		return
	}
	_, enumTypeNode := utils.FindKeyNodeTop("type", node.Content)
	if enumTypeNode == nil {
		return
	}
	index.allEnums = append(index.allEnums, &EnumReference{
		ParentNode: parent,
		Path:       jsonPath,
		Node:       node.Content[keyIndex+1],
		KeyNode:    node.Content[keyIndex],
		Type:       enumTypeNode,
		SchemaNode: node,
	})
	index.enumCount++
}

func (index *SpecIndex) collectObjectWithPropertiesMetadata(node, parent, keyNode *yaml.Node, jsonPath string) {
	_, typeKeyValueNode := utils.FindKeyNodeTop("type", node.Content)
	if typeKeyValueNode == nil {
		return
	}

	isObject := typeKeyValueNode.Value == "object"
	if !isObject {
		for _, valueNode := range typeKeyValueNode.Content {
			if valueNode.Value == "object" {
				isObject = true
				break
			}
		}
	}

	if isObject {
		index.allObjectsWithProperties = append(index.allObjectsWithProperties, &ObjectReference{
			Path:       jsonPath,
			Node:       node,
			KeyNode:    keyNode,
			ParentNode: parent,
		})
	}
}
