// Copyright 2022 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// ResolvingError represents an issue the resolver had trying to stitch the tree together.
type ResolvingError struct {
	// ErrorRef is the error thrown by the resolver
	ErrorRef error

	// Node is the *yaml.Node reference that contains the resolving error
	Node *yaml.Node

	// Path is the shortened journey taken by the resolver
	Path string

	// CircularReference is set if the error is a reference to the circular reference.
	CircularReference *CircularReferenceResult
}

func (r *ResolvingError) Error() string {
	errs := utils.UnwrapErrors(r.ErrorRef)
	var msgs []string
	for _, e := range errs {
		var idxErr *IndexingError
		if errors.As(e, &idxErr) {
			msgs = append(msgs, fmt.Sprintf("%s: %s [%d:%d]", idxErr.Error(),
				idxErr.Path, idxErr.Node.Line, idxErr.Node.Column))
		} else {
			var l, c int
			if r.Node != nil {
				l = r.Node.Line
				c = r.Node.Column
			}
			msgs = append(msgs, fmt.Sprintf("%s: %s [%d:%d]", e.Error(),
				r.Path, l, c))
		}
	}
	return strings.Join(msgs, "\n")
}

// Resolver will use a *index.SpecIndex to stitch together a resolved root tree using all the discovered
// references in the doc.
type Resolver struct {
	specIndex              *SpecIndex
	resolvedRoot           *yaml.Node
	resolvingErrors        []*ResolvingError
	circularReferences     []*CircularReferenceResult
	ignoredPolyReferences  []*CircularReferenceResult
	ignoredArrayReferences []*CircularReferenceResult
	referencesVisited      int
	indexesVisited         int
	journeysTaken          int
	relativesSeen          int
	IgnorePoly             bool
	IgnoreArray            bool
	circChecked            bool
}

// NewResolver will create a new resolver from a *index.SpecIndex
func NewResolver(index *SpecIndex) *Resolver {
	if index == nil {
		return nil
	}
	r := &Resolver{
		specIndex:    index,
		resolvedRoot: index.GetRootNode(),
	}
	index.resolver = r
	return r
}

// GetIgnoredCircularPolyReferences returns all ignored circular references that are polymorphic
func (resolver *Resolver) GetIgnoredCircularPolyReferences() []*CircularReferenceResult {
	return resolver.ignoredPolyReferences
}

// GetIgnoredCircularArrayReferences returns all ignored circular references that are arrays
func (resolver *Resolver) GetIgnoredCircularArrayReferences() []*CircularReferenceResult {
	return resolver.ignoredArrayReferences
}

// GetResolvingErrors returns all errors found during resolving
func (resolver *Resolver) GetResolvingErrors() []*ResolvingError {
	return resolver.resolvingErrors
}

func (resolver *Resolver) GetCircularReferences() []*CircularReferenceResult {
	return resolver.GetSafeCircularReferences()
}

// GetSafeCircularReferences returns all circular reference errors found.
func (resolver *Resolver) GetSafeCircularReferences() []*CircularReferenceResult {
	var refs []*CircularReferenceResult
	for _, ref := range resolver.circularReferences {
		if !ref.IsInfiniteLoop {
			refs = append(refs, ref)
		}
	}
	return refs
}

// GetInfiniteCircularReferences returns all circular reference errors found that are infinite / unrecoverable
func (resolver *Resolver) GetInfiniteCircularReferences() []*CircularReferenceResult {
	var refs []*CircularReferenceResult
	for _, ref := range resolver.circularReferences {
		if ref.IsInfiniteLoop {
			refs = append(refs, ref)
		}
	}
	return refs
}

// GetPolymorphicCircularErrors returns all circular errors that stem from polymorphism
func (resolver *Resolver) GetPolymorphicCircularErrors() []*CircularReferenceResult {
	var res []*CircularReferenceResult
	for i := range resolver.circularReferences {
		if !resolver.circularReferences[i].IsInfiniteLoop {
			continue
		}
		if !resolver.circularReferences[i].IsPolymorphicResult {
			continue
		}
		res = append(res, resolver.circularReferences[i])
	}
	return res
}

