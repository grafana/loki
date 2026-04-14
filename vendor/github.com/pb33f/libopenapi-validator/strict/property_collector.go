// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"regexp"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// declaredProperty holds information about a declared property in a schema.
type declaredProperty struct {
	// proxy is the SchemaProxy for the property.
	proxy *base.SchemaProxy
}

// collectDeclaredProperties gathers all property names that are declared in a schema.
// This includes explicit properties, patternProperties matches, and properties from
// dependentSchemas and if/then/else based on the actual data.
//
// Returns a map from property name to its declaration info, plus a slice of
// pattern regexes for patternProperties matching.
func (v *Validator) collectDeclaredProperties(
	schema *base.Schema,
	data map[string]any,
) (declared map[string]*declaredProperty, patterns []*regexp.Regexp) {
	declared = make(map[string]*declaredProperty)

	if schema == nil {
		return declared, nil
	}

	// explicit properties
	if schema.Properties != nil {
		for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
			declared[pair.Key()] = &declaredProperty{
				proxy: pair.Value(),
			}
		}
	}

	// pattern properties - use cached compiled patterns
	if schema.PatternProperties != nil {
		for pair := schema.PatternProperties.First(); pair != nil; pair = pair.Next() {
			pattern := v.getCompiledPattern(pair.Key())
			if pattern == nil {
				continue
			}
			patterns = append(patterns, pattern)
		}
	}

	// dependent schemas - if trigger property exists in data
	if schema.DependentSchemas != nil {
		for pair := schema.DependentSchemas.First(); pair != nil; pair = pair.Next() {
			triggerProp := pair.Key()
			if _, exists := data[triggerProp]; !exists {
				continue
			}
			// trigger property exists, include dependent schema's properties
			mergePropertiesIntoDeclared(declared, pair.Value().Schema())
		}
	}

	// if/then/else
	if schema.If != nil {
		ifProxy := schema.If
		ifSchema := ifProxy.Schema()
		if ifSchema != nil {
			matches, err := v.dataMatchesSchema(ifSchema, data)
			if err != nil {
				// schema compilation failed - log and use else branch
				v.logger.Debug("strict: if schema compilation failed, using else branch", "error", err)
				matches = false
			}
			if matches {
				if schema.Then != nil {
					mergePropertiesIntoDeclared(declared, schema.Then.Schema())
				}
			} else {
				if schema.Else != nil {
					mergePropertiesIntoDeclared(declared, schema.Else.Schema())
				}
			}
		}
	}

	return declared, patterns
}

// mergePropertiesIntoDeclared merges properties from a schema's Properties map into
// the declared map. Only adds properties that are not already declared.
// This eliminates code duplication when collecting properties from multiple sources.
func mergePropertiesIntoDeclared(declared map[string]*declaredProperty, schema *base.Schema) {
	if schema == nil || schema.Properties == nil {
		return
	}
	for p := schema.Properties.First(); p != nil; p = p.Next() {
		if _, alreadyDeclared := declared[p.Key()]; !alreadyDeclared {
			declared[p.Key()] = &declaredProperty{
				proxy: p.Value(),
			}
		}
	}
}

// getDeclaredPropertyNames returns just the property names from declared properties.
func getDeclaredPropertyNames(declared map[string]*declaredProperty) []string {
	if len(declared) == 0 {
		return nil
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	return names
}

// isPropertyDeclared checks if a property name is declared in the schema.
// A property is declared if:
// - It's in the explicit properties map
// - It matches any patternProperties regex
func isPropertyDeclared(name string, declared map[string]*declaredProperty, patterns []*regexp.Regexp) bool {
	// check explicit properties
	if _, ok := declared[name]; ok {
		return true
	}

	// check pattern properties
	for _, pattern := range patterns {
		if pattern.MatchString(name) {
			return true
		}
	}

	return false
}

// getPropertySchema returns the SchemaProxy for a declared property.
// Returns nil if the property is not declared or is only matched by pattern.
func getPropertySchema(name string, declared map[string]*declaredProperty) *base.SchemaProxy {
	// check explicit properties first
	if dp, ok := declared[name]; ok && dp.proxy != nil {
		return dp.proxy
	}
	return nil
}

// shouldSkipProperty checks if a property should be skipped based on
// readOnly/writeOnly and the current validation direction.
func (v *Validator) shouldSkipProperty(schema *base.Schema, direction Direction) bool {
	if schema == nil {
		return false
	}

	// readOnly: skip in requests (should not be sent by client)
	if direction == DirectionRequest && schema.ReadOnly != nil && *schema.ReadOnly {
		return true
	}

	// writeOnly: skip in responses (should not be returned by server)
	if direction == DirectionResponse && schema.WriteOnly != nil && *schema.WriteOnly {
		return true
	}

	return false
}
