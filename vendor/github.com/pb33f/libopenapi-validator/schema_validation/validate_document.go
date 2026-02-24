// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
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
	options := config.NewValidationOptions(opts...)

	info := doc.GetSpecInfo()
	loadedSchema := info.APISchema
	var validationErrors []*liberrors.ValidationError

	// Check if SpecJSON is nil before dereferencing
	if info.SpecJSON == nil {
		violation := &liberrors.SchemaValidationFailure{
			Reason:          "document SpecJSON is nil - document may not be properly parsed",
			Location:        "document root",
			ReferenceSchema: loadedSchema,
		}
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType:         "schema",
			ValidationSubType:      "document",
			Message:                "OpenAPI document validation failed",
			Reason:                 "The document's SpecJSON is nil, indicating the document was not properly parsed or is empty",
			SpecLine:               1,
			SpecCol:                0,
			SchemaValidationErrors: []*liberrors.SchemaValidationFailure{violation},
			HowToFix:               "ensure the OpenAPI document is valid YAML/JSON and can be properly parsed by libopenapi",
			Context:                "document root",
		})
		return false, validationErrors
	}

	decodedDocument := *info.SpecJSON

	// Compile the JSON Schema
	jsch, err := helpers.NewCompiledSchema("schema", []byte(loadedSchema), options)
	if err != nil {
		// schema compilation failed, return validation error instead of panicking
		violation := &liberrors.SchemaValidationFailure{
			Reason:          fmt.Sprintf("failed to compile OpenAPI schema: %s", err.Error()),
			Location:        "schema compilation",
			ReferenceSchema: loadedSchema,
		}
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType:         helpers.Schema,
			ValidationSubType:      "compilation",
			Message:                "OpenAPI document schema compilation failed",
			Reason:                 fmt.Sprintf("The OpenAPI schema failed to compile: %s", err.Error()),
			SpecLine:               1,
			SpecCol:                0,
			SchemaValidationErrors: []*liberrors.SchemaValidationFailure{violation},
			HowToFix:               "check the OpenAPI schema for invalid JSON Schema syntax, complex regex patterns, or unsupported schema constructs",
			Context:                loadedSchema,
		})
		return false, validationErrors
	}

	// Validate the document
	scErrs := jsch.Validate(normalizeJSON(decodedDocument))

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
						Reason:           errMsg,
						Location:         er.InstanceLocation,
						FieldName:        helpers.ExtractFieldNameFromStringLocation(er.InstanceLocation),
						FieldPath:        helpers.ExtractJSONPathFromStringLocation(er.InstanceLocation),
						InstancePath:     helpers.ConvertStringLocationToPathSegments(er.InstanceLocation),
						DeepLocation:     er.KeywordLocation,
						AbsoluteLocation: er.AbsoluteKeywordLocation,
						OriginalError:    jk,
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
