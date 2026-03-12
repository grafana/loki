// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package errors

import (
	"fmt"
	"strings"
)

// StrictValidationType is the validation type for strict mode errors.
const StrictValidationType = "strict"

// StrictValidationSubTypes for different kinds of undeclared values.
const (
	StrictSubTypeProperty = "undeclared-property"
	StrictSubTypeHeader   = "undeclared-header"
	StrictSubTypeQuery    = "undeclared-query-param"
	StrictSubTypeCookie   = "undeclared-cookie"
)

// UndeclaredPropertyError creates a ValidationError for an undeclared property.
func UndeclaredPropertyError(
	path string,
	name string,
	value any,
	declaredProperties []string,
	direction string,
	requestPath string,
	requestMethod string,
	specLine int,
	specCol int,
) *ValidationError {
	dirStr := direction
	if dirStr == "" {
		dirStr = "request"
	}

	return &ValidationError{
		ValidationType:    StrictValidationType,
		ValidationSubType: StrictSubTypeProperty,
		Message: fmt.Sprintf("%s property '%s' at '%s' is not declared in schema",
			dirStr, name, path),
		Reason: fmt.Sprintf("Strict mode: found property not in schema. "+
			"Declared properties: [%s]", strings.Join(declaredProperties, ", ")),
		HowToFix: fmt.Sprintf("Add '%s' to the schema, remove it from the %s, "+
			"or add '%s' to StrictIgnorePaths", name, dirStr, path),
		RequestPath:   requestPath,
		RequestMethod: requestMethod,
		ParameterName: name,
		Context:       truncateForContext(value),
		SpecLine:      specLine,
		SpecCol:       specCol,
	}
}

// UndeclaredHeaderError creates a ValidationError for an undeclared header.
func UndeclaredHeaderError(
	name string,
	value string,
	declaredHeaders []string,
	direction string,
	requestPath string,
	requestMethod string,
) *ValidationError {
	dirStr := direction
	if dirStr == "" {
		dirStr = "request"
	}

	return &ValidationError{
		ValidationType:    StrictValidationType,
		ValidationSubType: StrictSubTypeHeader,
		Message: fmt.Sprintf("%s header '%s' is not declared in specification",
			dirStr, name),
		Reason: fmt.Sprintf("Strict mode: found header not in spec. "+
			"Declared headers: [%s]", strings.Join(declaredHeaders, ", ")),
		HowToFix: fmt.Sprintf("Add '%s' to the operation's parameters, remove it from the %s, "+
			"or add it to StrictIgnoredHeaders", name, dirStr),
		RequestPath:   requestPath,
		RequestMethod: requestMethod,
		ParameterName: name,
		Context:       value,
	}
}

// UndeclaredQueryParamError creates a ValidationError for an undeclared query parameter.
func UndeclaredQueryParamError(
	path string,
	name string,
	value any,
	declaredParams []string,
	requestPath string,
	requestMethod string,
) *ValidationError {
	return &ValidationError{
		ValidationType:    StrictValidationType,
		ValidationSubType: StrictSubTypeQuery,
		Message:           fmt.Sprintf("query parameter '%s' at '%s' is not declared in specification", name, path),
		Reason: fmt.Sprintf("Strict mode: found query parameter not in spec. "+
			"Declared parameters: [%s]", strings.Join(declaredParams, ", ")),
		HowToFix: fmt.Sprintf("Add '%s' to the operation's query parameters, remove it from the request, "+
			"or add '%s' to StrictIgnorePaths", name, path),
		RequestPath:   requestPath,
		RequestMethod: requestMethod,
		ParameterName: name,
		Context:       truncateForContext(value),
	}
}

// UndeclaredCookieError creates a ValidationError for an undeclared cookie.
func UndeclaredCookieError(
	path string,
	name string,
	value any,
	declaredCookies []string,
	requestPath string,
	requestMethod string,
) *ValidationError {
	return &ValidationError{
		ValidationType:    StrictValidationType,
		ValidationSubType: StrictSubTypeCookie,
		Message:           fmt.Sprintf("cookie '%s' at '%s' is not declared in specification", name, path),
		Reason: fmt.Sprintf("Strict mode: found cookie not in spec. "+
			"Declared cookies: [%s]", strings.Join(declaredCookies, ", ")),
		HowToFix: fmt.Sprintf("Add '%s' to the operation's cookie parameters, remove it from the request, "+
			"or add '%s' to StrictIgnorePaths", name, path),
		RequestPath:   requestPath,
		RequestMethod: requestMethod,
		ParameterName: name,
		Context:       truncateForContext(value),
	}
}

// truncateForContext creates a truncated string representation for error context.
func truncateForContext(v any) string {
	switch val := v.(type) {
	case string:
		if len(val) > 50 {
			return val[:47] + "..."
		}
		return val
	case map[string]any:
		return "{...}"
	case []any:
		return "[...]"
	default:
		s := fmt.Sprintf("%v", v)
		if len(s) > 50 {
			return s[:47] + "..."
		}
		return s
	}
}