// GetNonPolymorphicCircularErrors returns all circular errors that DO NOT stem from polymorphism
func (resolver *Resolver) GetNonPolymorphicCircularErrors() []*CircularReferenceResult {
	var res []*CircularReferenceResult
	for i := range resolver.circularReferences {
		if !resolver.circularReferences[i].IsInfiniteLoop {
			continue
		}

		if !resolver.circularReferences[i].IsPolymorphicResult {
			res = append(res, resolver.circularReferences[i])
		}
	}
	return res
}

// IgnorePolymorphicCircularReferences will ignore any circular references that are polymorphic (oneOf, anyOf, allOf)
// This must be set before any resolving is done.
func (resolver *Resolver) IgnorePolymorphicCircularReferences() {
	resolver.IgnorePoly = true
}

// IgnoreArrayCircularReferences will ignore any circular references that stem from arrays. This must be set before
// any resolving is done.
func (resolver *Resolver) IgnoreArrayCircularReferences() {
	resolver.IgnoreArray = true
}

// GetJourneysTaken returns the number of journeys taken by the resolver
func (resolver *Resolver) GetJourneysTaken() int {
	return resolver.journeysTaken
}

// GetReferenceVisited returns the number of references visited by the resolver
func (resolver *Resolver) GetReferenceVisited() int {
	return resolver.referencesVisited
}

// GetIndexesVisited returns the number of indexes visited by the resolver
func (resolver *Resolver) GetIndexesVisited() int {
	return resolver.indexesVisited
}

// GetRelativesSeen returns the number of siblings (nodes at the same level) seen for each reference found.
func (resolver *Resolver) GetRelativesSeen() int {
	return resolver.relativesSeen
}

// Resolve will resolve the specification, everything that is not polymorphic and not circular, will be resolved.
// this data can get big, it results in a massive duplication of data. This is a destructive method and will permanently
// re-organize the node tree. Make sure you have copied your original tree before running this (if you want to preserve
// original data)
func (resolver *Resolver) Resolve() []*ResolvingError {
	visitIndex(resolver, resolver.specIndex)

	for _, circRef := range resolver.circularReferences {
		// If the circular reference is not required, we can ignore it, as it's a terminable loop rather than an infinite one
		if !circRef.IsInfiniteLoop {
			continue
		}

		if !resolver.circChecked {
			resolver.resolvingErrors = append(resolver.resolvingErrors, &ResolvingError{
				ErrorRef:          fmt.Errorf("infinite circular reference detected: %s", circRef.Start.Definition),
				Node:              circRef.ParentNode,
				Path:              circRef.GenerateJourneyPath(),
				CircularReference: circRef,
			})
		}
	}
	resolver.specIndex.SetCircularReferences(resolver.circularReferences)
	resolver.specIndex.SetIgnoredArrayCircularReferences(resolver.ignoredArrayReferences)
	resolver.specIndex.SetIgnoredPolymorphicCircularReferences(resolver.ignoredPolyReferences)
	resolver.circChecked = true
	return resolver.resolvingErrors
}

// CheckForCircularReferences Check for circular references, without resolving, a non-destructive run.
func (resolver *Resolver) CheckForCircularReferences() []*ResolvingError {
	visitIndexWithoutDamagingIt(resolver, resolver.specIndex)
	for _, circRef := range resolver.circularReferences {
		// If the circular reference is not required, we can ignore it, as it's a terminable loop rather than an infinite one
		if !circRef.IsInfiniteLoop {
			continue
		}
		if !resolver.circChecked {
			resolver.resolvingErrors = append(resolver.resolvingErrors, &ResolvingError{
				ErrorRef:          fmt.Errorf("infinite circular reference detected: %s", circRef.Start.Name),
				Node:              circRef.ParentNode,
				Path:              circRef.GenerateJourneyPath(),
				CircularReference: circRef,
			})
		}
	}
	// update our index with any circular refs we found.
	resolver.specIndex.SetCircularReferences(resolver.circularReferences)
	resolver.specIndex.SetIgnoredArrayCircularReferences(resolver.ignoredArrayReferences)
	resolver.specIndex.SetIgnoredPolymorphicCircularReferences(resolver.ignoredPolyReferences)
	resolver.circChecked = true
	return resolver.resolvingErrors
}

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

	// Sort schema keys for deterministic iteration order
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
				// make a note of the reference and map the original ref after we're done
				if ok, _, _ := utils.IsNodeRefValue(ref.OriginalReference.Node); ok {
					refs = append(refs, refMap{
						ref:   ref.OriginalReference,
						nodes: n,
					})
				}
			}
		}
	}
	idx.pendingResolve = refs

	// Sort schema keys for deterministic iteration order
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

	// Sort security scheme keys for deterministic iteration order
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

	// map everything
	for _, sequenced := range idx.GetAllSequencedReferences() {
		locatedDef := mappedIndex[sequenced.FullDefinition]
		if locatedDef != nil {
			if !locatedDef.Circular {
				sequenced.Node.Content = locatedDef.Node.Content
			}
		}
	}
}

