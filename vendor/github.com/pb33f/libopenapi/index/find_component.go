// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	jsonpathconfig "github.com/pb33f/jsonpath/pkg/jsonpath/config"

	"github.com/pb33f/jsonpath/pkg/jsonpath"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// FindComponent will locate a component by its reference, returns nil if nothing is found.
// This method will recurse through remote, local and file references. For each new external reference
// a new index will be created. These indexes can then be traversed recursively.
func (index *SpecIndex) FindComponent(ctx context.Context, componentId string) *Reference {
	if index.root == nil {
		return nil
	}

	if strings.HasPrefix(componentId, "/") {
		baseUri, fragment := SplitRefFragment(componentId)
		if resolved := index.resolveRefViaSchemaIdPath(baseUri); resolved != nil {
			if fragment != "" && resolved.Node != nil {
				if fragmentNode := navigateToFragment(resolved.Node, fragment); fragmentNode != nil {
					resolved.Node = fragmentNode
				}
			}
			return resolved
		}
	}

	uri := strings.Split(componentId, "#/")
	if len(uri) == 2 {
		if uri[0] != "" {
			if index.specAbsolutePath == uri[0] {
				return index.FindComponentInRoot(ctx, fmt.Sprintf("#/%s", uri[1]))
			} else {
				return index.lookupRolodex(ctx, uri)
			}
		} else {
			return index.FindComponentInRoot(ctx, fmt.Sprintf("#/%s", uri[1]))
		}
	} else {

		// does it contain a file extension?
		fileExt := filepath.Ext(componentId)
		if fileExt != "" {

			// check if the context has a root index set, if so this is a deep search that has moved through multiple
			// indexes and we need to adjust the URI to reflect the location of the root index.
			//
			// the below code has been commended out due to being handled in the index. Keeping it for legacy and for
			// future bugs.
			//
			//if ctx.Value(RootIndexKey) != nil {
			//	rootIndex := ctx.Value(RootIndexKey).(*SpecIndex)
			//	if rootIndex != nil && rootIndex.specAbsolutePath != "" {
			//		dir := filepath.Dir(rootIndex.specAbsolutePath)
			//		// create an absolute path to the file.
			//		absoluteFilePath := filepath.Join(dir, componentId)
			//		// split into a URI.
			//		uri = []string{absoluteFilePath}
			//	}
			//}

			return index.lookupRolodex(ctx, uri)
		}

		// root search
		return index.FindComponentInRoot(ctx, componentId)
	}
}

func FindComponent(_ context.Context, root *yaml.Node, componentId, absoluteFilePath string, index *SpecIndex) *Reference {
	// check component for url encoding.
	if strings.Contains(componentId, "%") {
		// decode the url.
		componentId, _ = url.QueryUnescape(componentId)
	}

	name, friendlySearch := utils.ConvertComponentIdIntoFriendlyPathSearch(componentId)
	if friendlySearch == "$." {
		friendlySearch = "$"
	}
	path, err := jsonpath.NewPath(friendlySearch, jsonpathconfig.WithPropertyNameExtension())
	if path == nil || err != nil || root == nil {
		return nil // no component found
	}
	res := path.Query(root)

	if len(res) == 1 {
		resNode := res[0]
		fullDef := fmt.Sprintf("%s%s", absoluteFilePath, componentId)
		// extract properties

		// check if we have already seen this reference and there is a parent, use it
		var parentNode *yaml.Node
		if index.allRefs[componentId] != nil {
			parentNode = index.allRefs[componentId].ParentNode
		}
		if index.allRefs[fullDef] != nil {
			parentNode = index.allRefs[fullDef].ParentNode
		}

		ref := &Reference{
			FullDefinition:        fullDef,
			Definition:            componentId,
			Name:                  name,
			Node:                  resNode,
			Path:                  friendlySearch,
			RemoteLocation:        absoluteFilePath,
			ParentNode:            parentNode,
			Index:                 index,
			RequiredRefProperties: extractDefinitionRequiredRefProperties(resNode, map[string][]string{}, fullDef, index),
		}
		return ref
	}
	return nil
}

func (index *SpecIndex) FindComponentInRoot(ctx context.Context, componentId string) *Reference {
	if index.root != nil {

		componentId = utils.ReplaceWindowsDriveWithLinuxPath(componentId)
		if !strings.HasPrefix(componentId, "#/") {
			spl := strings.Split(componentId, "#/")
			if len(spl) == 2 {
				if spl[0] != "" {
					componentId = fmt.Sprintf("#/%s", spl[1])
				}
			}
		}

		return FindComponent(ctx, index.root, componentId, index.specAbsolutePath, index)
	}
	return nil
}

