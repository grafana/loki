// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
	"log/slog"
	"os"

	"github.com/pb33f/libopenapi/datamodel/high/base"

	"github.com/pb33f/libopenapi-validator/config"
	liberrors "github.com/pb33f/libopenapi-validator/errors"
)

// XMLValidator is an interface that defines methods for validating XML against OpenAPI schemas.
// There are 2 methods for validating XML:
//
//	ValidateXMLString validates an XML string against a schema, applying OpenAPI xml object transformations.
//	ValidateXMLStringWithVersion - version-aware XML validation that allows OpenAPI 3.0 keywords when version is specified.
type XMLValidator interface {
	// ValidateXMLString validates an XML string against an OpenAPI schema, applying xml object transformations.
	// Uses OpenAPI 3.1+ validation by default (strict JSON Schema compliance).
	ValidateXMLString(schema *base.Schema, xmlString string) (bool, []*liberrors.ValidationError)

	// ValidateXMLStringWithVersion validates an XML string with version-specific rules.
	// When version is 3.0, OpenAPI 3.0-specific keywords like 'nullable' are allowed and processed.
	// When version is 3.1+, OpenAPI 3.0-specific keywords like 'nullable' will cause validation to fail.
	ValidateXMLStringWithVersion(schema *base.Schema, xmlString string, version float32) (bool, []*liberrors.ValidationError)
}

type xmlValidator struct {
	schemaValidator *schemaValidator
	logger          *slog.Logger
}

// NewXMLValidatorWithLogger creates a new XMLValidator instance with a custom logger.
func NewXMLValidatorWithLogger(logger *slog.Logger, opts ...config.Option) XMLValidator {
	options := config.NewValidationOptions(opts...)
	// Create an internal schema validator for JSON validation after XML transformation
	sv := &schemaValidator{options: options, logger: logger}
	return &xmlValidator{schemaValidator: sv, logger: logger}
}

// NewXMLValidator creates a new XMLValidator instance with default logging configuration.
func NewXMLValidator(opts ...config.Option) XMLValidator {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	return NewXMLValidatorWithLogger(logger, opts...)
}

func (x *xmlValidator) ValidateXMLString(schema *base.Schema, xmlString string) (bool, []*liberrors.ValidationError) {
	return x.validateXMLWithVersion(schema, xmlString, x.logger, 3.1)
}

func (x *xmlValidator) ValidateXMLStringWithVersion(schema *base.Schema, xmlString string, version float32) (bool, []*liberrors.ValidationError) {
	return x.validateXMLWithVersion(schema, xmlString, x.logger, version)
}
