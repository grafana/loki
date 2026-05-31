// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

func (resolver *Resolver) handleCircularJourneyRelative(ref, relative *Reference, journey []*Reference) bool {
	for loopIndex, journeyRef := range journey {
		if journeyRef.FullDefinition != relative.FullDefinition {
			continue
		}

		foundDup, _, _ := resolver.searchReferenceWithContext(ref, relative)
		if foundDup == nil {
			return true
		}
		if foundDup.Circular {
			return true
		}

		circRef := resolver.buildCircularReferenceResult(foundDup, relative, journey, loopIndex)
		resolver.recordCircularReferenceResult(circRef)
		resolver.markReferencesCircular(relative, foundDup)
		return true
	}
	return false
}

func (resolver *Resolver) buildCircularReferenceResult(
	foundDup, relative *Reference,
	journey []*Reference,
	loopIndex int,
) *CircularReferenceResult {
	loop := append(journey, foundDup)
	visitedDefinitions := make(map[string]bool)
	isInfiniteLoop, _ := resolver.isInfiniteCircularDependency(foundDup, visitedDefinitions, nil)

	return &CircularReferenceResult{
		ParentNode:     foundDup.ParentNode,
		Journey:        loop,
		Start:          foundDup,
		LoopIndex:      loopIndex,
		LoopPoint:      foundDup,
		IsArrayResult:  resolver.relativeIsArrayResult(relative),
		IsInfiniteLoop: isInfiniteLoop,
	}
}

func (resolver *Resolver) recordCircularReferenceResult(circRef *CircularReferenceResult) {
	if circRef == nil {
		return
	}
	if resolver.IgnorePoly && !circRef.IsArrayResult {
		resolver.ignoredPolyReferences = append(resolver.ignoredPolyReferences, circRef)
		return
	}
	if resolver.IgnoreArray && circRef.IsArrayResult {
		resolver.ignoredArrayReferences = append(resolver.ignoredArrayReferences, circRef)
		return
	}
	if !resolver.circChecked {
		resolver.circularReferences = append(resolver.circularReferences, circRef)
	}
}

func (resolver *Resolver) markReferencesCircular(relative, duplicate *Reference) {
	if relative != nil {
		relative.Seen = true
		relative.Circular = true
	}
	if duplicate != nil {
		duplicate.Seen = true
		duplicate.Circular = true
	}
}

func (resolver *Resolver) relativeIsArrayResult(relative *Reference) bool {
	if relative == nil {
		return false
	}
	if relative.ParentNodeSchemaType == "array" {
		return true
	}
	for _, nodeType := range relative.ParentNodeTypes {
		if nodeType == "array" {
			return true
		}
	}
	return false
}

func (resolver *Resolver) isInfiniteCircularDependency(
	ref *Reference, visitedDefinitions map[string]bool, initialRef *Reference,
) (bool, map[string]bool) {
	// Recursive DFS: walks all required $ref properties of ref, tracking visited
	// definitions to detect cycles. initialRef anchors the starting point so we
	// can recognize when the chain loops back to the origin.
	if ref == nil {
		return false, visitedDefinitions
	}
	for refDefinition := range ref.RequiredRefProperties {
		r, _ := resolver.specIndex.SearchIndexForReference(refDefinition)

		// Direct loop back to the original starting reference — infinite cycle.
		if initialRef != nil && initialRef.FullDefinition == r.FullDefinition {
			return true, visitedDefinitions
		}
		// Self-reference: ref points back to itself.
		if len(visitedDefinitions) > 0 && ref.FullDefinition == r.FullDefinition {
			return true, visitedDefinitions
		}
		// Already visited in this DFS path — skip to avoid re-processing.
		if visitedDefinitions[r.FullDefinition] {
			continue
		}

		visitedDefinitions[r.FullDefinition] = true
		ir := initialRef
		if ir == nil {
			ir = ref
		}

		isChildICD, visitedDefinitions := resolver.isInfiniteCircularDependency(r, visitedDefinitions, ir)
		if isChildICD {
			return true, visitedDefinitions
		}
	}

	return false, visitedDefinitions
}
