// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"errors"
	"fmt"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/utils"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/pb33f/libopenapi-validator/helpers"
)

// dataMatchesSchema checks if the given data matches the schema using
// JSON Schema validation. This is used for:
//   - oneOf/anyOf variant selection (finding which variant the data matches)
//   - if/then/else condition evaluation
//   - additionalProperties schema matching
//
// The method uses version-aware schema compilation to handle OpenAPI 3.0 vs 3.1
// differences (especially nullable handling).
//
// Returns (true, nil) if data matches the schema.
// Returns (false, nil) if data does not match the schema.
// Returns (false, error) if schema compilation failed.
func (v *Validator) dataMatchesSchema(schema *base.Schema, data any) (bool, error) {
	if schema == nil {
		return true, nil // No schema means anything matches
	}

	compiled, err := v.getCompiledSchema(schema)
	if err != nil {
		return false, err
	}
	return compiled.Validate(data) == nil, nil
}

// getCompiledSchema returns a compiled JSON Schema for the given high-level schema.
// It checks multiple cache levels:
// 1. Global SchemaCache (if configured in options)
// 2. Local instance cache (for reuse within this validation call)
// 3. Compiles on-the-fly if not cached
//
// Returns the compiled schema and nil error on success.
// Returns nil schema and nil error if the input schema is nil.
// Returns nil schema and error if compilation failed.
func (v *Validator) getCompiledSchema(schema *base.Schema) (*jsonschema.Schema, error) {
	if schema == nil || schema.GoLow() == nil {
		return nil, nil
	}

	hash := schema.GoLow().Hash()
	hashKey := fmt.Sprintf("%x", hash)

	// try global cache first (if available)
	if v.options != nil && v.options.SchemaCache != nil {
		if cached, ok := v.options.SchemaCache.Load(hash); ok && cached != nil && cached.CompiledSchema != nil {
			return cached.CompiledSchema, nil
		}
	}

	// try local instance cache
	if compiled, ok := v.localCache[hashKey]; ok {
		return compiled, nil
	}

	// cache miss - compile on-the-fly with context-aware rendering
	compiled, err := v.compileSchema(schema)
	if err != nil {
		return nil, err
	}
	if compiled != nil {
		v.localCache[hashKey] = compiled
	}

	return compiled, nil
}

// compileSchema renders and compiles a schema for validation.
// Uses RenderInlineWithContext for safe cycle handling.
//
// Returns the compiled schema and nil error on success.
// Returns nil schema and error if any step fails (render, conversion, compilation).
func (v *Validator) compileSchema(schema *base.Schema) (*jsonschema.Schema, error) {
	if schema == nil {
		return nil, nil
	}

	schemaHash := fmt.Sprintf("%x", schema.GoLow().Hash())

	// use RenderInlineWithContext for safe cycle handling
	renderedSchema, err := schema.RenderInlineWithContext(v.renderCtx)
	if err != nil {
		return nil, fmt.Errorf("strict: schema render failed (hash=%s): %w", schemaHash, err)
	}

	jsonSchema, convErr := utils.ConvertYAMLtoJSON(renderedSchema)
	if convErr != nil {
		return nil, fmt.Errorf("strict: YAML to JSON conversion failed: %w", convErr)
	}
	if len(jsonSchema) == 0 {
		return nil, errors.New("strict: schema rendered to empty JSON")
	}

	schemaName := fmt.Sprintf("strict-match-%s", schemaHash)
	compiled, err := helpers.NewCompiledSchemaWithVersion(
		schemaName,
		jsonSchema,
		v.options,
		v.version,
	)
	if err != nil {
		return nil, fmt.Errorf("strict: schema compilation failed (name=%s): %w", schemaName, err)
	}

	return compiled, nil
}
