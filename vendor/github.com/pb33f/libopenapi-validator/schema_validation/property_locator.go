// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"

	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
)

// PropertyNameInfo contains extracted information about a property name validation error
type PropertyNameInfo struct {
	PropertyName   string   // The property name that violated validation (e.g., "$defs-atmVolatility_type")
	ParentLocation []string // The path to the parent containing the property (e.g., ["components", "schemas"])
	EnhancedReason string   // A more detailed error message with context
	Pattern        string   // The pattern that was violated, if applicable
}

var (
	// invalidPropertyNameRegex matches errors like: "invalid propertyName 'X'"
	invalidPropertyNameRegex = regexp.MustCompile(`invalid propertyName '([^']+)'`)

	// patternMismatchRegex matches errors like: "'X' does not match pattern 'Y'"
	patternMismatchRegex = regexp.MustCompile(`'([^']+)' does not match pattern '([^']+)'`)
)

// extractPropertyNameFromError extracts property name information from a jsonschema.ValidationError
// when BasicOutput doesn't provide useful InstanceLocation.
// This handles Priority 1 (invalid propertyName) and Priority 2 (pattern mismatch) cases.
//
// Returns PropertyNameInfo with extracted details, or nil if no relevant information found.
// Note: ValidationError.Error() includes all cause information in the formatted string,
// so we only need to check the root error message.
func extractPropertyNameFromError(ve *jsonschema.ValidationError) *PropertyNameInfo {
	if ve == nil {
		return nil
	}

	// Check error message for patterns (Error() includes all cause information)
	return checkErrorForPropertyInfo(ve)
}

// checkErrorForPropertyInfo examines a single ValidationError for property name patterns.
// This is extracted as a separate function to avoid duplication and improve testability.
func checkErrorForPropertyInfo(ve *jsonschema.ValidationError) *PropertyNameInfo {
	errMsg := ve.Error()
	return checkErrorMessageForPropertyInfo(errMsg, ve.InstanceLocation, ve)
}

// checkErrorMessageForPropertyInfo extracts property name info from an error message string.
// This is separated to improve testability while keeping validation error traversal logic intact.
func checkErrorMessageForPropertyInfo(errMsg string, instanceLocation []string, ve *jsonschema.ValidationError) *PropertyNameInfo {
	// Check for "invalid propertyName 'X'" first (most specific error message)
	if matches := invalidPropertyNameRegex.FindStringSubmatch(errMsg); len(matches) > 1 {
		propertyName := matches[1]
		info := &PropertyNameInfo{
			PropertyName:   propertyName,
			ParentLocation: instanceLocation,
		}

		// try to extract pattern information from deeper causes if available
		var pattern string
		if ve != nil {
			pattern = extractPatternFromCauses(ve)
		}

		if pattern != "" {
			info.Pattern = pattern
			info.EnhancedReason = buildEnhancedReason(propertyName, pattern)
		} else {
			info.EnhancedReason = "invalid propertyName '" + propertyName + "'"
		}

		return info
	}

	// Check for "'X' does not match pattern 'Y'" as fallback (pattern violation)
	if matches := patternMismatchRegex.FindStringSubmatch(errMsg); len(matches) > 2 {
		return &PropertyNameInfo{
			PropertyName:   matches[1],
			ParentLocation: instanceLocation,
			Pattern:        matches[2],
			EnhancedReason: buildEnhancedReason(matches[1], matches[2]),
		}
	}

	return nil
}

// extractPatternFromCauses looks through error causes to find pattern violation details.
// Since ValidationError.Error() includes all cause information, we check the formatted error string.
func extractPatternFromCauses(ve *jsonschema.ValidationError) string {
	if ve == nil {
		return ""
	}

	// Check the error message which includes all cause information
	errMsg := ve.Error()
	if matches := patternMismatchRegex.FindStringSubmatch(errMsg); len(matches) > 2 {
		return matches[2]
	}

	return ""
}

// buildEnhancedReason constructs a detailed error message with property name and pattern
func buildEnhancedReason(propertyName, pattern string) string {
	var buf strings.Builder
	buf.Grow(len(propertyName) + len(pattern) + 50) // pre-allocate to avoid reallocation
	buf.WriteString("invalid propertyName '")
	buf.WriteString(propertyName)
	buf.WriteString("': does not match pattern '")
	buf.WriteString(pattern)
	buf.WriteString("'")
	return buf.String()
}

// findPropertyKeyNodeInYAML searches the YAML tree for a property key node at a specific location.
// It first navigates to the parent location, then searches for the property name as a map key.
//
// Parameters:
//   - rootNode: The root YAML node to search from
//   - propertyName: The property key to find (e.g., "$defs-atmVolatility_type")
//   - parentPath: Path segments to the parent (e.g., ["components", "schemas"])
//
// Returns the YAML node for the property key, or nil if not found.
func findPropertyKeyNodeInYAML(rootNode *yaml.Node, propertyName string, parentPath []string) *yaml.Node {
	if rootNode == nil || propertyName == "" {
		return nil
	}

	// Navigate to parent location first
	currentNode := rootNode
	for _, segment := range parentPath {
		currentNode = navigateToYAMLChild(currentNode, segment)
		if currentNode == nil {
			return nil
		}
	}

	// Search for the property name as a map key
	return findMapKeyNode(currentNode, propertyName)
}