// searchReferenceWithContext resolves a reference using document context when enabled in the config.
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

// VisitReference will visit a reference as part of a journey and will return resolved nodes.
func (resolver *Resolver) VisitReference(ref *Reference, seen map[string]bool, journey []*Reference, resolve bool) []*yaml.Node {
	resolver.referencesVisited++
	if resolve && ref != nil && ref.Seen {
		if ref.Resolved {
			return ref.Node.Content
		}
	}
	if !resolve && ref != nil && ref.Seen {
		return ref.Node.Content
	}
	if ref != nil {
		journey = append(journey, ref)
		seenRelatives := make(map[int]bool)
		base := resolver.resolveSchemaIdBase(ref.SchemaIdBase, ref.Node)
		relatives := resolver.extractRelatives(ref, ref.Node, nil, seen, journey, seenRelatives, resolve, 0, base)

		seen = make(map[string]bool)

		seen[ref.FullDefinition] = true
		for _, r := range relatives {
			// check if we have seen this on the journey before, if so! it's circular
			skip := false
			for i, j := range journey {
				if j.FullDefinition == r.FullDefinition {

					var foundDup *Reference
					foundRef, _, _ := resolver.searchReferenceWithContext(ref, r)
					if foundRef != nil {
						foundDup = foundRef
					}

					var circRef *CircularReferenceResult
					if !foundDup.Circular {
						loop := append(journey, foundDup)

						visitedDefinitions := make(map[string]bool)
						isInfiniteLoop, _ := resolver.isInfiniteCircularDependency(foundDup,
							visitedDefinitions, nil)

						isArray := false
						if r.ParentNodeSchemaType == "array" || slices.Contains(r.ParentNodeTypes, "array") {
							isArray = true
						}
						circRef = &CircularReferenceResult{
							ParentNode:     foundDup.ParentNode,
							Journey:        loop,
							Start:          foundDup,
							LoopIndex:      i,
							LoopPoint:      foundDup,
							IsArrayResult:  isArray,
							IsInfiniteLoop: isInfiniteLoop,
						}

						if resolver.IgnorePoly && !isArray {
							resolver.ignoredPolyReferences = append(resolver.ignoredPolyReferences, circRef)
						} else if resolver.IgnoreArray && isArray {
							resolver.ignoredArrayReferences = append(resolver.ignoredArrayReferences, circRef)
						} else {
							if !resolver.circChecked {
								resolver.circularReferences = append(resolver.circularReferences, circRef)
							}
						}
						r.Seen = true
						r.Circular = true
						foundDup.Seen = true
						foundDup.Circular = true
					}
					skip = true
				}
			}

			if !skip {
				var original *Reference
				foundRef, _, _ := resolver.searchReferenceWithContext(ref, r)
				if foundRef != nil {
					original = foundRef
				}
				resolved := resolver.VisitReference(original, seen, journey, resolve)
				if resolve && !original.Circular {
					ref.Resolved = true
					r.Resolved = true
					r.Node.Content = resolved // this is where we perform the actual resolving.
				}
				r.Seen = true
				ref.Seen = true
			}
		}

		ref.Seen = true

		if ref.Node != nil {
			return ref.Node.Content
		}
	}
	return nil
}

