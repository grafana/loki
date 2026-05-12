// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"sort"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func visitIndexWithoutDamagingIt(res *Resolver, idx *SpecIndex) {
	mapped := idx.GetMappedReferencesSequenced()
	mappedIndex := idx.GetMappedReferences()
	res.indexesVisited++
	for _, ref := range mapped {
		seenReferences := make(map[string]bool)
		var journey []*Reference
		res.journeysTaken++
		res.VisitReference(ref.Reference, seenReferences, journey, false)
	}

	schemas := idx.GetAllComponentSchemas()
	schemaKeys := make([]string, 0, len(schemas))
	for k := range schemas {
		schemaKeys = append(schemaKeys, k)
	}
	sort.Strings(schemaKeys)

	for _, s := range schemaKeys {
		schemaRef := schemas[s]
		if mappedIndex[s] == nil {
			seenReferences := make(map[string]bool)
			var journey []*Reference
			res.journeysTaken++
			res.VisitReference(schemaRef, seenReferences, journey, false)
		}
	}
}

type refMap struct {
	ref   *Reference
	nodes []*yaml.Node
}

func visitIndex(res *Resolver, idx *SpecIndex) {
	mapped := idx.GetMappedReferencesSequenced()
	mappedIndex := idx.GetMappedReferences()
	res.indexesVisited++

	var refs []refMap
	for _, ref := range mapped {
		seenReferences := make(map[string]bool)
		var journey []*Reference
		res.journeysTaken++
		if ref != nil && ref.Reference != nil {
			n := res.VisitReference(ref.Reference, seenReferences, journey, true)
			if !ref.Reference.Circular {
				if ok, _, _ := utils.IsNodeRefValue(ref.OriginalReference.Node); ok {
					refs = append(refs, refMap{ref: ref.OriginalReference, nodes: n})
				}
			}
		}
	}
	idx.pendingResolve = refs

	schemas := idx.GetAllComponentSchemas()
	schemaKeys := make([]string, 0, len(schemas))
	for k := range schemas {
		schemaKeys = append(schemaKeys, k)
	}
	sort.Strings(schemaKeys)

	for _, s := range schemaKeys {
		schemaRef := schemas[s]
		if mappedIndex[s] == nil {
			seenReferences := make(map[string]bool)
			var journey []*Reference
			res.journeysTaken++
			schemaRef.Node.Content = res.VisitReference(schemaRef, seenReferences, journey, true)
		}
	}

	securitySchemes := idx.GetAllSecuritySchemes()
	securityKeys := make([]string, 0, len(securitySchemes))
	for k := range securitySchemes {
		securityKeys = append(securityKeys, k)
	}
	sort.Strings(securityKeys)

	for _, s := range securityKeys {
		schemaRef := securitySchemes[s]
		if mappedIndex[s] == nil {
			seenReferences := make(map[string]bool)
			var journey []*Reference
			res.journeysTaken++
			schemaRef.Node.Content = res.VisitReference(schemaRef, seenReferences, journey, true)
		}
	}

	for _, sequenced := range idx.GetAllSequencedReferences() {
		locatedDef := mappedIndex[sequenced.FullDefinition]
		if locatedDef != nil && !locatedDef.Circular {
			sequenced.Node.Content = locatedDef.Node.Content
		}
	}
}

func (resolver *Resolver) searchReferenceWithContext(sourceRef, searchRef *Reference) (*Reference, *SpecIndex, context.Context) {
	if resolver.specIndex == nil || resolver.specIndex.config == nil || !resolver.specIndex.config.ResolveNestedRefsWithDocumentContext {
		ref, idx := resolver.specIndex.SearchIndexForReferenceByReference(searchRef)
		return ref, idx, context.Background()
	}

	searchIndex := resolver.specIndex
	if searchRef != nil && searchRef.Index != nil {
		searchIndex = searchRef.Index
	} else if sourceRef != nil && sourceRef.Index != nil {
		searchIndex = sourceRef.Index
	}

	ctx := context.Background()
	currentPath := ""
	if sourceRef != nil {
		currentPath = sourceRef.RemoteLocation
	}
	if currentPath == "" && searchIndex != nil {
		currentPath = searchIndex.specAbsolutePath
	}
	if currentPath != "" {
		ctx = context.WithValue(ctx, CurrentPathKey, currentPath)
	}
	if searchRef != nil || sourceRef != nil {
		base := ""
		if searchRef != nil && searchRef.SchemaIdBase != "" {
			base = searchRef.SchemaIdBase
		} else if sourceRef != nil && sourceRef.SchemaIdBase != "" {
			base = sourceRef.SchemaIdBase
		}
		if base != "" {
			scope := NewSchemaIdScope(base)
			scope.PushId(base)
			ctx = WithSchemaIdScope(ctx, scope)
		}
	}

	return searchIndex.SearchIndexForReferenceByReferenceWithContext(ctx, searchRef)
}

// VisitReference visits a single reference, collecting its relatives (dependencies) and recursively
// visiting them. The seen map prevents infinite loops, journey tracks the path for circular detection,
// and resolve controls whether nodes are actually resolved or just visited for analysis.
func (resolver *Resolver) VisitReference(ref *Reference, seen map[string]bool, journey []*Reference, resolve bool) []*yaml.Node {
	resolver.referencesVisited++
	if content, done := resolver.visitReferenceShortCircuit(ref, resolve); done {
		return content
	}

	journey = append(journey, ref)
	relatives := resolver.collectReferenceRelatives(ref, seen, journey, resolve)

	seen = make(map[string]bool)
	seen[ref.FullDefinition] = true
	resolver.visitReferenceRelatives(ref, relatives, seen, journey, resolve)

	ref.Seen = true
	if ref.Node != nil {
		return ref.Node.Content
	}
	return nil
}
