// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/utils"
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

// ExtractComponentsFromRefs returns located components from references. The returned nodes from here
// can be used for resolving as they contain the actual object properties.
//
// This function uses singleflight to deduplicate concurrent lookups for the same reference,
// channel-based collection to avoid mutex contention during resolution, and sorts results
// by input position for deterministic ordering.

// isExternalReference checks whether a Reference originated from an external $ref.
// ref.Definition may have been transformed (e.g., HTTP URL with fragment becomes "#/fragment"),
// so we also check the original raw ref value.
func isExternalReference(ref *Reference) bool {
	if ref == nil {
		return false
	}
	return utils.IsExternalRef(ref.Definition) || utils.IsExternalRef(ref.RawRef)
}

func (index *SpecIndex) ExtractComponentsFromRefs(ctx context.Context, refs []*Reference) []*Reference {
	if len(refs) == 0 {
		return nil
	}

	refsToCheck := refs
	mappedRefsInSequence := make([]*ReferenceMapped, len(refsToCheck))

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
				if index.config != nil && index.config.SkipExternalRefResolution && isExternalReference(ref) {
					continue
				}
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
		for _, rm := range mappedRefsInSequence {
			if rm != nil {
				index.allMappedRefsSequenced = append(index.allMappedRefsSequenced, rm)
			}
		}
		return found
	}

	var wg sync.WaitGroup
	var sfGroup singleflight.Group
	resultsChan := make(chan indexedRef, len(refsToCheck))

	maxConcurrency := runtime.GOMAXPROCS(0)
	if maxConcurrency < 4 {
		maxConcurrency = 4
	}
	sem := make(chan struct{}, maxConcurrency)

	for i, ref := range refsToCheck {
		i, ref := i, ref
		wg.Add(1)

		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			defer wg.Done()

			result, _, _ := sfGroup.Do(ref.FullDefinition, func() (interface{}, error) {
				index.refLock.RLock()
				if existing := index.allMappedRefs[ref.FullDefinition]; existing != nil {
					index.refLock.RUnlock()
					return existing, nil
				}
				index.refLock.RUnlock()

				return index.locateRef(ctx, ref), nil
			})

			located := result.(*Reference)
			if located != nil {
				resultsChan <- indexedRef{ref: located, pos: i}
			} else {
				resultsChan <- indexedRef{ref: nil, pos: i}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	collected := make([]indexedRef, 0, len(refsToCheck))
	for r := range resultsChan {
		collected = append(collected, r)
	}

	if !preserveLegacyRefOrder {
		sort.Slice(collected, func(i, j int) bool {
			return collected[i].pos < collected[j].pos
		})
	}

	found := make([]*Reference, 0, len(collected))

	for _, c := range collected {
		ref := refsToCheck[c.pos]
		located := c.ref

		if located == nil {
			index.refLock.RLock()
			located = index.allMappedRefs[ref.FullDefinition]
			index.refLock.RUnlock()
		}

		if located != nil {
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
			if index.config != nil && index.config.SkipExternalRefResolution && isExternalReference(ref) {
				continue
			}
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

	for _, rm := range mappedRefsInSequence {
		if rm != nil {
			index.allMappedRefsSequenced = append(index.allMappedRefsSequenced, rm)
		}
	}

	return found
}

func (index *SpecIndex) locateRef(ctx context.Context, ref *Reference) *Reference {
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
