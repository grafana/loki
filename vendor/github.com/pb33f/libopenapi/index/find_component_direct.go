// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"net/url"
	"strings"
	"sync"
)

func findDirectComponent(index *SpecIndex, componentID, absoluteFilePath string) *Reference {
	if index == nil || !strings.HasPrefix(componentID, "#/") {
		return nil
	}

	normalizedComponentID := normalizeComponentLookupID(componentID)

	var found *Reference
	switch {
	case strings.HasPrefix(normalizedComponentID, "#/components/schemas/"),
		strings.HasPrefix(normalizedComponentID, "#/definitions/"):
		found = loadSyncMapReference(index.allComponentSchemaDefinitions, normalizedComponentID)
	case strings.HasPrefix(normalizedComponentID, "#/components/securitySchemes/"):
		found = loadSyncMapReference(index.allSecuritySchemes, normalizedComponentID)
	case strings.HasPrefix(normalizedComponentID, "#/components/parameters/"):
		found = index.allParameters[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/requestBodies/"):
		found = index.allRequestBodies[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/responses/"):
		found = index.allResponses[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/headers/"):
		found = index.allHeaders[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/examples/"):
		found = index.allExamples[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/links/"):
		found = index.allLinks[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/callbacks/"):
		found = index.allCallbacks[normalizedComponentID]
	case strings.HasPrefix(normalizedComponentID, "#/components/pathItems/"):
		found = index.allComponentPathItems[normalizedComponentID]
	}

	if found == nil {
		return nil
	}
	return cloneFoundComponentReference(index, found, componentID, absoluteFilePath)
}

func normalizeComponentLookupID(componentID string) string {
	if componentID == "" {
		return ""
	}
	segs := strings.Split(componentID, "/")
	for i, seg := range segs {
		if i == 0 {
			continue
		}
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		if strings.ContainsRune(seg, '%') {
			seg, _ = url.QueryUnescape(seg)
		}
		segs[i] = seg
	}
	return strings.Join(segs, "/")
}

func loadSyncMapReference(collection *sync.Map, key string) *Reference {
	if collection == nil {
		return nil
	}
	if found, ok := collection.Load(key); ok {
		return found.(*Reference)
	}
	return nil
}
