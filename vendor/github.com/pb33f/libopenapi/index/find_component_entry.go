// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/pb33f/jsonpath/pkg/jsonpath"
	jsonpathconfig "github.com/pb33f/jsonpath/pkg/jsonpath/config"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// FindComponent locates a component in the index by reference.
//
// It resolves local references directly from the current document first, then recurses through
// rolodex-backed file and remote references as needed. It returns nil when the target cannot be found.
func (index *SpecIndex) FindComponent(ctx context.Context, componentId string) *Reference {
	if index.root == nil {
		return nil
	}

	if resolved := index.ResolveRefViaSchemaId(componentId); resolved != nil {
		return resolved
	}

	if strings.HasPrefix(componentId, "/") {
		baseURI, fragment := SplitRefFragment(componentId)
		if resolved := index.resolveRefViaSchemaIdPath(baseURI); resolved != nil {
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
			}
			return index.lookupRolodex(ctx, uri)
		}
		return index.FindComponentInRoot(ctx, fmt.Sprintf("#/%s", uri[1]))
	}

	fileExt := filepath.Ext(componentId)
	if fileExt != "" {
		return index.lookupRolodex(ctx, uri)
	}

	return index.FindComponentInRoot(ctx, componentId)
}

// FindComponent locates a component within a specific root YAML node.
//
// The lookup prefers direct fragment navigation and direct component maps first, and falls back to
// JSONPath traversal for legacy or non-direct component identifiers.
func FindComponent(_ context.Context, root *yaml.Node, componentID, absoluteFilePath string, index *SpecIndex) *Reference {
	if strings.Contains(componentID, "%") {
		componentID, _ = url.QueryUnescape(componentID)
	}

	if fastRef := findDirectComponent(index, componentID, absoluteFilePath); fastRef != nil {
		return fastRef
	}

	if strings.HasPrefix(componentID, "#/") {
		if node := navigateToFragment(root, componentID); node != nil {
			name, friendlySearch := utils.ConvertComponentIdIntoFriendlyPathSearch(componentID)
			if friendlySearch == "$." {
				friendlySearch = "$"
			}
			return buildResolvedComponentReference(index, nil, componentID, absoluteFilePath, name, friendlySearch, node)
		}
	}

	name, friendlySearch := utils.ConvertComponentIdIntoFriendlyPathSearch(componentID)
	if friendlySearch == "$." {
		friendlySearch = "$"
	}
	path, err := jsonpath.NewPath(friendlySearch, jsonpathconfig.WithPropertyNameExtension(), jsonpathconfig.WithLazyContextTracking())
	if path == nil || err != nil || root == nil {
		return nil
	}
	res := path.Query(root)
	if len(res) == 1 {
		return buildResolvedComponentReference(index, nil, componentID, absoluteFilePath, name, friendlySearch, res[0])
	}
	return nil
}

// FindComponentInRoot locates a component reference in the current root document only.
//
// It normalizes file-prefixed local references back to root-document fragments before delegating
// to FindComponent.
func (index *SpecIndex) FindComponentInRoot(ctx context.Context, componentID string) *Reference {
	if index.root != nil {
		componentID = utils.ReplaceWindowsDriveWithLinuxPath(componentID)
		if !strings.HasPrefix(componentID, "#/") {
			spl := strings.Split(componentID, "#/")
			if len(spl) == 2 && spl[0] != "" {
				componentID = fmt.Sprintf("#/%s", spl[1])
			}
		}
		return FindComponent(ctx, index.root, componentID, index.specAbsolutePath, index)
	}
	return nil
}
