// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pb33f/libopenapi"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/pb33f/libopenapi-validator/config"
	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
)

func normalizeJSON(data any) any {
	d, _ := json.Marshal(data)
	var normalized any
	_ = json.Unmarshal(d, &normalized)
	return normalized
}

// ValidateOpenAPIDocument will validate an OpenAPI document against the OpenAPI 2, 3.0 and 3.1 schemas (depending on version)
// It will return true if the document is valid, false if it is not and a slice of ValidationError pointers.
func ValidateOpenAPIDocument(doc libopenapi.Document, opts ...config.Option) (bool, []*liberrors.ValidationError) {
	return ValidateOpenAPIDocumentWithPrecompiled(doc, nil, opts...)
}

// ValidateOpenAPIDocumentWithPrecompiled validates an OpenAPI document against the OAS JSON Schema.
// When compiledSchema is non-nil it is used directly, skipping schema compilation.
// When SpecJSONBytes is available on the document's SpecInfo, the normalizeJSON round-trip is
// bypassed in favour of a single jsonschema.UnmarshalJSON call.
func ValidateOpenAPIDocumentWithPrecompiled(doc libopenapi.Document, compiledSchema *jsonschema.Schema, opts ...config.Option) (bool, []*liberrors.ValidationError) {
	options := config.NewValidationOptions(opts...)

	info := doc.GetSpecInfo()
	loadedSchema := info.APISchema
	var validationErrors []*liberrors.ValidationError

	// Check if both JSON representations are nil before proceeding
	if info.SpecJSON == nil && info.SpecJSONBytes == nil {
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType:    helpers.Schema,
			ValidationSubType: "document",
			Message:           "OpenAPI document validation failed",
			Reason:            "The document's SpecJSON is nil, indicating the document was not properly parsed or is empty",
			SpecLine:          1,
			SpecCol:           0,
			HowToFix:          "ensure the OpenAPI document is valid YAML/JSON and can be properly parsed by libopenapi",
			Context:           "document root",
		})
		return false, validationErrors
	}

	// Use the precompiled schema if provided, otherwise compile it
	jsch := compiledSchema
	if jsch == nil {
		var err error
		jsch, err = helpers.NewCompiledSchema("schema", []byte(loadedSchema), options)
		if err != nil {
			validationErrors = append(validationErrors, &liberrors.ValidationError{
				ValidationType:    helpers.Schema,
				ValidationSubType: "compilation",
				Message:           "OpenAPI document schema compilation failed",
				Reason:            fmt.Sprintf("The OpenAPI schema failed to compile: %s", err.Error()),
				SpecLine:          1,
				SpecCol:           0,
				HowToFix:          "check the OpenAPI schema for invalid JSON Schema syntax, complex regex patterns, or unsupported schema constructs",
				Context:           loadedSchema,
			})
			return false, validationErrors
		}
	}

	// Build the normalized document value for validation.
	// Prefer SpecJSONBytes (single unmarshal) over SpecJSON (marshal+unmarshal round-trip).
	var normalized any
	if info.SpecJSONBytes != nil && len(*info.SpecJSONBytes) > 0 {
		var err error
		normalized, err = jsonschema.UnmarshalJSON(bytes.NewReader(*info.SpecJSONBytes))
		if err != nil {
			// Fall back to normalizeJSON if UnmarshalJSON fails
			if info.SpecJSON != nil {
				normalized = normalizeJSON(*info.SpecJSON)
			}
		}
	} else if info.SpecJSON != nil {
		normalized = normalizeJSON(*info.SpecJSON)
	}

	// Validate the document
	scErrs := jsch.Validate(normalized)

	var schemaValidationErrors []*liberrors.SchemaValidationFailure

	if scErrs != nil {

		var jk *jsonschema.ValidationError
		if errors.As(scErrs, &jk) {

			// flatten the validationErrors
			schFlatErrs := jk.BasicOutput().Errors

			// Extract property name info once before processing errors (performance optimization)
			propertyInfo := extractPropertyNameFromError(jk)

			for q := range schFlatErrs {
				er := schFlatErrs[q]

				errMsg := er.Error.Kind.LocalizedString(message.NewPrinter(language.Tag{}))
				if er.KeywordLocation == "" || helpers.IgnorePolyRegex.MatchString(errMsg) {
					continue // ignore this error, it's useless tbh, utter noise.
				}
				if errMsg != "" {

					// locate the violated property in the schema
					located := LocateSchemaPropertyNodeByJSONPath(info.RootNode.Content[0], er.InstanceLocation)
					violation := &liberrors.SchemaValidationFailure{
						Reason:                  errMsg,
						FieldName:               helpers.ExtractFieldNameFromStringLocation(er.InstanceLocation),
						FieldPath:               helpers.ExtractJSONPathFromStringLocation(er.InstanceLocation),
						InstancePath:            helpers.ConvertStringLocationToPathSegments(er.InstanceLocation),
						KeywordLocation:         er.KeywordLocation,
						OriginalJsonSchemaError: jk,
					}

					// if we have a location within the schema, add it to the error
					if located != nil {
						line := located.Line
						// if the located node is a map or an array, then the actual human interpretable
						// line on which the violation occurred is the line of the key, not the value.
						if located.Kind == yaml.MappingNode || located.Kind == yaml.SequenceNode {
							if line > 0 {
								line--
							}
						}

						// location of the violation within the rendered schema.
						violation.Line = line
						violation.Column = located.Column
					} else {
						// handles property name validation errors that don't provide useful InstanceLocation
						applyPropertyNameFallback(propertyInfo, info.RootNode.Content[0], violation)
					}
					schemaValidationErrors = append(schemaValidationErrors, violation)
				}
			}
		}

		// add the error to the list
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType: helpers.Schema,
			Message:        "Document does not pass validation",
			Reason: fmt.Sprintf("OpenAPI document is not valid according "+
				"to the %s specification", info.Version),
			SchemaValidationErrors: schemaValidationErrors,
			HowToFix:               liberrors.HowToFixInvalidSchema,
		})
	}
	if len(validationErrors) > 0 {
		return false, validationErrors
	}
	return true, nil
}
