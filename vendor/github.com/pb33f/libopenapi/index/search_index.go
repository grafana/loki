// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

type ContextKey string

const (
	CurrentPathKey   ContextKey = "currentPath"
	FoundIndexKey    ContextKey = "foundIndex"
	RootIndexKey     ContextKey = "currentIndex"
	IndexingFilesKey ContextKey = "indexingFiles" // Tracks files being indexed in current call chain
)

// GetIndexingFiles returns the set of files currently being indexed in the call chain.
// Returns nil if not set.
func GetIndexingFiles(ctx context.Context) map[string]bool {
	if v := ctx.Value(IndexingFilesKey); v != nil {
		return v.(map[string]bool)
	}
	return nil
}

// AddIndexingFile adds a file to the indexing set in the context.
// Returns a new context with the updated set.
func AddIndexingFile(ctx context.Context, filePath string) context.Context {
	existing := GetIndexingFiles(ctx)
	newSet := make(map[string]bool)
	for k, v := range existing {
		newSet[k] = v
	}
	newSet[filePath] = true
	return context.WithValue(ctx, IndexingFilesKey, newSet)
}

// IsFileBeingIndexed checks if a file is currently being indexed in the call chain.
// For HTTP URLs, it also checks if the PATH portion matches any indexed file,
// since the same file might be referenced with different hostnames (which get normalized
// to a common server).
func IsFileBeingIndexed(ctx context.Context, filePath string) bool {
	files := GetIndexingFiles(ctx)
	if files == nil {
		return false
	}
	// Direct match
	if files[filePath] {
		return true
	}
	// For HTTP URLs, also check if the path matches any indexed file's path
	if strings.HasPrefix(filePath, "http") {
		if u, err := url.Parse(filePath); err == nil {
			// Check if the path portion matches any indexed file
			for indexedFile := range files {
				if strings.HasPrefix(indexedFile, "http") {
					if indexedU, err2 := url.Parse(indexedFile); err2 == nil {
						// Compare paths (the filename portion)
						if u.Path == indexedU.Path || filepath.Base(u.Path) == filepath.Base(indexedU.Path) {
							return true
						}
					}
				} else {
					// Compare with non-HTTP paths (just the filename)
					if filepath.Base(u.Path) == filepath.Base(indexedFile) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (index *SpecIndex) SearchIndexForReferenceByReference(fullRef *Reference) (*Reference, *SpecIndex) {
	r, idx, _ := index.SearchIndexForReferenceByReferenceWithContext(context.Background(), fullRef)
	return r, idx
}

// SearchIndexForReference searches the index for a reference, first looking through the mapped references
// and then externalSpecIndex for a match. If no match is found, it will recursively search the child indexes
// extracted when parsing the OpenAPI Spec.
func (index *SpecIndex) SearchIndexForReference(ref string) (*Reference, *SpecIndex) {
	return index.SearchIndexForReferenceByReference(&Reference{FullDefinition: ref})
}

func (index *SpecIndex) SearchIndexForReferenceWithContext(ctx context.Context, ref string) (*Reference, *SpecIndex, context.Context) {
	return index.SearchIndexForReferenceByReferenceWithContext(ctx, &Reference{FullDefinition: ref})
}

func (index *SpecIndex) SearchIndexForReferenceByReferenceWithContext(ctx context.Context, searchRef *Reference) (*Reference, *SpecIndex, context.Context) {
	if index.cache != nil {
		if v, ok := index.cache.Load(searchRef.FullDefinition); ok {
			idx := index.extractIndex(v.(*Reference))
			return v.(*Reference), idx, context.WithValue(ctx, CurrentPathKey, v.(*Reference).RemoteLocation)
		}
	}

	schemaIdBase := searchRef.SchemaIdBase
	if schemaIdBase == "" {
		if scope := GetSchemaIdScope(ctx); scope != nil && scope.BaseUri != "" {
			schemaIdBase = scope.BaseUri
		}
	}

	rawRef := searchRef.FullDefinition
	if searchRef.RawRef != "" && schemaIdBase != "" {
		rawRef = searchRef.RawRef
	}
	normalizedRef := resolveRefWithSchemaBase(rawRef, schemaIdBase)

	if index.cache != nil && normalizedRef != searchRef.FullDefinition {
		if v, ok := index.cache.Load(normalizedRef); ok {
			idx := index.extractIndex(v.(*Reference))
			return v.(*Reference), idx, context.WithValue(ctx, CurrentPathKey, v.(*Reference).RemoteLocation)
		}
	}

	// Try to resolve via JSON Schema 2020-12 $id registry first
	// This handles refs like "a.json" resolving to schemas with $id: "https://example.com/a.json"
	if resolved := index.ResolveRefViaSchemaId(normalizedRef); resolved != nil {
		if index.cache != nil {
			index.cache.Store(searchRef.FullDefinition, resolved)
			if normalizedRef != searchRef.FullDefinition {
				index.cache.Store(normalizedRef, resolved)
			}
		}
		return resolved, resolved.Index, context.WithValue(ctx, CurrentPathKey, resolved.RemoteLocation)
	}
	pathRef := ""
	if strings.HasPrefix(normalizedRef, "/") {
		pathRef = normalizedRef
	} else if strings.HasPrefix(rawRef, "/") {
		pathRef = rawRef
	}
	if pathRef != "" {
		if resolved := index.resolveRefViaSchemaIdPath(pathRef); resolved != nil {
			if index.cache != nil {
				index.cache.Store(searchRef.FullDefinition, resolved)
				if normalizedRef != searchRef.FullDefinition {
					index.cache.Store(normalizedRef, resolved)
				}
			}
			return resolved, resolved.Index, context.WithValue(ctx, CurrentPathKey, resolved.RemoteLocation)
		}
	}

	ref := normalizedRef
	refAlt := ref
	absPath := index.specAbsolutePath
	if searchRef.RemoteLocation != "" {
		absPath = searchRef.RemoteLocation
	}
	if absPath == "" && index.config != nil {
		absPath = index.config.BasePath
	}
	var roloLookup string
	uri := strings.Split(ref, "#/")
	if len(uri) == 2 {
		if uri[0] != "" {
			if strings.HasPrefix(uri[0], "http") {
				roloLookup = searchRef.FullDefinition
			} else {
				if filepath.IsAbs(uri[0]) {
					roloLookup = uri[0]
				} else {
					if filepath.Ext(absPath) != "" {
						absPath = filepath.Dir(absPath)
					}
					roloLookup = index.resolveRelativeFilePath(absPath, uri[0])
				}
			}
		} else {

			if filepath.Ext(uri[1]) != "" {
				roloLookup = absPath
			} else {
				roloLookup = ""
			}

			ref = fmt.Sprintf("#/%s", uri[1])
			refAlt = fmt.Sprintf("%s#/%s", absPath, uri[1])

		}
	} else {
		if filepath.IsAbs(uri[0]) {
			roloLookup = uri[0]
		} else {
			if strings.HasPrefix(uri[0], "http") {
				roloLookup = ref
			} else {
				if filepath.Ext(absPath) != "" {
					absPath = filepath.Dir(absPath)
				}
				roloLookup = index.resolveRelativeFilePath(absPath, uri[0])
			}
		}
		ref = uri[0]
	}
	if strings.Contains(ref, "%") {
		// decode the url.
		ref, _ = url.QueryUnescape(ref)
		refAlt, _ = url.QueryUnescape(refAlt)
	}

	if r, ok := index.allMappedRefs[ref]; ok {
		idx := index.extractIndex(r)
		index.cache.Store(ref, r)
		return r, idx, context.WithValue(ctx, CurrentPathKey, r.RemoteLocation)
	}

	if r, ok := index.allMappedRefs[refAlt]; ok {
		idx := index.extractIndex(r)
		idx.cache.Store(refAlt, r)
		return r, idx, context.WithValue(ctx, CurrentPathKey, r.RemoteLocation)
	}

	if r, ok := index.allComponentSchemaDefinitions.Load(refAlt); ok {
		rf := r.(*Reference)
		idx := index.extractIndex(rf)
		index.cache.Store(refAlt, r)
		return rf, idx, context.WithValue(ctx, CurrentPathKey, rf.RemoteLocation)
	}

	// check security schemes
	if index.allSecuritySchemes != nil {
		if r, ok := index.allSecuritySchemes.Load(refAlt); ok {
			rf := r.(*Reference)
			idx := index.extractIndex(rf)
			index.cache.Store(refAlt, r)
			return rf, idx, context.WithValue(ctx, CurrentPathKey, rf.RemoteLocation)
		}
	}

	// check the rolodex for the reference.
	if roloLookup != "" {

		if strings.Contains(roloLookup, "#") {
			roloLookup = strings.Split(roloLookup, "#")[0]
		}

		b := filepath.Base(roloLookup)
		sfn := index.GetSpecFileName()

		abp := index.GetSpecAbsolutePath()

		if b == sfn && roloLookup == abp {
			// if the reference is the same as the spec file name, we should look through the index for the component
			var r *Reference
			if len(uri) == 2 {
				r = index.FindComponentInRoot(ctx, fmt.Sprintf("#/%s", uri[1]))
			}
			return r, index, ctx
		}
		rFile, err := index.rolodex.Open(roloLookup)
		if err != nil {
			return nil, index, ctx
		}

		// extract the index from the rolodex file.
		if rFile != nil {

			n := rFile.GetFullPath()
			refParsed := ref

			// do we have a relative reference and an exact match on the suffix?
			if strings.HasPrefix(ref, "./") {
				refParsed = strings.ReplaceAll(ref, "./", "")
			}

			// if the reference starts with ../, then we need to create an absolute path from the current path context.
			if strings.HasPrefix(ref, "../") {
				// check if there is a current path in the context and then create an absolute path from it.
				if currentPath, ok := ctx.Value(CurrentPathKey).(string); ok {
					refParsed = filepath.Join(filepath.Dir(currentPath), ref)
				}
			}

			// Normalize separators for Windows comparisons.
			normPath := filepath.ToSlash(n)
			normRef := filepath.ToSlash(refParsed)
			if strings.HasSuffix(normPath, normRef) {
				node, _ := rFile.GetContentAsYAMLNode()
				if node != nil {
					r := &Reference{
						FullDefinition: n,
						Definition:     n,
						IsRemote:       true,
						RemoteLocation: n,
						Index:          rFile.GetIndex(),
						Node:           node.Content[0],
						ParentNode:     node,
					}
					index.cache.Store(ref, r)
					return r, rFile.GetIndex(), context.WithValue(ctx, CurrentPathKey, rFile.GetFullPath())
				}
			}

			idx := rFile.GetIndex()
			if index.resolver != nil {
				index.resolverLock.Lock()
				index.resolver.indexesVisited++
				index.resolverLock.Unlock()
			}
			if idx != nil {

				// check mapped refs.
				if r, ok := idx.allMappedRefs[ref]; ok {
					i := index.extractIndex(r)
					return r, i, context.WithValue(ctx, CurrentPathKey, r.RemoteLocation)
				}

				// build a collection of all the inline schemas and search them
				// for the reference.
				var d []*Reference
				d = append(d, idx.allInlineSchemaDefinitions...)
				d = append(d, idx.allRefSchemaDefinitions...)
				d = append(d, idx.allInlineSchemaObjectDefinitions...)
				for _, s := range d {
					if s.FullDefinition == ref {
						i := index.extractIndex(s)
						idx.cache.Store(ref, s)
						index.cache.Store(ref, s)
						return s, i, context.WithValue(ctx, CurrentPathKey, s.RemoteLocation)
					}
				}

				// does component exist in the root?
				node, _ := rFile.GetContentAsYAMLNode()
				if node != nil {
					var found *Reference
					exp := strings.Split(ref, "#/")
					compId := ref

					if len(exp) == 2 {
						compId = fmt.Sprintf("#/%s", exp[1])
						found = FindComponent(ctx, node, compId, exp[0], idx)
					}
					if found == nil {
						found = idx.FindComponent(ctx, ref)
					}

					if found != nil {
						i := index.extractIndex(found)
						i.cache.Store(ref, found)
						return found, i, context.WithValue(ctx, CurrentPathKey, found.RemoteLocation)
					}
				}
			}
		}
	}

	if index.logger != nil {
		// this is a last ditch effort. if this fails, all hope is lost.
		if index.GetRolodex() != nil {
			for _, i := range index.GetRolodex().GetIndexes() {
				v := i.FindComponent(ctx, ref)
				if v != nil {
					return v, v.Index, ctx
				}
			}
		}
		index.logger.Error("unable to locate reference anywhere in the rolodex", "reference", ref)
	}
	return nil, index, ctx
}

func (index *SpecIndex) extractIndex(r *Reference) *SpecIndex {
	idx := r.Index
	if idx != nil && r.Index.GetSpecAbsolutePath() != r.RemoteLocation {
		for i := range r.Index.rolodex.indexes {
			if r.Index.rolodex.indexes[i].GetSpecAbsolutePath() == r.RemoteLocation {
				idx = r.Index.rolodex.indexes[i]
				break
			}
		}
	}
	return idx
}
