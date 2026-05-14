// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
	"log/slog"
	"os"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/pb33f/libopenapi-validator/config"
	liberrors "github.com/pb33f/libopenapi-validator/errors"
)

// URLEncodedValidator is an interface that defines methods for validating URL encoded strings against OpenAPI schemas.
// There are 2 methods for validating URL encoded:
//
//	ValidateURLEncodedString validates an URL encoded string against a schema, applying OpenAPI object transformations.
//	ValidateURLEncodedStringWithVersion - version-aware URL encoded validation that allows OpenAPI 3.0 keywords when version is specified.
type URLEncodedValidator interface {
	// ValidateURLEncodedString validates an URL encoded string against a schema, applying OpenAPI object transformations.
	// Uses OpenAPI 3.1+ validation by default (strict JSON Schema compliance).
	ValidateURLEncodedString(schema *base.Schema, encoding *orderedmap.Map[string, *v3.Encoding], urlEncodedString string) (bool, []*liberrors.ValidationError)

	// ValidateURLEncodedStringWithVersion validates an URL encoded string with version-specific rules.
	// When version is 3.0, OpenAPI 3.0-specific keywords like 'nullable' are allowed and processed.
	// When version is 3.1+, OpenAPI 3.0-specific keywords like 'nullable' will cause validation to fail.
	ValidateURLEncodedStringWithVersion(schema *base.Schema, encoding *orderedmap.Map[string, *v3.Encoding], urlEncodedString string, version float32) (bool, []*liberrors.ValidationError)
}

type urlEncodedValidator struct {
	schemaValidator *schemaValidator
	logger          *slog.Logger
}

// NewURLEncodedValidatorWithLogger creates a new URLEncodedValidator instance with a custom logger.
func NewURLEncodedValidatorWithLogger(logger *slog.Logger, opts ...config.Option) URLEncodedValidator {
	options := config.NewValidationOptions(opts...)
	// Create an internal schema validator for JSON validation after URLEncoded transformation
	sv := &schemaValidator{options: options, logger: logger}
	return &urlEncodedValidator{schemaValidator: sv, logger: logger}
}

// NewURLEncodedValidator creates a new URLEncodedValidator instance with default logging configuration.
func NewURLEncodedValidator(opts ...config.Option) URLEncodedValidator {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	return NewURLEncodedValidatorWithLogger(logger, opts...)
}

func (x *urlEncodedValidator) ValidateURLEncodedString(schema *base.Schema, encoding *orderedmap.Map[string, *v3.Encoding], urlEncodedString string) (bool, []*liberrors.ValidationError) {
	return x.validateURLEncodedWithVersion(schema, encoding, urlEncodedString, x.logger, 3.1)
}

func (x *urlEncodedValidator) ValidateURLEncodedStringWithVersion(schema *base.Schema, encoding *orderedmap.Map[string, *v3.Encoding], urlEncodedString string, version float32) (bool, []*liberrors.ValidationError) {
	return x.validateURLEncodedWithVersion(schema, encoding, urlEncodedString, x.logger, version)
}