func (index *SpecIndex) lookupRolodex(ctx context.Context, uri []string) *Reference {
	if index.rolodex == nil {
		return nil
	}

	// If SkipExternalRefResolution is enabled, don't attempt rolodex lookups
	if index.config != nil && index.config.SkipExternalRefResolution {
		return nil
	}

	if len(uri) > 0 {

		// split string to remove file reference
		file := strings.ReplaceAll(uri[0], "file:", "")

		var absoluteFileLocation, fileName string
		fileName = filepath.Base(file)
		absoluteFileLocation = file
		if !filepath.IsAbs(file) && !strings.HasPrefix(file, "http") {
			// When resolving relative references, use the directory of the current index's spec file
			// if available, not the root BasePath. This ensures that external files can resolve
			// relative paths correctly (e.g., "./models/foo.yaml" from an external file should
			// resolve relative to that external file's directory, not the root spec directory).
			basePath := index.config.BasePath
			if index.specAbsolutePath != "" {
				basePath = filepath.Dir(index.specAbsolutePath)
			}
			absoluteFileLocation, _ = filepath.Abs(utils.CheckPathOverlap(basePath, file, string(os.PathSeparator)))
		}

		// if the absolute file location has no file ext, then get the rolodex root.
		ext := filepath.Ext(absoluteFileLocation)
		var parsedDocument *yaml.Node
		idx := index
		if ext != "" {
			// extract the document from the rolodex.
			rFile, rError := index.rolodex.OpenWithContext(ctx, absoluteFileLocation)

			if rError != nil {
				index.logger.Error("unable to open the rolodex file, check specification references and base path",
					"file", absoluteFileLocation, "error", rError)
				return nil
			}

			if rFile == nil {
				index.logger.Error("cannot locate file in the rolodex, check specification references and base path",
					"file", absoluteFileLocation)
				return nil
			}
			// Check if the index is already available (handles recursive lookups within same goroutine).
			// Only wait for indexing if the index isn't already set - this prevents deadlocks
			// in recursive scenarios where A->B->A would otherwise wait forever.
			if rFile.GetIndex() == nil {
				// Check if this file is being indexed in the current call chain.
				// If so, we have a circular dependency and should NOT wait (would deadlock).
				// Instead, proceed without the index - the component lookup will still work
				// using the parsed YAML content, and circular references will be detected later.
				if !IsFileBeingIndexed(ctx, absoluteFileLocation) {
					// Wait for the file's index to be ready before using it.
					// This handles the case where another goroutine is still indexing the file.
					rFile.WaitForIndexing()
				}
			}
			if rFile.GetIndex() != nil {
				idx = rFile.GetIndex()
			}

			parsedDocument, _ = rFile.GetContentAsYAMLNode()

		} else {
			parsedDocument = index.root
		}

		wholeFile := false
		query := ""
		if len(uri) < 2 {
			wholeFile = true
		} else {
			query = fmt.Sprintf("#/%s", uri[1])
		}

		// check if there is a component we want to suck in, or if the
		// entire root needs to come in.
		var foundRef *Reference
		if wholeFile {

			if parsedDocument != nil {
				if parsedDocument.Kind == yaml.DocumentNode {
					parsedDocument = parsedDocument.Content[0]
				}
			}

			var parentNode *yaml.Node
			if index.allRefs[absoluteFileLocation] != nil {
				parentNode = index.allRefs[absoluteFileLocation].ParentNode
			}

			foundRef = &Reference{
				ParentNode:            parentNode,
				FullDefinition:        absoluteFileLocation,
				Definition:            fileName,
				Name:                  fileName,
				Index:                 idx,
				Node:                  parsedDocument,
				IsRemote:              true,
				RemoteLocation:        absoluteFileLocation,
				Path:                  "$",
				RequiredRefProperties: extractDefinitionRequiredRefProperties(parsedDocument, map[string][]string{}, absoluteFileLocation, index),
			}
			return foundRef
		} else {
			foundRef = FindComponent(ctx, parsedDocument, query, absoluteFileLocation, index)
			if foundRef != nil {
				foundRef.IsRemote = true
				foundRef.RemoteLocation = absoluteFileLocation
				return foundRef
			} else {
				// Debug: log when FindComponent returns nil
				index.logger.Debug("[lookupRolodex] FindComponent returned nil",
					"absoluteFileLocation", absoluteFileLocation,
					"query", query,
					"parsedDocument_nil", parsedDocument == nil,
					"idx_nil", idx == nil)
			}
		}
	}
	return nil
}
