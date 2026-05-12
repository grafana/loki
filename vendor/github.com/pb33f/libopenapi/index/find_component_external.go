// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (index *SpecIndex) lookupRolodex(ctx context.Context, uri []string) *Reference {
	if index.rolodex == nil {
		return nil
	}
	if index.config != nil && index.config.SkipExternalRefResolution {
		return nil
	}

	if len(uri) == 0 {
		return nil
	}

	file := strings.ReplaceAll(uri[0], "file:", "")
	fileName := filepath.Base(file)
	absoluteFileLocation := file
	if !filepath.IsAbs(file) && !strings.HasPrefix(file, "http") {
		basePath := index.config.BasePath
		if index.specAbsolutePath != "" {
			basePath = filepath.Dir(index.specAbsolutePath)
		}
		absoluteFileLocation, _ = filepath.Abs(utils.CheckPathOverlap(basePath, file, string(os.PathSeparator)))
	}

	ext := filepath.Ext(absoluteFileLocation)
	var parsedDocument *yaml.Node
	idx := index
	if ext != "" {
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
		if rFile.GetIndex() == nil && !IsFileBeingIndexed(ctx, absoluteFileLocation) {
			rFile.WaitForIndexing()
		}
		if rFile.GetIndex() != nil {
			idx = rFile.GetIndex()
		}
		parsedDocument, _ = rFile.GetContentAsYAMLNode()
	} else {
		parsedDocument = index.root
	}

	wholeFile := len(uri) < 2
	query := ""
	if !wholeFile {
		query = fmt.Sprintf("#/%s", uri[1])
	}

	if wholeFile {
		if parsedDocument != nil && parsedDocument.Kind == yaml.DocumentNode {
			parsedDocument = parsedDocument.Content[0]
		}
		var parentNode *yaml.Node
		if index.allRefs[absoluteFileLocation] != nil {
			parentNode = index.allRefs[absoluteFileLocation].ParentNode
		}
		return &Reference{
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
	}

	foundRef := FindComponent(ctx, parsedDocument, query, absoluteFileLocation, index)
	if foundRef != nil {
		foundRef.IsRemote = true
		foundRef.RemoteLocation = absoluteFileLocation
		return foundRef
	}
	index.logger.Debug("[lookupRolodex] FindComponent returned nil",
		"absoluteFileLocation", absoluteFileLocation,
		"query", query,
		"parsedDocument_nil", parsedDocument == nil,
		"idx_nil", idx == nil)
	return nil
}
