// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"regexp"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// validatePolymorphic handles allOf, oneOf, and anyOf schemas.
// For allOf: merge all schemas and validate against all.
// For oneOf/anyOf: find the matching variant and validate against it.
func (v *Validator) validatePolymorphic(ctx *traversalContext, schema *base.Schema, data map[string]any) []UndeclaredValue {
	var undeclared []UndeclaredValue

	// Handle allOf first - data must match ALL schemas
	if len(schema.AllOf) > 0 {
		undeclared = append(undeclared, v.validateAllOf(ctx, schema, data)...)
	}

	// Handle oneOf - data must match exactly ONE schema
	if len(schema.OneOf) > 0 {
		undeclared = append(undeclared, v.validateOneOf(ctx, schema, data)...)
	}

	// Handle anyOf - data must match at least ONE schema
	if len(schema.AnyOf) > 0 {
		undeclared = append(undeclared, v.validateAnyOf(ctx, schema, data)...)
	}

	// Also validate any direct properties on the parent schema
	if schema.Properties != nil {
		declared, patterns := v.collectDeclaredProperties(schema, data)

		// Check properties that aren't handled by allOf/oneOf/anyOf
		for propName := range data {
			// Skip if declared directly or via patterns
			if isPropertyDeclared(propName, declared, patterns) {
				continue
			}

			// Check if it's declared in any of the allOf schemas
			if v.isPropertyDeclaredInAllOf(schema.AllOf, propName) {
				continue
			}

			// For oneOf/anyOf, we've already validated against the matching variant
		}
	}

	return undeclared
}

// validateAllOf validates data against all schemas in allOf.
// Collects properties from all schemas as declared.
func (v *Validator) validateAllOf(ctx *traversalContext, schema *base.Schema, data map[string]any) []UndeclaredValue {
	var undeclared []UndeclaredValue

	// Collect declared properties from ALL schemas in allOf
	allDeclared := make(map[string]*declaredProperty)
	var allPatterns []*regexp.Regexp

	for _, schemaProxy := range schema.AllOf {
		if schemaProxy == nil {
			continue
		}

		subSchema := schemaProxy.Schema()
		if subSchema == nil {
			continue
		}

		declared, patterns := v.collectDeclaredProperties(subSchema, data)
		for name, prop := range declared {
			if _, exists := allDeclared[name]; !exists {
				allDeclared[name] = prop
			}
		}

		allPatterns = append(allPatterns, patterns...)
	}

	// collect from parent schema
	declared, patterns := v.collectDeclaredProperties(schema, data)
	for name, prop := range declared {
		if _, exists := allDeclared[name]; !exists {
			allDeclared[name] = prop
		}
	}

	allPatterns = append(allPatterns, patterns...)

	// check if strict mode should report for this combined schema
	if !v.shouldReportUndeclaredForAllOf(schema) {
		// Still recurse into declared properties
		return v.recurseIntoAllOfDeclaredProperties(ctx, schema.AllOf, data, allDeclared)
	}

	// Check each property in data
	for propName, propValue := range data {
		propPath := buildPath(ctx.path, propName)
		propCtx := ctx.withPath(propPath)

		if propCtx.shouldIgnore() {
			continue
		}

		// Check if declared in merged schema
		if isPropertyDeclared(propName, allDeclared, allPatterns) {
			// Recurse into the property
			propSchema := v.findPropertySchemaInAllOf(schema.AllOf, propName, allDeclared)
			if propSchema != nil {
				if v.shouldSkipProperty(propSchema, ctx.direction) {
					continue
				}
				undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
			}
			continue
		}

		// Not declared - report as undeclared
		undeclared = append(undeclared,
			newUndeclaredProperty(propPath, propName, propValue, getDeclaredPropertyNames(allDeclared), ctx.direction, schema))
	}

	return undeclared
}

// validateOneOf finds the matching oneOf variant and validates against it.
// Parent schema properties are merged with the variant's properties.
func (v *Validator) validateOneOf(ctx *traversalContext, schema *base.Schema, data map[string]any) []UndeclaredValue {
	var matchingVariant *base.Schema

	// discriminator is present, use it to select the variant
	if schema.Discriminator != nil {
		matchingVariant = v.selectByDiscriminator(schema, schema.OneOf, data)
	}

	// no discriminator or no match: find matching variant by validation
	if matchingVariant == nil {
		matchingVariant = v.findMatchingVariant(schema.OneOf, data)
	}

	if matchingVariant == nil {
		// No match found - base validation would report this error
		return nil
	}

	// Validate against variant, but filter out properties declared in parent
	return v.validateVariantWithParent(ctx, schema, matchingVariant, data)
}

