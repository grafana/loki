// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// validateValue is the main entry point for validating a value against a schema.
// It dispatches to the appropriate handler based on the value type.
func (v *Validator) validateValue(ctx *traversalContext, schema *base.Schema, data any) []UndeclaredValue {
	if schema == nil || data == nil {
		return nil
	}

	if ctx.shouldIgnore() {
		return nil
	}

	if ctx.exceedsDepth() {
		return nil
	}

	// check for cycles using schema hash
	schemaKey := v.getSchemaKey(schema)
	if ctx.checkAndMarkVisited(schemaKey) {
		return nil
	}

	// switch on data type
	switch val := data.(type) {
	case map[string]any:
		return v.validateObject(ctx, schema, val)
	case []any:
		return v.validateArray(ctx, schema, val)
	default:
		return nil
	}
}

// validateObject checks an object value against a schema for undeclared properties.
func (v *Validator) validateObject(ctx *traversalContext, schema *base.Schema, data map[string]any) []UndeclaredValue {
	var undeclared []UndeclaredValue

	if len(schema.AllOf) > 0 || len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return v.validatePolymorphic(ctx, schema, data)
	}

	if !v.shouldReportUndeclared(schema) {
		// additionalProperties: false - base validation catches this, no strict check needed
		// Still need to recurse into declared properties
		return v.recurseIntoDeclaredProperties(ctx, schema, data)
	}

	declared, patterns := v.collectDeclaredProperties(schema, data)

	// check each property in the data
	for propName, propValue := range data {
		propPath := buildPath(ctx.path, propName)
		propCtx := ctx.withPath(propPath)

		if propCtx.shouldIgnore() {
			continue
		}

		if !isPropertyDeclared(propName, declared, patterns) {
			undeclared = append(undeclared,
				newUndeclaredProperty(propPath, propName, propValue, getDeclaredPropertyNames(declared), ctx.direction, schema))

			// even if undeclared, recurse into additionalProperties schema if present
			if schema.AdditionalProperties != nil && schema.AdditionalProperties.IsA() {
				addlProxy := schema.AdditionalProperties.A
				if addlProxy != nil {
					addlSchema := addlProxy.Schema()
					if addlSchema != nil {
						undeclared = append(undeclared, v.validateValue(propCtx, addlSchema, propValue)...)
					}
				}
			}
			continue
		}

		// property is declared, recurse into it
		propProxy := getPropertySchema(propName, declared)
		if propProxy == nil {
			propProxy = v.getPatternPropertySchema(schema, propName)
		}

		if propProxy != nil {
			propSchema := propProxy.Schema()
			if propSchema != nil {
				// check readOnly/writeOnly
				if v.shouldSkipProperty(propSchema, ctx.direction) {
					continue
				}
				undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
			}
		}
	}

	return undeclared
}

// shouldReportUndeclared determines if strict mode should report undeclared
// properties for this schema.
func (v *Validator) shouldReportUndeclared(schema *base.Schema) bool {
	if schema == nil {
		return false
	}

	// SHORT-CIRCUIT: If additionalProperties: false, base validation already catches extras.
	if schema.AdditionalProperties != nil && schema.AdditionalProperties.IsB() && !schema.AdditionalProperties.B {
		return false
	}

	// STRICT OVERRIDE: Even if additionalProperties: true, report undeclared.
	if schema.AdditionalProperties != nil {
		if schema.AdditionalProperties.IsB() && schema.AdditionalProperties.B {
			return true
		}
		if schema.AdditionalProperties.IsA() {
			// additionalProperties with schema - properties matching schema are
			// technically "declared" but we still want to flag them as not in
			// the explicit schema. They will be recursed into.
			return true
		}
	}

	// STRICT OVERRIDE: unevaluatedProperties: false with implicit additionalProperties: true
	// Standard JSON Schema would catch via unevaluatedProperties, but strict reports
	// even when additionalProperties: true would normally allow extras.
	if schema.UnevaluatedProperties != nil && schema.UnevaluatedProperties.IsB() && !schema.UnevaluatedProperties.B {
		// unevaluatedProperties: false means base validation catches extras
		// BUT if there's no additionalProperties: false, strict should report
		return true
	}

	// default: no additionalProperties means implicit true in JSON Schema
	// Strict reports undeclared in this case
	return true
}

// getPatternPropertySchema finds the schema for a property that matches
// a patternProperties regex. Uses cached compiled patterns.
func (v *Validator) getPatternPropertySchema(schema *base.Schema, propName string) *base.SchemaProxy {
	if schema.PatternProperties == nil {
		return nil
	}

	for pair := schema.PatternProperties.First(); pair != nil; pair = pair.Next() {
		pattern := v.getCompiledPattern(pair.Key())
		if pattern == nil {
			continue
		}
		if pattern.MatchString(propName) {
			return pair.Value()
		}
	}

	return nil
}

// recurseIntoDeclaredProperties recurses into declared properties without
// checking for undeclared (used when additionalProperties: false).
// This includes both explicit properties and patternProperties matches.
func (v *Validator) recurseIntoDeclaredProperties(ctx *traversalContext, schema *base.Schema, data map[string]any) []UndeclaredValue {
	var undeclared []UndeclaredValue

	processed := make(map[string]bool)

	// process explicit properties
	if schema.Properties != nil {
		for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
			propName := pair.Key()
			propProxy := pair.Value()

			propValue, exists := data[propName]
			if !exists {
				continue
			}

			processed[propName] = true

			propPath := buildPath(ctx.path, propName)
			propCtx := ctx.withPath(propPath)

			if propCtx.shouldIgnore() {
				continue
			}

			propSchema := propProxy.Schema()
			if propSchema != nil {
				if v.shouldSkipProperty(propSchema, ctx.direction) {
					continue
				}
				undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
			}
		}
	}

	// process patternProperties - recurse into any data properties that match patterns
	if schema.PatternProperties != nil {
		for propName, propValue := range data {
			if processed[propName] {
				continue
			}

			propProxy := v.getPatternPropertySchema(schema, propName)
			if propProxy == nil {
				continue
			}

			processed[propName] = true

			propPath := buildPath(ctx.path, propName)
			propCtx := ctx.withPath(propPath)

			if propCtx.shouldIgnore() {
				continue
			}

			propSchema := propProxy.Schema()
			if propSchema != nil {
				if v.shouldSkipProperty(propSchema, ctx.direction) {
					continue
				}
				undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
			}
		}
	}

	return undeclared
}
