// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package helpers

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ExtractJSONPathFromValidationError traverses and processes a ValidationError to construct a JSONPath string representation of its instance location.
func ExtractJSONPathFromValidationError(e *jsonschema.ValidationError) string {
	if len(e.Causes) > 0 {
		for _, cause := range e.Causes {
			ExtractJSONPathFromValidationError(cause)
		}
	}

	if len(e.InstanceLocation) > 0 {

		var b strings.Builder
		b.WriteString("$")

		for _, seg := range e.InstanceLocation {
			switch {
			case isNumeric(seg):
				b.WriteString(fmt.Sprintf("[%s]", seg))

			case isSimpleIdentifier(seg):
				b.WriteByte('.')
				b.WriteString(seg)

			default:
				esc := escapeBracketString(seg)
				b.WriteString("['")
				b.WriteString(esc)
				b.WriteString("']")
			}
		}
		return b.String()
	}
	return ""
}

// isNumeric returns true if s is a non‐empty string of digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isSimpleIdentifier returns true if s matches [A-Za-z_][A-Za-z0-9_]*.
func isSimpleIdentifier(s string) bool {
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return len(s) > 0
}

// escapeBracketString escapes backslashes and single‐quotes for inside ['...']
func escapeBracketString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// ExtractJSONPathsFromValidationErrors takes a slice of ValidationError pointers and returns a slice of JSONPath strings
func ExtractJSONPathsFromValidationErrors(errors []*jsonschema.ValidationError) []string {
	var paths []string
	for _, err := range errors {
		path := ExtractJSONPathFromValidationError(err)
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// ExtractFieldNameFromInstanceLocation returns the last segment of the instance location as the field name
func ExtractFieldNameFromInstanceLocation(instanceLocation []string) string {
	if len(instanceLocation) == 0 {
		return ""
	}
	return instanceLocation[len(instanceLocation)-1]
}

// ExtractFieldNameFromStringLocation returns the last segment of the instance location as the field name
// when the location is provided as a string path
func ExtractFieldNameFromStringLocation(instanceLocation string) string {
	if instanceLocation == "" {
		return ""
	}

	// Handle string format like "/properties/email" or "/0/name"
	segments := strings.Split(strings.Trim(instanceLocation, "/"), "/")
	if len(segments) == 0 || (len(segments) == 1 && segments[0] == "") {
		return ""
	}

	return segments[len(segments)-1]
}

// ExtractJSONPathFromInstanceLocation creates a JSONPath string from instance location segments
func ExtractJSONPathFromInstanceLocation(instanceLocation []string) string {
	if len(instanceLocation) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("$")

	for _, seg := range instanceLocation {
		switch {
		case isNumeric(seg):
			b.WriteString(fmt.Sprintf("[%s]", seg))

		case isSimpleIdentifier(seg):
			b.WriteByte('.')
			b.WriteString(seg)

		default:
			esc := escapeBracketString(seg)
			b.WriteString("['")
			b.WriteString(esc)
			b.WriteString("']")
		}
	}
	return b.String()
}

// ExtractJSONPathFromStringLocation creates a JSONPath string from string-based instance location
func ExtractJSONPathFromStringLocation(instanceLocation string) string {
	if instanceLocation == "" {
		return ""
	}

	// Convert string format like "/properties/email" to array format
	segments := strings.Split(strings.Trim(instanceLocation, "/"), "/")
	if len(segments) == 0 || (len(segments) == 1 && segments[0] == "") {
		return ""
	}

	return ExtractJSONPathFromInstanceLocation(segments)
}

// ConvertStringLocationToPathSegments converts a string-based instance location to path segments array
// Handles edge cases like empty strings and root-only paths
func ConvertStringLocationToPathSegments(instanceLocation string) []string {
	if instanceLocation == "" {
		return []string{}
	}

	segments := strings.Split(strings.Trim(instanceLocation, "/"), "/")
	if len(segments) == 1 && segments[0] == "" {
		return []string{}
	}

	return segments
}
