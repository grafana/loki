// Copyright 2023-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package paths

import (
	"net/http"
	"strings"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// pathCandidate represents a potential path match with metadata for selection.
type pathCandidate struct {
	pathItem  *v3.PathItem
	path      string
	score     int
	hasMethod bool
}

// computeSpecificityScore calculates how specific a path template is.
// literal segments score higher than parameterized segments, ensuring
// "/pets/mine" is preferred over "/pets/{id}" per OpenAPI spec.
//
// scoring:
//   - literal segment: 1000 points
//   - parameter segment: 1 point
//
// this weighting ensures any path with more literal segments always wins,
// regardless of parameter positions.
func computeSpecificityScore(pathTemplate string) int {
	segments := strings.Split(pathTemplate, "/")
	score := 0

	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if isParameterSegment(seg) {
			score += 1
		} else {
			score += 1000
		}
	}
	return score
}

// isParameterSegment returns true if the segment contains a path parameter.
// handles standard {param}, label {.param}, and exploded {param*} formats.
func isParameterSegment(seg string) bool {
	return strings.Contains(seg, "{") && strings.Contains(seg, "}")
}

// pathHasMethod checks if the PathItem has an operation for the given HTTP method.
func pathHasMethod(pathItem *v3.PathItem, method string) bool {
	switch method {
	case http.MethodGet:
		return pathItem.Get != nil
	case http.MethodPost:
		return pathItem.Post != nil
	case http.MethodPut:
		return pathItem.Put != nil
	case http.MethodDelete:
		return pathItem.Delete != nil
	case http.MethodOptions:
		return pathItem.Options != nil
	case http.MethodHead:
		return pathItem.Head != nil
	case http.MethodPatch:
		return pathItem.Patch != nil
	case http.MethodTrace:
		return pathItem.Trace != nil
	}
	return false
}

// selectMatches finds the best matching candidates in a single pass.
// returns the highest-scoring candidate with the method (or nil), and
// the highest-scoring candidate overall (for error reporting).
func selectMatches(candidates []pathCandidate) (withMethod, highest *pathCandidate) {
	for i := range candidates {
		c := &candidates[i]

		if c.hasMethod && (withMethod == nil || c.score > withMethod.score) {
			withMethod = c
		}

		if highest == nil || c.score > highest.score {
			highest = c
		}
	}
	return withMethod, highest
}
