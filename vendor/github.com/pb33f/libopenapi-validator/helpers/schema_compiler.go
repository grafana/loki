package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/openapi_vocabulary"
)

// ConfigureCompiler configures a JSON Schema compiler with the desired behavior.
func ConfigureCompiler(c *jsonschema.Compiler, o *config.ValidationOptions) {
	if o == nil {
		// Sanity
		return
	}

	// nil is the default so this is OK.
	c.UseRegexpEngine(o.RegexEngine)

	if o.FormatAssertions {
		c.AssertFormat()
	}

	if o.ContentAssertions {
		c.AssertContent()
	}

	for n, v := range o.Formats {
		c.RegisterFormat(&jsonschema.Format{
			Name:     n,
			Validate: v,
		})
	}
}

// NewCompilerWithOptions mints a new JSON schema compiler with custom configuration.
func NewCompilerWithOptions(o *config.ValidationOptions) *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	ConfigureCompiler(c, o)
	return c
}

// NewCompiledSchema establishes a programmatic representation of a JSON Schema document that is used for validation.
// Defaults to OpenAPI 3.1+ behavior (strict JSON Schema compliance).
func NewCompiledSchema(name string, jsonSchema []byte, o *config.ValidationOptions) (*jsonschema.Schema, error) {
	return NewCompiledSchemaWithVersion(name, jsonSchema, o, 3.1)
}

// NewCompiledSchemaWithVersion establishes a programmatic representation of a JSON Schema document that is used for validation.
// The version parameter determines which OpenAPI keywords are allowed:
// - version 3.0: Allows OpenAPI 3.0 keywords like 'nullable'
// - version 3.1+: Rejects OpenAPI 3.0 keywords like 'nullable' (strict JSON Schema compliance)
func NewCompiledSchemaWithVersion(name string, jsonSchema []byte, options *config.ValidationOptions, version float32) (*jsonschema.Schema, error) {
	compiler := NewCompilerWithOptions(options)
	compiler.UseLoader(NewCompilerLoader())

	// register OpenAPI vocabulary with appropriate version and coercion settings
	if options != nil && options.OpenAPIMode {
		var vocabVersion openapi_vocabulary.VersionType
		if version >= 3.15 { // use 3.15 to avoid floating point precision issues (3.2+)
			vocabVersion = openapi_vocabulary.Version32
		} else if version >= 3.05 { // use 3.05 to avoid floating point precision issues (3.1)
			vocabVersion = openapi_vocabulary.Version31
		} else {
			vocabVersion = openapi_vocabulary.Version30
		}

		vocab := openapi_vocabulary.NewOpenAPIVocabularyWithCoercion(vocabVersion, options.AllowScalarCoercion)
		compiler.RegisterVocabulary(vocab)
		compiler.AssertVocabs()

		if version < 3.05 {
			jsonSchema = transformOpenAPI30Schema(jsonSchema)
		}

		if options.AllowScalarCoercion {
			jsonSchema = transformSchemaForCoercion(jsonSchema)
		}
	}

	decodedSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonSchema))
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON schema: %w", err)
	}

	if err = compiler.AddResource(name, decodedSchema); err != nil {
		return nil, fmt.Errorf("failed to add resource to schema compiler: %w", err)
	}

	jsch, err := compiler.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("JSON schema compile failed: %s", err.Error())
	}

	return jsch, nil
}

// transformOpenAPI30Schema transforms OpenAPI 3.0 schemas to JSON Schema 2020-12 compatible format.
// Handles OAS 3.0-specific keywords:
//   - nullable: true → type array with "null"
//   - exclusiveMinimum/exclusiveMaximum: bool → numeric (draft-04 → 2020-12)
func transformOpenAPI30Schema(jsonSchema []byte) []byte {
	var schema map[string]interface{}
	if err := json.Unmarshal(jsonSchema, &schema); err != nil {
		return jsonSchema
	}

	transformed := transformOAS30Keywords(schema)

	result, err := json.Marshal(transformed)
	if err != nil {
		return jsonSchema
	}

	return result
}

// transformOAS30Keywords recursively transforms OAS 3.0-specific keywords in a schema object
func transformOAS30Keywords(schema interface{}) interface{} {
	switch s := schema.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})

		// copy all properties first, recursing into nested schemas
		for key, value := range s {
			result[key] = transformOAS30Keywords(value)
		}

		// handle nullable keyword
		if nullable, ok := s["nullable"]; ok {
			if nullableBool, ok := nullable.(bool); ok {
				if nullableBool {
					result = transformNullableSchema(result)
				} else {
					delete(result, "nullable")
				}
			}
		}

		// handle exclusiveMinimum: bool → numeric
		transformExclusiveBound(result, "exclusiveMinimum", "minimum")

		// handle exclusiveMaximum: bool → numeric
		transformExclusiveBound(result, "exclusiveMaximum", "maximum")

		return result

	case []interface{}:
		result := make([]interface{}, len(s))
		for i, item := range s {
			result[i] = transformOAS30Keywords(item)
		}
		return result

	default:
		return schema
	}
}

