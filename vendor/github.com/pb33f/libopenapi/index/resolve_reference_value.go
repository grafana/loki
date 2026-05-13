package index

import (
	"net/url"
	"strconv"
	"strings"
)

// ResolveReferenceValue resolves a reference string to a decoded value.
//
// Resolution order:
//  1. Resolve using SpecIndex when available.
//  2. Fallback to local JSON pointer resolution (e.g. "#/components/schemas/Foo")
//     using getDocData when provided.
//
// Returns nil when the reference cannot be resolved.
func ResolveReferenceValue(ref string, specIndex *SpecIndex, getDocData func() map[string]interface{}) interface{} {
	if ref == "" {
		return nil
	}

	if specIndex != nil {
		if resolvedRef, _ := specIndex.SearchIndexForReference(ref); resolvedRef != nil && resolvedRef.Node != nil {
			var decoded interface{}
			if err := resolvedRef.Node.Decode(&decoded); err == nil {
				return decoded
			}
		}
	}

	// Fallback parser only supports local JSON pointers ("#" root or "#/...").
	if ref != "#" && !strings.HasPrefix(ref, "#/") {
		return nil
	}

	if getDocData == nil {
		return nil
	}
	docData := getDocData()
	if docData == nil {
		return nil
	}

	return resolveLocalJSONPointer(docData, ref)
}

func resolveLocalJSONPointer(docData map[string]interface{}, ref string) interface{} {
	if ref == "" {
		return nil
	}
	if ref == "#" {
		return docData
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}

	segments := strings.Split(ref[2:], "/")
	var current interface{} = docData

	for _, rawSegment := range segments {
		segment := decodeJSONPointerToken(rawSegment)
		switch node := current.(type) {
		case map[string]interface{}:
			next, ok := node[segment]
			if !ok {
				return nil
			}
			current = next
		case []interface{}:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil
			}
			current = node[idx]
		default:
			return nil
		}
	}

	return current
}

func decodeJSONPointerToken(token string) string {
	if strings.Contains(token, "%") {
		decoded, err := url.PathUnescape(token)
		if err == nil {
			token = decoded
		}
	}
	if !strings.Contains(token, "~") {
		return token
	}
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}