// navigateToYAMLChild navigates from a parent node to a child by name.
// Handles both document root navigation and map content navigation.
func navigateToYAMLChild(parent *yaml.Node, childName string) *yaml.Node {
	if parent == nil {
		return nil
	}

	// If parent is a document node, navigate to its content
	if parent.Kind == yaml.DocumentNode && len(parent.Content) > 0 {
		parent = parent.Content[0]
	}

	// Navigate through mapping node
	if parent.Kind == yaml.MappingNode {
		return findMapKeyValue(parent, childName)
	}

	return nil
}

// findMapKeyValue searches a mapping node for a key and returns its value node
func findMapKeyValue(mappingNode *yaml.Node, keyName string) *yaml.Node {
	if mappingNode.Kind != yaml.MappingNode {
		return nil
	}

	// mapping nodes have key-value pairs: [key1, value1, key2, value2, ...]
	for i := 0; i < len(mappingNode.Content); i += 2 {
		keyNode := mappingNode.Content[i]
		if keyNode.Value == keyName {
			// return the value node (i+1)
			if i+1 < len(mappingNode.Content) {
				return mappingNode.Content[i+1]
			}
		}
	}

	return nil
}

// findMapKeyNode searches a mapping node for a key and returns the key node itself (not the value)
func findMapKeyNode(mappingNode *yaml.Node, keyName string) *yaml.Node {
	if mappingNode == nil {
		return nil
	}

	// if it's a document node, unwrap to content
	if mappingNode.Kind == yaml.DocumentNode && len(mappingNode.Content) > 0 {
		mappingNode = mappingNode.Content[0]
	}

	if mappingNode.Kind != yaml.MappingNode {
		return nil
	}

	// mapping nodes have key-value pairs: [key1, value1, key2, value2, ...]
	for i := 0; i < len(mappingNode.Content); i += 2 {
		keyNode := mappingNode.Content[i]
		if keyNode.Value == keyName {
			return keyNode // contains line/column metadata for error reporting
		}
	}

	return nil
}

// applyPropertyNameFallback attempts to enrich a violation with property name information
// when the primary location method fails. Returns true if enrichment was applied.
func applyPropertyNameFallback(
	propertyInfo *PropertyNameInfo,
	rootNode *yaml.Node,
	violation *liberrors.SchemaValidationFailure,
) bool {
	if propertyInfo == nil {
		return false
	}

	return enrichSchemaValidationFailure(
		propertyInfo,
		rootNode,
		&violation.Line,
		&violation.Column,
		&violation.Reason,
		&violation.FieldName,
		&violation.FieldPath,
		&violation.Location,
		&violation.InstancePath,
	)
}

// enrichSchemaValidationFailure attempts to enhance a SchemaValidationFailure with better
// location information by searching the YAML tree when the standard location is empty.
//
// This function:
// 1. searches YAML tree for the property key in various locations
// 2. updates Line, Column, Reason, and other fields if found
//
// Returns true if enrichment was performed, false otherwise.
func enrichSchemaValidationFailure(
	failure *PropertyNameInfo,
	rootNode *yaml.Node,
	line *int,
	column *int,
	reason *string,
	fieldName *string,
	fieldPath *string,
	location *string,
	instancePath *[]string,
) bool {
	if failure == nil {
		return false
	}

	// search for the property key in the YAML tree with multiple fallback locations
	// since InstanceLocation may be empty for property name errors
	var foundNode *yaml.Node

	// try with the provided parent location first
	if len(failure.ParentLocation) > 0 {
		foundNode = findPropertyKeyNodeInYAML(rootNode, failure.PropertyName, failure.ParentLocation)
	}

	// common fallback locations for OpenAPI property name errors
	if foundNode == nil {
		foundNode = findPropertyKeyNodeInYAML(rootNode, failure.PropertyName, []string{"components", "schemas"})
	}
	if foundNode == nil {
		foundNode = findPropertyKeyNodeInYAML(rootNode, failure.PropertyName, []string{"components"})
	}
	if foundNode == nil {
		foundNode = findPropertyKeyNodeInYAML(rootNode, failure.PropertyName, []string{})
	}

	if foundNode == nil {
		return false
	}

	// populate location metadata from YAML node
	*line = foundNode.Line
	*column = foundNode.Column

	if failure.EnhancedReason != "" {
		*reason = failure.EnhancedReason
	}

	*fieldName = failure.PropertyName

	// construct JSONPath from parent location segments
	if len(failure.ParentLocation) > 0 {
		*fieldPath = helpers.ExtractJSONPathFromStringLocation("/" + strings.Join(failure.ParentLocation, "/") + "/" + failure.PropertyName)
		*location = "/" + strings.Join(failure.ParentLocation, "/")
		*instancePath = failure.ParentLocation
	} else {
		*fieldPath = helpers.ExtractJSONPathFromStringLocation("/" + failure.PropertyName)
		*location = "/"
		*instancePath = []string{}
	}

	return true
}
