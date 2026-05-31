// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// ResolvingError represents an issue the resolver had trying to stitch the tree together.
type ResolvingError struct {
	ErrorRef error
	Node     *yaml.Node
	Path     string

	// CircularReference is the detected circular reference result, if this error relates to one.
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
			msgs = append(msgs, fmt.Sprintf("%s: %s [%d:%d]", e.Error(), r.Path, l, c))
		}
	}
	return strings.Join(msgs, "\n")
}

// Resolver uses a SpecIndex to stitch together a resolved root tree from all discovered references,
// detecting circular references and resolving polymorphic relationships along the way.
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

func (resolver *Resolver) Release() {
	if resolver == nil {
		return
	}
	resolver.specIndex = nil
	resolver.resolvedRoot = nil
	resolver.resolvingErrors = nil
	resolver.circularReferences = nil
	resolver.ignoredPolyReferences = nil
	resolver.ignoredArrayReferences = nil
}

func NewResolver(index *SpecIndex) *Resolver {
	if index == nil {
		return nil
	}
	r := &Resolver{
		specIndex:    index,
		resolvedRoot: index.GetRootNode(),
	}
	index.SetResolver(r)
	return r
}

func (resolver *Resolver) GetIgnoredCircularPolyReferences() []*CircularReferenceResult {
	return resolver.ignoredPolyReferences
}

func (resolver *Resolver) GetIgnoredCircularArrayReferences() []*CircularReferenceResult {
	return resolver.ignoredArrayReferences
}

func (resolver *Resolver) GetResolvingErrors() []*ResolvingError {
	return resolver.resolvingErrors
}

func (resolver *Resolver) GetCircularReferences() []*CircularReferenceResult {
	return resolver.GetSafeCircularReferences()
}

func (resolver *Resolver) GetSafeCircularReferences() []*CircularReferenceResult {
	var refs []*CircularReferenceResult
	for _, ref := range resolver.circularReferences {
		if !ref.IsInfiniteLoop {
			refs = append(refs, ref)
		}
	}
	return refs
}

func (resolver *Resolver) GetInfiniteCircularReferences() []*CircularReferenceResult {
	var refs []*CircularReferenceResult
	for _, ref := range resolver.circularReferences {
		if ref.IsInfiniteLoop {
			refs = append(refs, ref)
		}
	}
	return refs
}

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

func (resolver *Resolver) IgnorePolymorphicCircularReferences() {
	resolver.IgnorePoly = true
}

func (resolver *Resolver) IgnoreArrayCircularReferences() {
	resolver.IgnoreArray = true
}

func (resolver *Resolver) GetJourneysTaken() int {
	return resolver.journeysTaken
}

func (resolver *Resolver) GetReferenceVisited() int {
	return resolver.referencesVisited
}

func (resolver *Resolver) GetIndexesVisited() int {
	return resolver.indexesVisited
}

func (resolver *Resolver) GetRelativesSeen() int {
	return resolver.relativesSeen
}

func (resolver *Resolver) Resolve() []*ResolvingError {
	visitIndex(resolver, resolver.specIndex)
	for _, circRef := range resolver.circularReferences {
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

// CheckForCircularReferences walks all references without resolving them, detecting circular
// reference chains. Returns any resolving errors found, including infinite circular loops.
func (resolver *Resolver) CheckForCircularReferences() []*ResolvingError {
	visitIndexWithoutDamagingIt(resolver, resolver.specIndex)
	for _, circRef := range resolver.circularReferences {
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
	resolver.specIndex.SetCircularReferences(resolver.circularReferences)
	resolver.specIndex.SetIgnoredArrayCircularReferences(resolver.ignoredArrayReferences)
	resolver.specIndex.SetIgnoredPolymorphicCircularReferences(resolver.ignoredPolyReferences)
	resolver.circChecked = true
	return resolver.resolvingErrors
}
