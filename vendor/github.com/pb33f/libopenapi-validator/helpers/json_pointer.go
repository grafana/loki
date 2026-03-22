// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package helpers

import (
	"fmt"
	"strings"

	"github.com/go-openapi/jsonpointer"
)

// EscapeJSONPointerSegment escapes a single segment for use in a JSON Pointer (RFC 6901).
// It replaces '~' with '~0' and '/' with '~1'.
func EscapeJSONPointerSegment(segment string) string {
	return jsonpointer.Escape(segment)
}

// ConstructParameterJSONPointer constructs a full JSON Pointer path for a parameter
// in the OpenAPI specification.
// Format: /paths/{path}/{method}/parameters/{paramName}/schema/{keyword}
// The path segment is automatically escaped according to RFC 6901.
// The keyword can be a simple keyword like "type" or a nested path like "items/type".
func ConstructParameterJSONPointer(pathTemplate, method, paramName, keyword string) string {
	escapedPath := EscapeJSONPointerSegment(pathTemplate)
	escapedPath = strings.TrimPrefix(escapedPath, "~1") // Remove leading slash encoding
	method = strings.ToLower(method)
	return fmt.Sprintf("/paths/%s/%s/parameters/%s/schema/%s", escapedPath, method, paramName, keyword)
}

// ConstructResponseHeaderJSONPointer constructs a full JSON Pointer path for a response header
// in the OpenAPI specification.
// Format: /paths/{path}/{method}/responses/{statusCode}/headers/{headerName}/{keyword}
// The path segment is automatically escaped according to RFC 6901.
func ConstructResponseHeaderJSONPointer(pathTemplate, method, statusCode, headerName, keyword string) string {
	escapedPath := EscapeJSONPointerSegment(pathTemplate)
	escapedPath = strings.TrimPrefix(escapedPath, "~1") // Remove leading slash encoding
	method = strings.ToLower(method)
	return fmt.Sprintf("/paths/%s/%s/responses/%s/headers/%s/%s", escapedPath, method, statusCode, headerName, keyword)
}
