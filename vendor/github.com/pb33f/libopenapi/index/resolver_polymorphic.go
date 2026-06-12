// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (resolver *Resolver) extractPolymorphicRelatives(
	ref *Reference,
	node, keywordNode *yaml.Node,
	state relativeWalkState,
	index int,
) []*Reference {
	var found []*Reference

	if index+1 < len(node.Content) && utils.IsNodeMap(node.Content[index+1]) {
		if k, v := utils.FindKeyNodeTop("items", node.Content[index+1].Content); v != nil {
			if utils.IsNodeMap(v) {
				if d, _, l := utils.IsNodeRefValue(v); d {
					resolver.visitPolymorphicReference(ref, keywordNode.Value, k, l, state, index)
				}
			}
		} else {
			v := node.Content[index+1]
			if utils.IsNodeMap(v) {
				if d, _, l := utils.IsNodeRefValue(v); d {
					resolver.visitPolymorphicReference(ref, keywordNode.Value, node.Content[index], l, state, index)
				}
			}
		}
	}

	if index+1 < len(node.Content) && utils.IsNodeArray(node.Content[index+1]) {
		for q := range node.Content[index+1].Content {
			v := node.Content[index+1].Content[q]
			if utils.IsNodeMap(v) {
				if d, _, l := utils.IsNodeRefValue(v); d {
					resolver.visitPolymorphicReference(ref, keywordNode.Value, node.Content[index], l, state, index)
				} else {
					found = append(found, resolver.extractRelativesWithState(ref, v, keywordNode, state.descend())...)
				}
			}
		}
	}
	return found
}

func (resolver *Resolver) visitPolymorphicReference(
	ref *Reference,
	polymorphicType string,
	parentNode *yaml.Node,
	lookup string,
	state relativeWalkState,
	loopIndex int,
) {
	def := resolver.buildDefPathWithSchemaBase(ref, lookup, state.schemaIDBase)
	mappedRefs, _ := resolver.specIndex.SearchIndexForReference(def)
	if mappedRefs == nil || mappedRefs.Circular {
		return
	}
	circ := false
	for f := range state.journey {
		if state.journey[f].FullDefinition == mappedRefs.FullDefinition {
			circ = true
			break
		}
	}
	if !circ {
		resolver.VisitReference(mappedRefs, state.foundRelatives, state.journey, state.resolve)
		return
	}

	loop := append(state.journey, mappedRefs)
	circRef := &CircularReferenceResult{
		ParentNode:          parentNode,
		Journey:             loop,
		Start:               mappedRefs,
		LoopIndex:           loopIndex,
		LoopPoint:           mappedRefs,
		PolymorphicType:     polymorphicType,
		IsPolymorphicResult: true,
	}
	mappedRefs.Seen = true
	mappedRefs.Circular = true
	if resolver.IgnorePoly {
		resolver.ignoredPolyReferences = append(resolver.ignoredPolyReferences, circRef)
	} else if !resolver.circChecked {
		resolver.circularReferences = append(resolver.circularReferences, circRef)
	}
}
