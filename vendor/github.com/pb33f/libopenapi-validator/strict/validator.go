// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"net/http"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/config"
)

// Validate performs strict validation on the input data against the schema.
// This is the main entry point for body validation.
//
// It detects undeclared properties even when additionalProperties: true
// would normally allow them. This is useful for API governance scenarios
// where you want to ensure clients only send explicitly documented properties.
func (v *Validator) Validate(input Input) *Result {
	result := &Result{Valid: true}

	if input.Schema == nil || input.Data == nil {
		return result
	}

	ctx := newTraversalContext(input.Direction, v.compiledIgnorePaths, input.BasePath)

	undeclared := v.validateValue(ctx, input.Schema, input.Data)

	if len(undeclared) > 0 {
		result.Valid = false
		result.UndeclaredValues = undeclared
	}

	return result
}

// ValidateBody is a convenience method for validating request/response bodies.
func ValidateBody(schema *base.Schema, data any, direction Direction, options *config.ValidationOptions, version float32) *Result {
	v := NewValidator(options, version)
	return v.Validate(Input{
		Schema:    schema,
		Data:      data,
		Direction: direction,
		Options:   options,
		BasePath:  "$.body",
		Version:   version,
	})
}

// ValidateQueryParams checks for undeclared query parameters in an HTTP request.
// It compares the query parameters present in the request against those
// declared in the OpenAPI operation.
func ValidateQueryParams(
	request *http.Request,
	declaredParams []*v3.Parameter,
	options *config.ValidationOptions,
) []UndeclaredValue {
	if request == nil || options == nil || !options.StrictMode {
		return nil
	}

	v := NewValidator(options, 3.2)

	// build set of declared query params (case-sensitive)
	declared := make(map[string]bool)
	for _, param := range declaredParams {
		if param.In == "query" {
			declared[param.Name] = true
		}
	}

	var undeclared []UndeclaredValue

	// check each query parameter in the request
	for paramName := range request.URL.Query() {
		if !declared[paramName] {
			// build path using proper notation for special characters
			path := buildPath("$.query", paramName)
			if v.matchesIgnorePath(path) {
				continue
			}

			undeclared = append(undeclared,
				newUndeclaredParam(path, paramName, request.URL.Query().Get(paramName), "query", getParamNames(declaredParams, "query"), DirectionRequest))
		}
	}

	return undeclared
}

// ValidateRequestHeaders checks for undeclared headers in an HTTP request.
// Header names are normalized to lowercase for path generation and pattern matching.
//
// The securityHeaders parameter contains header names that are valid due to security
// scheme definitions (e.g., "X-API-Key" for apiKey schemes, "Authorization" for
// http/oauth2/openIdConnect schemes). These headers are considered "declared" even
// though they don't appear in the operation's parameters array.
func ValidateRequestHeaders(
	headers http.Header,
	declaredParams []*v3.Parameter,
	securityHeaders []string,
	options *config.ValidationOptions,
) []UndeclaredValue {
	if headers == nil || options == nil || !options.StrictMode {
		return nil
	}

	v := NewValidator(options, 3.2)

	// build set of declared headers (case-insensitive)
	declared := make(map[string]bool)
	for _, param := range declaredParams {
		if param.In == "header" {
			declared[strings.ToLower(param.Name)] = true
		}
	}

	// add security scheme headers (case-insensitive)
	for _, h := range securityHeaders {
		declared[strings.ToLower(h)] = true
	}

	var undeclared []UndeclaredValue

	// check each header
	for headerName := range headers {
		lowerName := strings.ToLower(headerName)

		// skip if declared (via parameters or security schemes)
		if declared[lowerName] {
			continue
		}

		// skip if in ignored headers list
		if v.isHeaderIgnored(headerName, DirectionRequest) {
			continue
		}

		// build path using lowercase name for case-insensitive pattern matching
		path := buildPath("$.headers", lowerName)
		if v.matchesIgnorePath(path) {
			continue
		}

		undeclared = append(undeclared,
			newUndeclaredParam(path, headerName, headers.Get(headerName), "header", getParamNames(declaredParams, "header"), DirectionRequest))
	}

	return undeclared
}

// ValidateCookies checks for undeclared cookies in an HTTP request.
func ValidateCookies(
	request *http.Request,
	declaredParams []*v3.Parameter,
	options *config.ValidationOptions,
) []UndeclaredValue {
	if request == nil || options == nil || !options.StrictMode {
		return nil
	}

	v := NewValidator(options, 3.2)

	// build set of declared cookies
	declared := make(map[string]bool)
	for _, param := range declaredParams {
		if param.In == "cookie" {
			declared[param.Name] = true
		}
	}

	var undeclared []UndeclaredValue

	// check each cookie in the request
	for _, cookie := range request.Cookies() {
		if !declared[cookie.Name] {
			// build path using proper notation for special characters
			path := buildPath("$.cookies", cookie.Name)
			if v.matchesIgnorePath(path) {
				continue
			}

			undeclared = append(undeclared,
				newUndeclaredParam(path, cookie.Name, cookie.Value, "cookie", getParamNames(declaredParams, "cookie"), DirectionRequest))
		}
	}

	return undeclared
}

// getParamNames extracts parameter names of a specific type.
func getParamNames(params []*v3.Parameter, paramType string) []string {
	var names []string
	for _, param := range params {
		if param.In == paramType {
			names = append(names, param.Name)
		}
	}
	return names
}

// ValidateResponseHeaders checks for undeclared headers in an HTTP response.
// Uses the declared headers from the OpenAPI response object.
// Header names are normalized to lowercase for path generation and pattern matching.
func ValidateResponseHeaders(
	headers http.Header,
	declaredHeaders *map[string]*v3.Header,
	options *config.ValidationOptions,
) []UndeclaredValue {
	if headers == nil || options == nil || !options.StrictMode {
		return nil
	}

	v := NewValidator(options, 3.2)

	// build set of declared headers (case-insensitive)
	declared := make(map[string]bool)
	if declaredHeaders != nil {
		for name := range *declaredHeaders {
			declared[strings.ToLower(name)] = true
		}
	}

	var undeclared []UndeclaredValue
	var declaredNames []string
	if declaredHeaders != nil {
		for name := range *declaredHeaders {
			declaredNames = append(declaredNames, name)
		}
	}

	for headerName := range headers {
		lowerName := strings.ToLower(headerName)

		if declared[lowerName] {
			continue
		}

		if v.isHeaderIgnored(headerName, DirectionResponse) {
			continue
		}

		// build path using lowercase name for case-insensitive pattern matching
		path := buildPath("$.headers", lowerName)
		if v.matchesIgnorePath(path) {
			continue
		}

		undeclared = append(undeclared,
			newUndeclaredParam(path, headerName, headers.Get(headerName), "header", declaredNames, DirectionResponse))
	}

	return undeclared
}
