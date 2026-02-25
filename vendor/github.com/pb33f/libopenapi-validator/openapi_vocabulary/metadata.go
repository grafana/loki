// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package openapi_vocabulary

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// exampleExtension handles the OpenAPI example keyword (metadata only)
type exampleExtension struct {
	example any
}

func (e *exampleExtension) Validate(ctx *jsonschema.ValidatorContext, v any) {
	// Example keyword is metadata only - no validation needed during runtime
}

// deprecatedExtension handles the OpenAPI deprecated keyword (metadata only)
type deprecatedExtension struct {
	deprecated bool
}

func (d *deprecatedExtension) Validate(ctx *jsonschema.ValidatorContext, v any) {
	// Deprecated keyword is metadata only - no validation needed during runtime
}

// compileExample compiles the example keyword
func CompileExample(ctx *jsonschema.CompilerContext, obj map[string]any, version VersionType) (jsonschema.SchemaExt, error) {
	v, exists := obj["example"]
	if !exists {
		return nil, nil
	}

	// Example can be any valid JSON value, so we just store it
	// The main validation is that it exists and is parseable (which it is if we got here)
	return &exampleExtension{example: v}, nil
}

// compileDeprecated compiles the deprecated keyword
func CompileDeprecated(ctx *jsonschema.CompilerContext, obj map[string]any, version VersionType) (jsonschema.SchemaExt, error) {
	v, exists := obj["deprecated"]
	if !exists {
		return nil, nil
	}

	// Validate that deprecated is a boolean
	deprecated, ok := v.(bool)
	if !ok {
		return nil, &OpenAPIKeywordError{
			Keyword: "deprecated",
			Message: "deprecated must be a boolean value",
		}
	}

	return &deprecatedExtension{deprecated: deprecated}, nil
}