func (resolver *Resolver) isInfiniteCircularDependency(ref *Reference, visitedDefinitions map[string]bool,
	initialRef *Reference,
) (bool, map[string]bool) {
	if ref == nil {
		return false, visitedDefinitions
	}
	for refDefinition := range ref.RequiredRefProperties {
		r, _ := resolver.specIndex.SearchIndexForReference(refDefinition)
		if initialRef != nil && initialRef.FullDefinition == r.FullDefinition {
			return true, visitedDefinitions
		}
		if len(visitedDefinitions) > 0 && ref.FullDefinition == r.FullDefinition {
			return true, visitedDefinitions
		}

		if visitedDefinitions[r.FullDefinition] {
			continue
		}

		visitedDefinitions[r.FullDefinition] = true

		ir := initialRef
		if ir == nil {
			ir = ref
		}

		var isChildICD bool

		isChildICD, visitedDefinitions = resolver.isInfiniteCircularDependency(r, visitedDefinitions, ir)
		if isChildICD {
			return true, visitedDefinitions
		}
	}

	return false, visitedDefinitions
}

func (resolver *Resolver) extractRelatives(ref *Reference, node, parent *yaml.Node,
	foundRelatives map[string]bool,
	journey []*Reference, seen map[int]bool, resolve bool, depth int, schemaIdBase string,
) []*Reference {
	if len(journey) > 100 {
		return nil
	}

	// this is a safety check to prevent a stack overflow.
	if depth > 500 {
		def := "unknown"
		if ref != nil {
			def = ref.FullDefinition
		}
		if resolver.specIndex != nil && resolver.specIndex.logger != nil {
			resolver.specIndex.logger.Warn("libopenapi resolver: relative depth exceeded 100 levels, "+
				"check for circular references - resolving may be incomplete",
				"reference", def)
		}

		loop := append(journey, ref)
		circRef := &CircularReferenceResult{
			Journey:        loop,
			Start:          ref,
			LoopIndex:      depth,
			LoopPoint:      ref,
			IsInfiniteLoop: true,
		}
		if !resolver.circChecked {
			resolver.circularReferences = append(resolver.circularReferences, circRef)
			ref.Circular = true
		}
		return nil
	}

	currentBase := resolver.resolveSchemaIdBase(schemaIdBase, node)
	var found []*Reference

	if node != nil && len(node.Content) > 0 {
		skip := false
		for i, n := range node.Content {
			if skip {
				skip = false
				continue
			}
			if utils.IsNodeMap(n) || utils.IsNodeArray(n) {
				depth++

				var foundRef *Reference
				foundRef, _, _ = resolver.searchReferenceWithContext(ref, ref)
				if foundRef != nil && !foundRef.Circular {
					found = append(found, resolver.extractRelatives(foundRef, n, node, foundRelatives, journey, seen, resolve, depth, currentBase)...)
					depth--
				}
				if foundRef == nil {
					found = append(found, resolver.extractRelatives(ref, n, node, foundRelatives, journey, seen, resolve, depth, currentBase)...)
					depth--
				}

			}

			if i%2 == 0 && n.Value == "$ref" && len(node.Content) > i%2+1 {

				if !utils.IsNodeStringValue(node.Content[i+1]) {
					continue
				}
				// issue #481 cannot look at an array value, the next not is not the value!
				if utils.IsNodeArray(node) {
					continue
				}

				value := node.Content[i+1].Value
				value = strings.ReplaceAll(value, "\\\\", "\\")

				// If SkipExternalRefResolution is enabled, skip external refs entirely
				if resolver.specIndex != nil && resolver.specIndex.config != nil &&
					resolver.specIndex.config.SkipExternalRefResolution && utils.IsExternalRef(value) {
					skip = true
					continue
				}

				var locatedRef *Reference
				var fullDef string
				var definition string

				// explode value
				exp := strings.Split(value, "#/")
				if len(exp) == 2 {
					definition = fmt.Sprintf("#/%s", exp[1])
					if exp[0] != "" {
						if strings.HasPrefix(exp[0], "http") {
							fullDef = value
						} else {
							if strings.HasPrefix(ref.FullDefinition, "http") {

								// split the http URI into parts
								httpExp := strings.Split(ref.FullDefinition, "#/")

								u, _ := url.Parse(httpExp[0])
								abs, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), exp[0], string(filepath.Separator)))
								u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
								u.Fragment = ""
								fullDef = fmt.Sprintf("%s#/%s", u.String(), exp[1])

							} else {

								// split the referring ref full def into parts
								fileDef := strings.Split(ref.FullDefinition, "#/")

								// extract the location of the ref and build a full def path.
								abs := resolver.resolveLocalRefPath(filepath.Dir(fileDef[0]), exp[0])
								// abs = utils.ReplaceWindowsDriveWithLinuxPath(abs)
								fullDef = fmt.Sprintf("%s#/%s", abs, exp[1])

							}
						}
					} else {
						// local component, full def is based on passed in ref
						baseLocation := ref.FullDefinition
						if ref.RemoteLocation != "" {
							baseLocation = ref.RemoteLocation
						}
						if strings.HasPrefix(baseLocation, "http") {

							// split the http URI into parts
							httpExp := strings.Split(baseLocation, "#/")

							// parse a URL from the full def
							u, _ := url.Parse(httpExp[0])

							// extract the location of the ref and build a full def path.
							fullDef = fmt.Sprintf("%s#/%s", u.String(), exp[1])

						} else {
							// split the full def into parts
							fileDef := strings.Split(baseLocation, "#/")
							fullDef = fmt.Sprintf("%s#/%s", fileDef[0], exp[1])
						}
					}
				} else {

					definition = value

					// if the reference is a http link
					if strings.HasPrefix(value, "http") {
						fullDef = value
					} else {

						// split the full def into parts
						baseLocation := ref.FullDefinition
						if ref.RemoteLocation != "" {
							baseLocation = ref.RemoteLocation
						}
						fileDef := strings.Split(baseLocation, "#/")

						// is the file def a http link?
						if strings.HasPrefix(fileDef[0], "http") {
							u, _ := url.Parse(fileDef[0])
							absPath, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), exp[0], string(filepath.Separator)))
							u.Path = utils.ReplaceWindowsDriveWithLinuxPath(absPath)
							fullDef = u.String()

						} else {
							fullDef = resolver.resolveLocalRefPath(filepath.Dir(fileDef[0]), exp[0])
						}

					}
				}

				if currentBase != "" {
					fullDef = resolveRefWithSchemaBase(value, currentBase)
				}

				searchRef := &Reference{
					Definition:     definition,
					FullDefinition: fullDef,
					RawRef:         value,
					SchemaIdBase:   currentBase,
					RemoteLocation: ref.RemoteLocation,
					IsRemote:       true,
					Index:          ref.Index,
				}

				locatedRef, _, _ = resolver.searchReferenceWithContext(ref, searchRef)

				if locatedRef == nil {
					_, path := utils.ConvertComponentIdIntoFriendlyPathSearch(value)
					err := &ResolvingError{
						ErrorRef: fmt.Errorf("cannot resolve reference `%s`, it's missing", value),
						Node:     n,
						Path:     path,
					}
					resolver.resolvingErrors = append(resolver.resolvingErrors, err)
					continue
				}

				if resolve {
					// if this is a reference also, we want to resolve it.
					if ok, _, _ := utils.IsNodeRefValue(ref.Node); ok {
						ref.Node.Content = locatedRef.Node.Content
						ref.Resolved = true
					}
				}

				schemaType := ""
				if parent != nil {
					_, arrayTypevn := utils.FindKeyNodeTop("type", parent.Content)
					if arrayTypevn != nil {
						if arrayTypevn.Value == "array" {
							schemaType = "array"
						}
					}
				}
				if ref.ParentNodeSchemaType != "" {
					locatedRef.ParentNodeTypes = append(locatedRef.ParentNodeTypes, ref.ParentNodeSchemaType)
				}
				locatedRef.ParentNodeSchemaType = schemaType
				found = append(found, locatedRef)
				foundRelatives[value] = true
			}

			if i%2 == 0 && n.Value != "$ref" && n.Value != "" {
				// Check if we're inside a properties object
				isInsideProperties := false
				if parent != nil {
					for j := 0; j < len(parent.Content); j += 2 {
						if j < len(parent.Content) && parent.Content[j].Value == "properties" {
							isInsideProperties = true
							break
						}
					}
				}

				// Only treat as polymorphic keywords if not inside properties
				if !isInsideProperties && (n.Value == "allOf" || n.Value == "oneOf" || n.Value == "anyOf") {

					// if this is a polymorphic link, we want to follow it and see if it becomes circular
					if i+1 < len(node.Content) && utils.IsNodeMap(node.Content[i+1]) { // check for nested items
						// check if items is present, to indicate an array
						if k, v := utils.FindKeyNodeTop("items", node.Content[i+1].Content); v != nil {
							if utils.IsNodeMap(v) {
								if d, _, l := utils.IsNodeRefValue(v); d {

									// create full definition lookup based on ref.
									def := resolver.buildDefPathWithSchemaBase(ref, l, currentBase)

									mappedRefs, _ := resolver.specIndex.SearchIndexForReference(def)
									if mappedRefs != nil && !mappedRefs.Circular {
										circ := false
										for f := range journey {
											if journey[f].FullDefinition == mappedRefs.FullDefinition {
												circ = true
												break
											}
										}
										if !circ {
											resolver.VisitReference(mappedRefs, foundRelatives, journey, resolve)
										} else {
											loop := append(journey, mappedRefs)
											circRef := &CircularReferenceResult{
												ParentNode:          k,
												Journey:             loop,
												Start:               mappedRefs,
												LoopIndex:           i,
												LoopPoint:           mappedRefs,
												PolymorphicType:     n.Value,
												IsPolymorphicResult: true,
											}

											mappedRefs.Seen = true
											mappedRefs.Circular = true
											if resolver.IgnorePoly {
												resolver.ignoredPolyReferences = append(resolver.ignoredPolyReferences, circRef)
											} else {
												if !resolver.circChecked {
													resolver.circularReferences = append(resolver.circularReferences, circRef)
												}
											}
										}
									}
								}
							}
						} else {
							// no items discovered, continue on and investigate anyway.
							v := node.Content[i+1]
							if utils.IsNodeMap(v) {
								if d, _, l := utils.IsNodeRefValue(v); d {

									// create full definition lookup based on ref.
									def := resolver.buildDefPathWithSchemaBase(ref, l, currentBase)

									mappedRefs, _ := resolver.specIndex.SearchIndexForReference(def)
									if mappedRefs != nil && !mappedRefs.Circular {
										circ := false
										for f := range journey {
											if journey[f].FullDefinition == mappedRefs.FullDefinition {
												circ = true
												break
											}
										}
										if !circ {
											resolver.VisitReference(mappedRefs, foundRelatives, journey, resolve)
										} else {
											loop := append(journey, mappedRefs)
											circRef := &CircularReferenceResult{
												ParentNode:          node.Content[i],
												Journey:             loop,
												Start:               mappedRefs,
												LoopIndex:           i,
												LoopPoint:           mappedRefs,
												PolymorphicType:     n.Value,
												IsPolymorphicResult: true,
											}

											mappedRefs.Seen = true
											mappedRefs.Circular = true
											if resolver.IgnorePoly {
												resolver.ignoredPolyReferences = append(resolver.ignoredPolyReferences, circRef)
											} else {
												if !resolver.circChecked {
													resolver.circularReferences = append(resolver.circularReferences, circRef)
												}
											}
										}
									}
								}
							}
						}
					}
					// for array based polymorphic items
					if i+1 < len(node.Content) && utils.IsNodeArray(node.Content[i+1]) { // check for nested items
						for q := range node.Content[i+1].Content {
							v := node.Content[i+1].Content[q]
							if utils.IsNodeMap(v) {
								if d, _, l := utils.IsNodeRefValue(v); d {
									def := resolver.buildDefPathWithSchemaBase(ref, l, currentBase)
									mappedRefs, _ := resolver.specIndex.SearchIndexForReference(def)
									if mappedRefs != nil && !mappedRefs.Circular {
										circ := false
										for f := range journey {
											if journey[f].FullDefinition == mappedRefs.FullDefinition {
												circ = true
												break
											}
										}
										if !circ {
											resolver.VisitReference(mappedRefs, foundRelatives, journey, resolve)
										} else {
											loop := append(journey, mappedRefs)

											circRef := &CircularReferenceResult{
												ParentNode:          node.Content[i],
												Journey:             loop,
												Start:               mappedRefs,
												LoopIndex:           i,
												LoopPoint:           mappedRefs,
												PolymorphicType:     n.Value,
												IsPolymorphicResult: true,
											}

											mappedRefs.Seen = true
											mappedRefs.Circular = true
											if resolver.IgnorePoly {
												resolver.ignoredPolyReferences = append(resolver.ignoredPolyReferences, circRef)
											} else {
												if !resolver.circChecked {
													resolver.circularReferences = append(resolver.circularReferences, circRef)
												}
											}
										}
									}
								} else {
									depth++
									found = append(found, resolver.extractRelatives(ref, v, n,
										foundRelatives, journey, seen, resolve, depth, currentBase)...)
								}
							}
						}
					}
					skip = true
					continue
				}
			}
		}
	}
	resolver.relativesSeen += len(found)
	return found
}

