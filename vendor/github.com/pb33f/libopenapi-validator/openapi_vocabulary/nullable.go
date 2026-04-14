// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package openapi_vocabulary

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// compileNullable compiles the nullable keyword based on OpenAPI version
func CompileNullable(_ *jsonschema.CompilerContext, obj map[string]any, version VersionType) (jsonschema.SchemaExt, error) {
	v, exists := obj["nullable"]
	if !exists {
		return nil, nil
	}

	// check if nullable is used in OpenAPI 3.1+ (not allowed)
	if version == Version31 || version == Version32 {
		return nil, &OpenAPIKeywordError{
			Keyword: "nullable",
			Message: "The `nullable` keyword is not supported in OpenAPI 3.1+. Use `type: ['string', 'null']` instead",
		}
	}

	// validate that nullable is a boolean
	_, ok := v.(bool)
	if !ok {
		return nil, &OpenAPIKeywordError{
			Keyword: "nullable",
			Message: "nullable must be a boolean value",
		}
	}
	return nil, nil
}