// validateAnyOf finds matching anyOf variants and validates against them.
// Parent schema properties are merged with the variant's properties.
func (v *Validator) validateAnyOf(ctx *traversalContext, schema *base.Schema, data map[string]any) []UndeclaredValue {
	var matchingVariant *base.Schema

	// If discriminator is present, use it to select the variant
	if schema.Discriminator != nil {
		matchingVariant = v.selectByDiscriminator(schema, schema.AnyOf, data)
	}

	// No discriminator or no match: find matching variant by validation
	if matchingVariant == nil {
		matchingVariant = v.findMatchingVariant(schema.AnyOf, data)
	}

	if matchingVariant == nil {
		// No match found - base validation would report this error
		return nil
	}

	// Validate against variant, but filter out properties declared in parent
	return v.validateVariantWithParent(ctx, schema, matchingVariant, data)
}

// validateVariantWithParent validates data against a variant schema while also
// considering properties declared in the parent schema. This ensures parent
// properties are not reported as undeclared when using oneOf/anyOf.
func (v *Validator) validateVariantWithParent(ctx *traversalContext, parent *base.Schema, variant *base.Schema, data map[string]any) []UndeclaredValue {
	var undeclared []UndeclaredValue

	// Collect declared properties from parent schema
	parentDeclared, parentPatterns := v.collectDeclaredProperties(parent, data)

	// Collect declared properties from variant schema
	variantDeclared, variantPatterns := v.collectDeclaredProperties(variant, data)

	// Merge: parent + variant
	allDeclared := make(map[string]*declaredProperty)
	for name, prop := range parentDeclared {
		allDeclared[name] = prop
	}
	for name, prop := range variantDeclared {
		allDeclared[name] = prop
	}
	allPatterns := append(parentPatterns, variantPatterns...)

	// Check if we should report undeclared (skip if additionalProperties: false)
	if !v.shouldReportUndeclared(variant) && !v.shouldReportUndeclared(parent) {
		// Still recurse into declared properties
		return v.recurseIntoDeclaredPropertiesWithMerged(ctx, variant, parent, data, allDeclared)
	}

	// Check each property in data
	for propName, propValue := range data {
		propPath := buildPath(ctx.path, propName)
		propCtx := ctx.withPath(propPath)

		if propCtx.shouldIgnore() {
			continue
		}

		// Check if declared in merged schema (parent + variant)
		if isPropertyDeclared(propName, allDeclared, allPatterns) {
			// Find the property schema (prefer variant, fallback to parent)
			propSchema := v.findPropertySchemaInMerged(variant, parent, propName, allDeclared)
			if propSchema != nil {
				if v.shouldSkipProperty(propSchema, ctx.direction) {
					continue
				}
				undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
			}
			continue
		}

		// Not declared - report as undeclared
		// Use variant schema location if available, otherwise fall back to parent
		locationSchema := variant
		if locationSchema == nil || locationSchema.GoLow() == nil {
			locationSchema = parent
		}
		undeclared = append(undeclared,
			newUndeclaredProperty(propPath, propName, propValue, getDeclaredPropertyNames(allDeclared), ctx.direction, locationSchema))
	}

	return undeclared
}

// findPropertySchemaInMerged finds the schema for a property, preferring variant over parent.
// Checks explicit properties first, then patternProperties.
func (v *Validator) findPropertySchemaInMerged(variant, parent *base.Schema, propName string, declared map[string]*declaredProperty) *base.Schema {
	// Check explicit declared first
	if prop, ok := declared[propName]; ok && prop.proxy != nil {
		return prop.proxy.Schema()
	}

	// Check variant schema explicit properties
	if variant != nil && variant.Properties != nil {
		if propProxy, exists := variant.Properties.Get(propName); exists && propProxy != nil {
			return propProxy.Schema()
		}
	}

	// Check parent schema explicit properties
	if parent != nil && parent.Properties != nil {
		if propProxy, exists := parent.Properties.Get(propName); exists && propProxy != nil {
			return propProxy.Schema()
		}
	}

	// Check variant patternProperties
	if variant != nil {
		if propProxy := v.getPatternPropertySchema(variant, propName); propProxy != nil {
			return propProxy.Schema()
		}
	}

	// Check parent patternProperties
	if parent != nil {
		if propProxy := v.getPatternPropertySchema(parent, propName); propProxy != nil {
			return propProxy.Schema()
		}
	}

	return nil
}

// recurseIntoDeclaredPropertiesWithMerged recurses into properties from merged parent+variant.
func (v *Validator) recurseIntoDeclaredPropertiesWithMerged(ctx *traversalContext, variant, parent *base.Schema, data map[string]any, declared map[string]*declaredProperty) []UndeclaredValue {
	var undeclared []UndeclaredValue

	for propName, propValue := range data {
		propPath := buildPath(ctx.path, propName)
		propCtx := ctx.withPath(propPath)

		if propCtx.shouldIgnore() {
			continue
		}

		propSchema := v.findPropertySchemaInMerged(variant, parent, propName, declared)
		if propSchema != nil {
			if v.shouldSkipProperty(propSchema, ctx.direction) {
				continue
			}
			undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
		}
	}

	return undeclared
}

