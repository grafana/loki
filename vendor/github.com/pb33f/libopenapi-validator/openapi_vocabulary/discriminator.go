// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package openapi_vocabulary

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// discriminatorExtension handles the OpenAPI discriminator keyword
type discriminatorExtension struct {
	propertyName string
	mapping      map[string]string // value -> schema reference
}

// Validate validates the discriminator property exists in the instance
func (d *discriminatorExtension) Validate(ctx *jsonschema.ValidatorContext, v any) {
	obj, _ := v.(map[string]any)

	// check if discriminator property exists in the object
	if d.propertyName != "" {
		if _, exists := obj[d.propertyName]; !exists {
			ctx.AddError(&DiscriminatorPropertyMissingError{
				PropertyName: d.propertyName,
			})
		}
	}
}

// CompileDiscriminator compiles the OpenAPI discriminator keyword
func CompileDiscriminator(_ *jsonschema.CompilerContext, obj map[string]any, _ VersionType) (jsonschema.SchemaExt, error) {
	v, exists := obj["discriminator"]
	if !exists {
		return nil, nil
	}

	discriminator, ok := v.(map[string]any)
	if !ok {
		return nil, &OpenAPIKeywordError{
			Keyword: "discriminator",
			Message: "discriminator must be an object",
		}
	}

	propertyNameValue, exists := discriminator["propertyName"]
	if !exists {
		return nil, &OpenAPIKeywordError{
			Keyword: "discriminator",
			Message: "discriminator must have a propertyName field",
		}
	}

	propertyName, ok := propertyNameValue.(string)
	if !ok {
		return nil, &OpenAPIKeywordError{
			Keyword: "discriminator",
			Message: "discriminator propertyName must be a string",
		}
	}

	var mapping map[string]string
	if mappingValue, exists := discriminator["mapping"]; exists {
		mappingObj, ok := mappingValue.(map[string]any)
		if !ok {
			return nil, &OpenAPIKeywordError{
				Keyword: "discriminator",
				Message: "discriminator mapping must be an object",
			}
		}

		mapping = make(map[string]string)
		for key, value := range mappingObj {
			if strValue, ok := value.(string); ok {
				mapping[key] = strValue
			} else {
				return nil, &OpenAPIKeywordError{
					Keyword: "discriminator",
					Message: "discriminator mapping values must be strings",
				}
			}
		}
	}

	return &discriminatorExtension{
		propertyName: propertyName,
		mapping:      mapping,
	}, nil
}
