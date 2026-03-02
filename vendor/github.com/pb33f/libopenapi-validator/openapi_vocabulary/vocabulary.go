// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package openapi_vocabulary

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// OpenAPIVocabularyURL is the vocabulary URL for OpenAPI-specific keywords
const OpenAPIVocabularyURL = "https://pb33f.io/openapi-validator/vocabulary"

// VersionType represents OpenAPI specification versions
type VersionType int

const (
	// Version30 represents OpenAPI 3.0.x
	Version30 VersionType = iota
	Version31
	Version32
)

// NewOpenAPIVocabulary creates a vocabulary for OpenAPI-specific keywords
// version determines which keywords are allowed/forbidden
func NewOpenAPIVocabulary(version VersionType) *jsonschema.Vocabulary {
	return NewOpenAPIVocabularyWithCoercion(version, false)
}

// NewOpenAPIVocabularyWithCoercion creates a vocabulary with optional scalar coercion
func NewOpenAPIVocabularyWithCoercion(version VersionType, allowCoercion bool) *jsonschema.Vocabulary {
	return &jsonschema.Vocabulary{
		URL:    OpenAPIVocabularyURL,
		Schema: nil, // We don't validate the vocabulary schema itself
		Compile: func(ctx *jsonschema.CompilerContext, obj map[string]any) (jsonschema.SchemaExt, error) {
			return compileOpenAPIKeywords(ctx, obj, version, allowCoercion)
		},
	}
}

// compileOpenAPIKeywords compiles all OpenAPI-specific keywords found in the schema object
func compileOpenAPIKeywords(ctx *jsonschema.CompilerContext,
	obj map[string]any,
	version VersionType,
	allowCoercion bool,
) (jsonschema.SchemaExt, error) {
	var extensions []jsonschema.SchemaExt

	if ext, err := CompileNullable(ctx, obj, version); err != nil {
		return nil, err
	} else if ext != nil {
		extensions = append(extensions, ext)
	}

	if ext, err := CompileDiscriminator(ctx, obj, version); err != nil {
		return nil, err
	} else if ext != nil {
		extensions = append(extensions, ext)
	}

	if ext, err := CompileExample(ctx, obj, version); err != nil {
		return nil, err
	} else if ext != nil {
		extensions = append(extensions, ext)
	}

	if ext, err := CompileDeprecated(ctx, obj, version); err != nil {
		return nil, err
	} else if ext != nil {
		extensions = append(extensions, ext)
	}

	if ext, err := CompileCoercion(ctx, obj, allowCoercion); err != nil {
		return nil, err
	} else if ext != nil {
		extensions = append(extensions, ext)
	}

	if len(extensions) == 0 {
		return nil, nil
	}

	return &combinedExtension{extensions: extensions}, nil
}

// combinedExtension combines multiple OpenAPI extensions into one
type combinedExtension struct {
	extensions []jsonschema.SchemaExt
}

func (c *combinedExtension) Validate(ctx *jsonschema.ValidatorContext, v any) {
	for _, ext := range c.extensions {
		ext.Validate(ctx, v)
	}
}
