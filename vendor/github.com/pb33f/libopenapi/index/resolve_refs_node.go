package index

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

// ResolveRefsInNode resolves local $ref values in a YAML node using the provided
// index. If a mapping contains sibling keys alongside $ref, sibling keys are
// preserved and merged into the resolved mapping (sibling values take precedence).
func ResolveRefsInNode(node *yaml.Node, idx *SpecIndex) *yaml.Node {
	if node == nil || idx == nil {
		return node
	}
	return resolveRefsInNode(node, idx, map[string]struct{}{})
}

func resolveRefsInNode(node *yaml.Node, idx *SpecIndex, seen map[string]struct{}) *yaml.Node {
	if node == nil || idx == nil {
		return node
	}

	switch node.Kind {
	case yaml.MappingNode:
		return resolveRefsInMappingNode(node, idx, seen)
	case yaml.SequenceNode:
		clone := *node
		clone.Content = make([]*yaml.Node, 0, len(node.Content))
		for _, item := range node.Content {
			clone.Content = append(clone.Content, resolveRefsInNode(item, idx, seen))
		}
		return &clone
	default:
		return node
	}
}

// resolveRefsInMappingNode handles $ref resolution for a single mapping node, including sibling merging.
func resolveRefsInMappingNode(node *yaml.Node, idx *SpecIndex, seen map[string]struct{}) *yaml.Node {
	ref, hasRef := findRefInMappingNode(node)
	if !hasRef {
		return cloneMappingNodeWithResolvedChildren(node, idx, seen)
	}

	// This helper is intentionally local-only; keep external refs intact.
	if !strings.HasPrefix(ref, "#/") {
		return cloneMappingNodeWithResolvedChildren(node, idx, seen)
	}

	if _, exists := seen[ref]; exists {
		return cloneMappingNodeWithResolvedChildren(node, idx, seen)
	}

	seen[ref] = struct{}{}
	var resolved *yaml.Node
	if resolvedRef, _ := idx.SearchIndexForReference(ref); resolvedRef != nil && resolvedRef.Node != nil {
		resolved = resolveRefsInNode(resolvedRef.Node, idx, seen)
	}
	delete(seen, ref)

	if resolved == nil {
		return cloneMappingNodeWithResolvedChildren(node, idx, seen)
	}

	if !hasNonRefSiblings(node) {
		return resolved
	}

	siblings := extractResolvedSiblingPairs(node, idx, seen)
	if resolved.Kind == yaml.MappingNode {
		return mergeResolvedMappingWithSiblings(resolved, siblings)
	}
	if resolved.Kind == yaml.DocumentNode && len(resolved.Content) > 0 && resolved.Content[0] != nil && resolved.Content[0].Kind == yaml.MappingNode {
		docClone := *resolved
		docClone.Content = append([]*yaml.Node(nil), resolved.Content...)
		docClone.Content[0] = mergeResolvedMappingWithSiblings(resolved.Content[0], siblings)
		return &docClone
	}

	// Fallback: keep original mapping (with $ref) but still resolve sibling values.
	return cloneMappingNodeWithResolvedChildren(node, idx, seen)
}

// hasNonRefSiblings returns true if the mapping node contains keys other than "$ref".
func hasNonRefSiblings(node *yaml.Node) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if key != nil && key.Value != "$ref" {
			return true
		}
	}
	return false
}

// findRefInMappingNode extracts the "$ref" value from a mapping node, if present.
func findRefInMappingNode(node *yaml.Node) (string, bool) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		if key != nil && key.Value == "$ref" && val != nil && val.Kind == yaml.ScalarNode {
			return val.Value, true
		}
	}
	return "", false
}

// extractResolvedSiblingPairs collects all non-$ref key-value pairs from a mapping, resolving their values.
func extractResolvedSiblingPairs(node *yaml.Node, idx *SpecIndex, seen map[string]struct{}) []*yaml.Node {
	out := make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		if key != nil && key.Value == "$ref" {
			continue
		}
		out = append(out, key, resolveRefsInNode(val, idx, seen))
	}
	return out
}

// cloneMappingNodeWithResolvedChildren shallow-clones a mapping node, recursively resolving each child value.
func cloneMappingNodeWithResolvedChildren(node *yaml.Node, idx *SpecIndex, seen map[string]struct{}) *yaml.Node {
	clone := *node
	clone.Content = make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		clone.Content = append(clone.Content, key, resolveRefsInNode(val, idx, seen))
	}
	return &clone
}

// mergeResolvedMappingWithSiblings combines a resolved mapping with sibling key-value pairs; siblings win on conflict.
func mergeResolvedMappingWithSiblings(resolved *yaml.Node, siblings []*yaml.Node) *yaml.Node {
	merged := *resolved
	merged.Content = make([]*yaml.Node, 0, len(resolved.Content)+len(siblings))

	keyPos := make(map[string]int, len(resolved.Content)/2+len(siblings)/2)
	for i := 0; i+1 < len(resolved.Content); i += 2 {
		key := resolved.Content[i]
		val := resolved.Content[i+1]
		merged.Content = append(merged.Content, key, val)
		if key != nil {
			keyPos[key.Value] = len(merged.Content) - 2
		}
	}

	for i := 0; i+1 < len(siblings); i += 2 {
		key := siblings[i]
		val := siblings[i+1]
		if key == nil {
			continue
		}
		if pos, ok := keyPos[key.Value]; ok {
			merged.Content[pos+1] = val
			continue
		}
		merged.Content = append(merged.Content, key, val)
		keyPos[key.Value] = len(merged.Content) - 2
	}

	return &merged
}