func (resolver *Resolver) buildDefPath(ref *Reference, l string) string {
	def := ""
	exp := strings.Split(l, "#/")
	if len(exp) == 2 {
		if exp[0] != "" {
			if !strings.HasPrefix(exp[0], "http") {
				if !filepath.IsAbs(exp[0]) {
					if strings.HasPrefix(ref.FullDefinition, "http") {

						u, _ := url.Parse(ref.FullDefinition)
						p, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), exp[0], string(filepath.Separator)))
						u.Path = utils.ReplaceWindowsDriveWithLinuxPath(p)
						def = fmt.Sprintf("%s#/%s", u.String(), exp[1])

					} else {
						z := strings.Split(ref.FullDefinition, "#/")
						if len(z) == 2 {
							if len(z[0]) > 0 {
								abs := resolver.resolveLocalRefPath(filepath.Dir(z[0]), exp[0])
								def = fmt.Sprintf("%s#/%s", abs, exp[1])
							} else {
								abs, _ := filepath.Abs(exp[0])
								def = fmt.Sprintf("%s#/%s", abs, exp[1])
							}
						} else {
							abs := resolver.resolveLocalRefPath(filepath.Dir(ref.FullDefinition), exp[0])
							def = fmt.Sprintf("%s#/%s", abs, exp[1])
						}
					}
				}
			} else {
				if len(exp[1]) > 0 {
					def = l
				} else {
					def = exp[0]
				}
			}
		} else {
			if strings.HasPrefix(ref.FullDefinition, "http") {
				u, _ := url.Parse(ref.FullDefinition)
				u.Fragment = ""
				def = fmt.Sprintf("%s#/%s", u.String(), exp[1])

			} else {
				if strings.HasPrefix(ref.FullDefinition, "#/") {
					def = fmt.Sprintf("#/%s", exp[1])
				} else {
					fdexp := strings.Split(ref.FullDefinition, "#/")
					def = fmt.Sprintf("%s#/%s", fdexp[0], exp[1])
				}
			}
		}
	} else {
		if strings.HasPrefix(l, "http") {
			def = l
		} else {
			// check if were dealing with a remote file
			if strings.HasPrefix(ref.FullDefinition, "http") {

				// split the url.
				u, _ := url.Parse(ref.FullDefinition)
				abs, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), l, string(filepath.Separator)))
				u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
				u.Fragment = ""
				def = u.String()
			} else {
				lookupRef := strings.Split(ref.FullDefinition, "#/")
				abs := resolver.resolveLocalRefPath(filepath.Dir(lookupRef[0]), l)
				def = abs
			}
		}
	}

	return def
}

