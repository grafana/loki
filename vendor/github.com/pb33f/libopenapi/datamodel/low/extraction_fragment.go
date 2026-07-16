// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package low

import (
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// navigateReferenceFragment navigates a local JSON Pointer fragment within a YAML node tree.
// Supported fragment formats are "#/path/to/node", "/path/to/node", and "#/" for the root.
func navigateReferenceFragment(root *yaml.Node, fragment string) *yaml.Node {
	if root == nil || fragment == "" {
		return nil
	}

	if !strings.HasPrefix(fragment, "#") && !strings.HasPrefix(fragment, "/") {
		return nil
	}

	path := strings.TrimPrefix(fragment, "#")
	if path == "" || path == "/" {
		return nil
	}

	current := utils.NodeAlias(root)
	if current.Kind == yaml.DocumentNode && len(current.Content) > 0 {
		current = utils.NodeAlias(current.Content[0])
	}

	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, segment := range segments {
		if segment == "" {
			continue
		}

		segment = strings.ReplaceAll(segment, "~1", "/")
		segment = strings.ReplaceAll(segment, "~0", "~")

		switch {
		case utils.IsNodeMap(current):
			current = lookupFragmentMapValue(current, segment)
		case utils.IsNodeArray(current):
			current = lookupFragmentSequenceValue(current, segment)
		default:
			return nil
		}

		if current == nil {
			return nil
		}
	}

	return utils.NodeAlias(current)
}

func lookupFragmentMapValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return utils.NodeAlias(node.Content[i+1])
		}
	}
	return nil
}

func lookupFragmentSequenceValue(node *yaml.Node, segment string) *yaml.Node {
	index := 0
	for _, c := range segment {
		if c < '0' || c > '9' {
			return nil
		}
		index = index*10 + int(c-'0')
	}
	if index >= len(node.Content) {
		return nil
	}
	return utils.NodeAlias(node.Content[index])
}