// selectByDiscriminator uses the discriminator to select the appropriate variant.
func (v *Validator) selectByDiscriminator(schema *base.Schema, variants []*base.SchemaProxy, data map[string]any) *base.Schema {
	if schema.Discriminator == nil {
		return nil
	}

	propName := schema.Discriminator.PropertyName
	if propName == "" {
		return nil
	}

	discriminatorValue, ok := data[propName]
	if !ok {
		return nil
	}

	valueStr, ok := discriminatorValue.(string)
	if !ok {
		return nil
	}

	// check mapping first
	if schema.Discriminator.Mapping != nil {
		for pair := schema.Discriminator.Mapping.First(); pair != nil; pair = pair.Next() {
			if pair.Key() == valueStr {
				// The mapping value is a reference like "#/components/schemas/Dog"
				mappedRef := pair.Value()
				for _, variantProxy := range variants {
					if variantProxy.IsReference() && variantProxy.GetReference() == mappedRef {
						return variantProxy.Schema()
					}
				}
			}
		}
	}

	// no mapping match, try to match by schema name in reference
	for _, variantProxy := range variants {
		if variantProxy.IsReference() {
			ref := variantProxy.GetReference()
			// Extract schema name from reference like "#/components/schemas/Dog"
			parts := strings.Split(ref, "/")
			if len(parts) > 0 && parts[len(parts)-1] == valueStr {
				return variantProxy.Schema()
			}
		}
	}

	return nil
}

// findMatchingVariant finds the first variant that the data validates against.
func (v *Validator) findMatchingVariant(variants []*base.SchemaProxy, data map[string]any) *base.Schema {
	for _, variantProxy := range variants {
		if variantProxy == nil {
			continue
		}

		variantSchema := variantProxy.Schema()
		if variantSchema == nil {
			continue
		}

		matches, _ := v.dataMatchesSchema(variantSchema, data)
		if matches {
			return variantSchema
		}
	}
	return nil
}

// isPropertyDeclaredInAllOf checks if a property is declared in any allOf schema.
func (v *Validator) isPropertyDeclaredInAllOf(allOf []*base.SchemaProxy, propName string) bool {
	for _, schemaProxy := range allOf {
		if schemaProxy == nil {
			continue
		}

		subSchema := schemaProxy.Schema()
		if subSchema == nil {
			continue
		}

		if subSchema.Properties != nil {
			if _, exists := subSchema.Properties.Get(propName); exists {
				return true
			}
		}
	}
	return false
}

// shouldReportUndeclaredForAllOf checks if any schema in allOf disables additional properties.
func (v *Validator) shouldReportUndeclaredForAllOf(schema *base.Schema) bool {
	// Check parent schema
	if schema.AdditionalProperties != nil && schema.AdditionalProperties.IsB() && !schema.AdditionalProperties.B {
		return false
	}

	// Check each allOf schema
	for _, schemaProxy := range schema.AllOf {
		if schemaProxy == nil {
			continue
		}

		subSchema := schemaProxy.Schema()
		if subSchema == nil {
			continue
		}

		if subSchema.AdditionalProperties != nil && subSchema.AdditionalProperties.IsB() && !subSchema.AdditionalProperties.B {
			return false
		}
	}

	return true
}

// findPropertySchemaInAllOf finds the schema for a property in allOf schemas.
func (v *Validator) findPropertySchemaInAllOf(allOf []*base.SchemaProxy, propName string, declared map[string]*declaredProperty) *base.Schema {
	// Check explicit declared first
	if prop, ok := declared[propName]; ok && prop.proxy != nil {
		return prop.proxy.Schema()
	}

	// Search in allOf schemas
	for _, schemaProxy := range allOf {
		if schemaProxy == nil {
			continue
		}

		subSchema := schemaProxy.Schema()
		if subSchema == nil {
			continue
		}

		if subSchema.Properties != nil {
			if propProxy, exists := subSchema.Properties.Get(propName); exists && propProxy != nil {
				return propProxy.Schema()
			}
		}
	}

	return nil
}

// recurseIntoAllOfDeclaredProperties recurses into properties without checking for undeclared.
func (v *Validator) recurseIntoAllOfDeclaredProperties(ctx *traversalContext, allOf []*base.SchemaProxy, data map[string]any, declared map[string]*declaredProperty) []UndeclaredValue {
	var undeclared []UndeclaredValue

	for propName, propValue := range data {
		propPath := buildPath(ctx.path, propName)
		propCtx := ctx.withPath(propPath)

		if propCtx.shouldIgnore() {
			continue
		}

		propSchema := v.findPropertySchemaInAllOf(allOf, propName, declared)
		if propSchema != nil {
			if v.shouldSkipProperty(propSchema, ctx.direction) {
				continue
			}
			undeclared = append(undeclared, v.validateValue(propCtx, propSchema, propValue)...)
		}
	}

	return undeclared
}