func (resolver *Resolver) resolveLocalRefPath(base, ref string) string {
	if resolver != nil && resolver.specIndex != nil {
		return resolver.specIndex.ResolveRelativeFilePath(base, ref)
	}
	abs, _ := filepath.Abs(utils.CheckPathOverlap(base, ref, string(filepath.Separator)))
	return abs
}

func (resolver *Resolver) buildDefPathWithSchemaBase(ref *Reference, l string, schemaIdBase string) string {
	if schemaIdBase != "" {
		normalized := resolveRefWithSchemaBase(l, schemaIdBase)
		if normalized != l {
			return normalized
		}
	}
	return resolver.buildDefPath(ref, l)
}

func (resolver *Resolver) resolveSchemaIdBase(parentBase string, node *yaml.Node) string {
	if node == nil {
		return parentBase
	}
	idValue := FindSchemaIdInNode(node)
	if idValue == "" {
		return parentBase
	}
	base := parentBase
	if base == "" && resolver.specIndex != nil {
		base = resolver.specIndex.specAbsolutePath
	}
	resolved, err := ResolveSchemaId(idValue, base)
	if err != nil || resolved == "" {
		return idValue
	}
	return resolved
}

func (resolver *Resolver) ResolvePendingNodes() {
	// map everything afterwards
	for _, r := range resolver.specIndex.pendingResolve {
		// r.Node.Content = refs[r].nodes
		r.ref.Node.Content = r.nodes
	}
}