// transformExclusiveBound converts OAS 3.0 boolean exclusiveMinimum/exclusiveMaximum
// to JSON Schema 2020-12 numeric form.
//
// OAS 3.0 (draft-04): minimum: 10, exclusiveMinimum: true → value must be > 10
// JSON Schema 2020-12: exclusiveMinimum: 10 → value must be > 10
func transformExclusiveBound(schema map[string]interface{}, exclusiveKey, boundKey string) {
	exVal, ok := schema[exclusiveKey]
	if !ok {
		return
	}
	exBool, isBool := exVal.(bool)
	if !isBool {
		return // already numeric (3.1 style), leave as-is
	}
	if exBool {
		// exclusiveMinimum: true + minimum: X → exclusiveMinimum: X (remove minimum)
		if bound, hasBound := schema[boundKey]; hasBound {
			schema[exclusiveKey] = bound
			delete(schema, boundKey)
		} else {
			// boolean true without a corresponding bound is invalid, just remove it
			delete(schema, exclusiveKey)
		}
	} else {
		// exclusiveMinimum: false is a no-op, just remove the keyword
		delete(schema, exclusiveKey)
	}
}

// transformNullableSchema transforms a schema with nullable: true to JSON Schema compatible format
func transformNullableSchema(schema map[string]interface{}) map[string]interface{} {
	delete(schema, "nullable")

	// get the current type
	currentType, hasType := schema["type"]

	if hasType {
		// if there's already a type, convert it to include null
		switch t := currentType.(type) {
		case string:
			// convert "string" to ["string", "null"]
			schema["type"] = []interface{}{t, "null"}
		case []interface{}:
			// if it's already an array, add null if not present
			found := false
			for _, item := range t {
				if str, ok := item.(string); ok && str == "null" {
					found = true
					break
				}
			}
			if !found {
				newTypes := make([]interface{}, len(t)+1)
				copy(newTypes, t)
				newTypes[len(t)] = "null"
				schema["type"] = newTypes
			}
		}
	}
	allOf, hasAllOf := schema["allOf"]
	if hasAllOf {
		delete(schema, "allOf")
		oneOfAdditions := []interface{}{
			map[string]interface{}{
				"allOf": allOf,
			},
			map[string]interface{}{
				"type": "null",
			},
		}
		var oneOfSlice []interface{}
		oneOf, hasOneOf := schema["oneOf"]
		if hasOneOf {
			oneOfSlice, _ = oneOf.([]interface{})
		}
		oneOfSlice = append(oneOfSlice, oneOfAdditions...)
		schema["oneOf"] = oneOfSlice
	}

	// Handle enum values - add null if nullable but not already in enum
	enum, hasEnum := schema["enum"]
	if hasEnum {
		if enumSlice, ok := enum.([]interface{}); ok {
			// Check if null is already in enum
			hasNull := false
			for _, v := range enumSlice {
				if v == nil {
					hasNull = true
					break
				}
			}
			// Add null if not present
			if !hasNull {
				enumSlice = append(enumSlice, nil)
				schema["enum"] = enumSlice
			}
		}
	}

	return schema
}

// transformSchemaForCoercion transforms schemas to allow scalar coercion (string->boolean/number)
func transformSchemaForCoercion(jsonSchema []byte) []byte {
	var schema map[string]interface{}
	if err := json.Unmarshal(jsonSchema, &schema); err != nil {
		// If we can't parse it, return as-is
		return jsonSchema
	}

	transformed := transformCoercionInSchema(schema)

	result, err := json.Marshal(transformed)
	if err != nil {
		return jsonSchema
	}

	return result
}

// transformCoercionInSchema recursively transforms schemas to support scalar coercion
func transformCoercionInSchema(schema interface{}) interface{} {
	switch s := schema.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})

		// copy all properties first
		for key, value := range s {
			result[key] = transformCoercionInSchema(value)
		}

		// transform type to allow string coercion for coercible types
		if schemaType, hasType := s["type"]; hasType {
			result["type"] = transformTypeForCoercion(schemaType)
		}

		return result

	case []interface{}:
		result := make([]interface{}, len(s))
		for i, item := range s {
			result[i] = transformCoercionInSchema(item)
		}
		return result

	default:
		return schema
	}
}

// transformTypeForCoercion transforms type fields to allow string coercion
func transformTypeForCoercion(schemaType interface{}) interface{} {
	switch t := schemaType.(type) {
	case string:
		// transform scalar types to include string for coercion
		if t == "boolean" || t == "number" || t == "integer" {
			return []interface{}{t, "string"}
		}
		return t

	case []interface{}:
		// if already an array, add string if it contains coercible types and doesn't already have string
		hasCoercibleType := false
		hasString := false

		for _, item := range t {
			if str, ok := item.(string); ok {
				if str == "boolean" || str == "number" || str == "integer" {
					hasCoercibleType = true
				}
				if str == "string" {
					hasString = true
				}
			}
		}

		if hasCoercibleType && !hasString {
			newTypes := make([]interface{}, len(t)+1)
			copy(newTypes, t)
			newTypes[len(t)] = "string"
			return newTypes
		}

		return t

	default:
		return schemaType
	}
}
