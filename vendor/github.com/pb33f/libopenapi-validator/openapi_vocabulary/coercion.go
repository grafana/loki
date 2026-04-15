// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package openapi_vocabulary

import (
	"regexp"
	"strconv"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// coercionExtension handles Jackson-style scalar coercion (string->boolean/number)
type coercionExtension struct {
	schemaType    any // string, []string, or nil
	allowCoercion bool
}

var (
	booleanRegex = regexp.MustCompile(`^(true|false)$`)
	numberRegex  = regexp.MustCompile(`^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$`)
	integerRegex = regexp.MustCompile(`^-?(?:0|[1-9]\d*)$`)
)

func (c *coercionExtension) Validate(ctx *jsonschema.ValidatorContext, v any) {
	if !c.allowCoercion {
		return // Coercion disabled - let normal validation handle it
	}

	str, ok := v.(string)
	if !ok {
		return // Not a string - let normal validation handle it
	}

	// check if we should coerce and validate the string format
	if c.shouldCoerceToBoolean() {
		if !c.isValidBooleanString(str) {
			ctx.AddError(&CoercionError{
				SourceType: "string",
				TargetType: "boolean",
				Value:      str,
				Message:    "string value cannot be coerced to boolean, must be 'true' or 'false'",
			})
		}
		return
	}

	if c.shouldCoerceToNumber() {
		if !c.isValidNumberString(str) {
			ctx.AddError(&CoercionError{
				SourceType: "string",
				TargetType: "number",
				Value:      str,
				Message:    "string value cannot be coerced to number, must be a valid numeric string",
			})
		}
		return
	}

	if c.shouldCoerceToInteger() {
		if !c.isValidIntegerString(str) {
			ctx.AddError(&CoercionError{
				SourceType: "string",
				TargetType: "integer",
				Value:      str,
				Message:    "string value cannot be coerced to integer, must be a valid integer string",
			})
		}
		return
	}
}

func (c *coercionExtension) shouldCoerceToBoolean() bool {
	return c.hasType("boolean")
}

func (c *coercionExtension) shouldCoerceToNumber() bool {
	return c.hasType("number")
}

func (c *coercionExtension) shouldCoerceToInteger() bool {
	return c.hasType("integer")
}

func (c *coercionExtension) hasType(targetType string) bool {
	switch t := c.schemaType.(type) {
	case string:
		return t == targetType
	case []any:
		for _, item := range t {
			if str, ok := item.(string); ok && str == targetType {
				return true
			}
		}
	}
	return false
}

func (c *coercionExtension) isValidBooleanString(s string) bool {
	return booleanRegex.MatchString(s)
}

func (c *coercionExtension) isValidNumberString(s string) bool {
	if !numberRegex.MatchString(s) {
		return false
	}
	// Additional validation using strconv
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func (c *coercionExtension) isValidIntegerString(s string) bool {
	if !integerRegex.MatchString(s) {
		return false
	}
	// Additional validation using strconv
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

// CompileCoercion compiles the coercion extension if coercion is allowed and applicable
func CompileCoercion(ctx *jsonschema.CompilerContext, obj map[string]any, allowCoercion bool) (jsonschema.SchemaExt, error) {
	if !allowCoercion {
		return nil, nil // Coercion disabled
	}

	// Get the type from the schema
	schemaType, hasType := obj["type"]
	if !hasType {
		return nil, nil // No type specified - no coercion needed
	}

	// Only apply coercion to scalar types
	if !IsCoercibleType(schemaType) {
		return nil, nil
	}

	return &coercionExtension{
		schemaType:    schemaType,
		allowCoercion: true,
	}, nil
}

// IsCoercibleType checks if the schema type is one that supports coercion
func IsCoercibleType(schemaType any) bool {
	switch t := schemaType.(type) {
	case string:
		return t == "boolean" || t == "number" || t == "integer"
	case []any:
		// for type arrays, check if any coercible type is present
		for _, item := range t {
			if str, ok := item.(string); ok {
				if str == "boolean" || str == "number" || str == "integer" {
					return true
				}
			}
		}
	}
	return false
}
