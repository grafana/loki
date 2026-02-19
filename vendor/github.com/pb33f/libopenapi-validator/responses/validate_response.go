// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/utils"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/pb33f/libopenapi-validator/cache"
	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/schema_validation"
	"github.com/pb33f/libopenapi-validator/strict"
)

var instanceLocationRegex = regexp.MustCompile(`^/(\d+)`)

// ValidateResponseSchemaInput contains parameters for response schema validation.
type ValidateResponseSchemaInput struct {
	Request  *http.Request   // Required: The HTTP request (for context)
	Response *http.Response  // Required: The HTTP response to validate
	Schema   *base.Schema    // Required: The OpenAPI schema to validate against
	Version  float32         // Required: OpenAPI version (3.0 or 3.1)
	Options  []config.Option // Optional: Functional options (defaults applied if empty/nil)
}

// ValidateResponseSchema will validate the response body for a http.Response pointer. The request is used to
// locate the operation in the specification, the response is used to ensure the response code, media type and the
// schema of the response body are valid.
//
// This function is used by the ValidateResponseBody function, but can be used independently.
// The schema will be compiled from cache if available, otherwise it will be compiled and cached.
func ValidateResponseSchema(input *ValidateResponseSchemaInput) (bool, []*errors.ValidationError) {
	validationOptions := config.NewValidationOptions(input.Options...)
	var validationErrors []*errors.ValidationError
	var renderedSchema, jsonSchema []byte
	var referenceSchema string
	var compiledSchema *jsonschema.Schema
	var cachedNode *yaml.Node

	if input.Schema == nil {
		return false, []*errors.ValidationError{{
			ValidationType:    helpers.ResponseBodyValidation,
			ValidationSubType: helpers.Schema,
			Message:           "schema is nil",
			Reason:            "The schema to validate against is nil",
		}}
	} else if input.Schema.GoLow() == nil {
		return false, []*errors.ValidationError{{
			ValidationType:    helpers.ResponseBodyValidation,
			ValidationSubType: helpers.Schema,
			Message:           "schema cannot be rendered",
			Reason:            "The schema does not have low-level information and cannot be rendered. Please ensure the schema is loaded from a document.",
		}}
	}

	if validationOptions.SchemaCache != nil {
		hash := input.Schema.GoLow().Hash()
		if cached, ok := validationOptions.SchemaCache.Load(hash); ok && cached != nil && cached.CompiledSchema != nil {
			renderedSchema = cached.RenderedInline
			referenceSchema = cached.ReferenceSchema
			compiledSchema = cached.CompiledSchema
			cachedNode = cached.RenderedNode
		}
	}

	// Cache miss or no cache - render and compile
	if compiledSchema == nil {
		renderCtx := base.NewInlineRenderContext()
		var renderErr error
		renderedSchema, renderErr = input.Schema.RenderInlineWithContext(renderCtx)
		referenceSchema = string(renderedSchema)

		// If rendering failed (e.g., circular reference), return the render error
		if renderErr != nil {
			violation := &errors.SchemaValidationFailure{
				Reason:          renderErr.Error(),
				Location:        "schema rendering",
				ReferenceSchema: referenceSchema,
			}
			validationErrors = append(validationErrors, &errors.ValidationError{
				ValidationType:    helpers.ResponseBodyValidation,
				ValidationSubType: helpers.Schema,
				Message: fmt.Sprintf("%d response body for '%s' failed schema rendering",
					input.Response.StatusCode, input.Request.URL.Path),
				Reason: fmt.Sprintf("The response schema for status code '%d' failed to render: %s",
					input.Response.StatusCode, renderErr.Error()),
				SpecLine:               1,
				SpecCol:                0,
				SchemaValidationErrors: []*errors.SchemaValidationFailure{violation},
				HowToFix:               "check the response schema for circular references or invalid structures",
				Context:                referenceSchema,
			})
			return false, validationErrors
		}

		jsonSchema, _ = utils.ConvertYAMLtoJSON(renderedSchema)

		var err error
		schemaName := fmt.Sprintf("%x", input.Schema.GoLow().Hash())
		compiledSchema, err = helpers.NewCompiledSchemaWithVersion(
			schemaName,
			jsonSchema,
			validationOptions,
			input.Version,
		)
		if err != nil {
			violation := &errors.SchemaValidationFailure{
				Reason:          fmt.Sprintf("failed to compile JSON schema: %s", err.Error()),
				Location:        "schema compilation",
				ReferenceSchema: referenceSchema,
			}
			validationErrors = append(validationErrors, &errors.ValidationError{
				ValidationType:    helpers.ResponseBodyValidation,
				ValidationSubType: helpers.Schema,
				Message: fmt.Sprintf("%d response body for '%s' failed schema compilation",
					input.Response.StatusCode, input.Request.URL.Path),
				Reason: fmt.Sprintf("The response schema for status code '%d' failed to compile: %s",
					input.Response.StatusCode, err.Error()),
				SpecLine:               1,
				SpecCol:                0,
				SchemaValidationErrors: []*errors.SchemaValidationFailure{violation},
				HowToFix:               "check the response schema for invalid JSON Schema syntax, complex regex patterns, or unsupported schema constructs",
				Context:                referenceSchema,
			})
			return false, validationErrors
		}

		if validationOptions.SchemaCache != nil {
			hash := input.Schema.GoLow().Hash()
			validationOptions.SchemaCache.Store(hash, &cache.SchemaCacheEntry{
				Schema:          input.Schema,
				RenderedInline:  renderedSchema,
				ReferenceSchema: referenceSchema,
				RenderedJSON:    jsonSchema,
				CompiledSchema:  compiledSchema,
			})
		}
	}

	request := input.Request
	response := input.Response
	schema := input.Schema

	if response == nil || response.Body == http.NoBody {
		// cannot decode the response body, so it's not valid
		violation := &errors.SchemaValidationFailure{
			Reason:          "response is empty",
			Location:        "unavailable",
			ReferenceSchema: referenceSchema,
		}
		validationErrors = append(validationErrors, &errors.ValidationError{
			ValidationType:    "response",
			ValidationSubType: "object",
			Message: fmt.Sprintf("%s response object is missing for '%s'",
				request.Method, request.URL.Path),
			Reason:                 "The response object is completely missing",
			SpecLine:               1,
			SpecCol:                0,
			SchemaValidationErrors: []*errors.SchemaValidationFailure{violation},
			HowToFix:               "ensure response object has been set",
			Context:                referenceSchema, // attach the rendered schema to the error
		})
		return false, validationErrors
	}

	responseBody, ioErr := io.ReadAll(response.Body)
	if ioErr != nil {
		// cannot decode the response body, so it's not valid
		violation := &errors.SchemaValidationFailure{
			Reason:          ioErr.Error(),
			Location:        "unavailable",
			ReferenceSchema: referenceSchema,
			ReferenceObject: string(responseBody),
		}
		validationErrors = append(validationErrors, &errors.ValidationError{
			ValidationType:    helpers.ResponseBodyValidation,
			ValidationSubType: helpers.Schema,
			Message: fmt.Sprintf("%s response body for '%s' cannot be read, it's empty or malformed",
				request.Method, request.URL.Path),
			Reason:                 fmt.Sprintf("The response body cannot be decoded: %s", ioErr.Error()),
			SpecLine:               1,
			SpecCol:                0,
			SchemaValidationErrors: []*errors.SchemaValidationFailure{violation},
			HowToFix:               "ensure body is not empty",
			Context:                referenceSchema, // attach the rendered schema to the error
		})
		return false, validationErrors
	}

	// close the request body, so it can be re-read later by another player in the chain
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewBuffer(responseBody))

	var decodedObj interface{}

	if len(responseBody) > 0 {
		err := json.Unmarshal(responseBody, &decodedObj)
		if err != nil {
			// cannot decode the response body, so it's not valid
			violation := &errors.SchemaValidationFailure{
				Reason:          err.Error(),
				Location:        "unavailable",
				ReferenceSchema: referenceSchema,
				ReferenceObject: string(responseBody),
			}
			validationErrors = append(validationErrors, &errors.ValidationError{
				ValidationType:    helpers.ResponseBodyValidation,
				ValidationSubType: helpers.Schema,
				Message: fmt.Sprintf("%s response body for '%s' failed to validate schema",
					request.Method, request.URL.Path),
				Reason:                 fmt.Sprintf("The response body cannot be decoded: %s", err.Error()),
				SpecLine:               1,
				SpecCol:                0,
				SchemaValidationErrors: []*errors.SchemaValidationFailure{violation},
				HowToFix:               errors.HowToFixInvalidSchema,
				Context:                referenceSchema, // attach the rendered schema to the error
			})
			return false, validationErrors
		}
	}

	// no response body? failed to decode anything? nothing to do here.
	if responseBody == nil || decodedObj == nil {
		return true, nil
	}

	// validate the object against the schema
	scErrs := compiledSchema.Validate(decodedObj)
	if scErrs != nil {
		jk := scErrs.(*jsonschema.ValidationError)

		// flatten the validationErrors
		schFlatErrs := jk.BasicOutput().Errors
		var schemaValidationErrors []*errors.SchemaValidationFailure

		renderedNode := cachedNode
		if renderedNode == nil {
			renderedNode = new(yaml.Node)
			_ = yaml.Unmarshal(renderedSchema, renderedNode)
		}

		for q := range schFlatErrs {
			er := schFlatErrs[q]

			errMsg := er.Error.Kind.LocalizedString(message.NewPrinter(language.Tag{}))
			if er.KeywordLocation == "" || helpers.IgnoreRegex.MatchString(errMsg) {
				continue // ignore this error, it's useless tbh, utter noise.
			}
			if er.Error != nil {
				// locate the violated property in the schema
				located := schema_validation.LocateSchemaPropertyNodeByJSONPath(renderedNode.Content[0], er.KeywordLocation)

				// extract the element specified by the instance
				val := instanceLocationRegex.FindStringSubmatch(er.InstanceLocation)
				var referenceObject string

				if len(val) > 0 {
					referenceIndex, _ := strconv.Atoi(val[1])
					if reflect.ValueOf(decodedObj).Type().Kind() == reflect.Slice {
						found := decodedObj.([]any)[referenceIndex]
						recoded, _ := json.MarshalIndent(found, "", "  ")
						referenceObject = string(recoded)
					}
				}
				if referenceObject == "" {
					referenceObject = string(responseBody)
				}

				violation := &errors.SchemaValidationFailure{
					Reason:          errMsg,
					Location:        er.KeywordLocation,
					FieldName:       helpers.ExtractFieldNameFromStringLocation(er.InstanceLocation),
					FieldPath:       helpers.ExtractJSONPathFromStringLocation(er.InstanceLocation),
					InstancePath:    helpers.ConvertStringLocationToPathSegments(er.InstanceLocation),
					ReferenceSchema: referenceSchema,
					ReferenceObject: referenceObject,
					OriginalError:   jk,
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
				}
				schemaValidationErrors = append(schemaValidationErrors, violation)
			}
		}

		line := 1
		col := 0
		if schema.GoLow().Type.KeyNode != nil {
			line = schema.GoLow().Type.KeyNode.Line
			col = schema.GoLow().Type.KeyNode.Column
		}

		// add the error to the list
		validationErrors = append(validationErrors, &errors.ValidationError{
			ValidationType:    helpers.ResponseBodyValidation,
			ValidationSubType: helpers.Schema,
			Message: fmt.Sprintf("%d response body for '%s' failed to validate schema",
				response.StatusCode, request.URL.Path),
			Reason: fmt.Sprintf("The response body for status code '%d' is defined as an object. "+
				"However, it does not meet the schema requirements of the specification", response.StatusCode),
			SpecLine:               line,
			SpecCol:                col,
			SchemaValidationErrors: schemaValidationErrors,
			HowToFix:               errors.HowToFixInvalidSchema,
			Context:                referenceSchema, // attach the rendered schema to the error
		})
	}
	if len(validationErrors) > 0 {
		return false, validationErrors
	}

	// strict mode: check for undeclared properties in response body
	if validationOptions.StrictMode && decodedObj != nil {
		strictValidator := strict.NewValidator(validationOptions, input.Version)
		strictResult := strictValidator.Validate(strict.Input{
			Schema:    schema,
			Data:      decodedObj,
			Direction: strict.DirectionResponse,
			Options:   validationOptions,
			BasePath:  "$.body",
			Version:   input.Version,
		})

		if !strictResult.Valid {
			for _, undeclared := range strictResult.UndeclaredValues {
				validationErrors = append(validationErrors,
					errors.UndeclaredPropertyError(
						undeclared.Path,
						undeclared.Name,
						undeclared.Value,
						undeclared.DeclaredProperties,
						undeclared.Direction.String(),
						request.URL.Path,
						request.Method,
						undeclared.SpecLine,
						undeclared.SpecCol,
					))
			}
		}
	}

	if len(validationErrors) > 0 {
		return false, validationErrors
	}
	return true, nil
}
