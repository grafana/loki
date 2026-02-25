// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/utils"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	_ "embed"

	"github.com/pb33f/libopenapi-validator/config"
	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
)

// SchemaValidator is an interface that defines the methods for validating a *base.Schema (V3+ Only) object.
// There are 6 methods for validating a schema:
//
//	ValidateSchemaString accepts a schema object to validate against, and a JSON/YAML blob that is defined as a string.
//	ValidateSchemaObject accepts a schema object to validate against, and an object, created from unmarshalled JSON/YAML.
//	ValidateSchemaBytes accepts a schema object to validate against, and a JSON/YAML blob that is defined as a byte array.
//	ValidateSchemaStringWithVersion - version-aware validation that allows OpenAPI 3.0 keywords when version is specified.
//	ValidateSchemaObjectWithVersion - version-aware validation that allows OpenAPI 3.0 keywords when version is specified.
//	ValidateSchemaBytesWithVersion - version-aware validation that allows OpenAPI 3.0 keywords when version is specified.
type SchemaValidator interface {
	// ValidateSchemaString accepts a schema object to validate against, and a JSON/YAML blob that is defined as a string.
	// Uses OpenAPI 3.1+ validation by default (strict JSON Schema compliance).
	ValidateSchemaString(schema *base.Schema, payload string) (bool, []*liberrors.ValidationError)

	// ValidateSchemaObject accepts a schema object to validate against, and an object, created from unmarshalled JSON/YAML.
	// This is a pre-decoded object that will skip the need to unmarshal a string of JSON/YAML.
	// Uses OpenAPI 3.1+ validation by default (strict JSON Schema compliance).
	ValidateSchemaObject(schema *base.Schema, payload interface{}) (bool, []*liberrors.ValidationError)

	// ValidateSchemaBytes accepts a schema object to validate against, and a byte slice containing a schema to
	// validate against. Uses OpenAPI 3.1+ validation by default (strict JSON Schema compliance).
	ValidateSchemaBytes(schema *base.Schema, payload []byte) (bool, []*liberrors.ValidationError)

	// ValidateSchemaStringWithVersion accepts a schema object to validate against, a JSON/YAML blob, and an OpenAPI version.
	// When version is 3.0, OpenAPI 3.0-specific keywords like 'nullable' are allowed and processed.
	// When version is 3.1+, OpenAPI 3.0-specific keywords like 'nullable' will cause validation to fail.
	ValidateSchemaStringWithVersion(schema *base.Schema, payload string, version float32) (bool, []*liberrors.ValidationError)

	// ValidateSchemaObjectWithVersion accepts a schema object to validate against, an object, and an OpenAPI version.
	// When version is 3.0, OpenAPI 3.0-specific keywords like 'nullable' are allowed and processed.
	// When version is 3.1+, OpenAPI 3.0-specific keywords like 'nullable' will cause validation to fail.
	ValidateSchemaObjectWithVersion(schema *base.Schema, payload interface{}, version float32) (bool, []*liberrors.ValidationError)

	// ValidateSchemaBytesWithVersion accepts a schema object to validate against, a byte slice, and an OpenAPI version.
	// When version is 3.0, OpenAPI 3.0-specific keywords like 'nullable' are allowed and processed.
	// When version is 3.1+, OpenAPI 3.0-specific keywords like 'nullable' will cause validation to fail.
	ValidateSchemaBytesWithVersion(schema *base.Schema, payload []byte, version float32) (bool, []*liberrors.ValidationError)
}

var instanceLocationRegex = regexp.MustCompile(`^/(\d+)`)

type schemaValidator struct {
	options *config.ValidationOptions
	logger  *slog.Logger
	lock    sync.Mutex
}

// NewSchemaValidatorWithLogger will create a new SchemaValidator instance, ready to accept schemas and payloads to validate.
func NewSchemaValidatorWithLogger(logger *slog.Logger, opts ...config.Option) SchemaValidator {
	options := config.NewValidationOptions(opts...)

	return &schemaValidator{options: options, logger: logger, lock: sync.Mutex{}}
}

// NewSchemaValidator will create a new SchemaValidator instance, ready to accept schemas and payloads to validate.
func NewSchemaValidator(opts ...config.Option) SchemaValidator {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	return NewSchemaValidatorWithLogger(logger, opts...)
}

func (s *schemaValidator) ValidateSchemaString(schema *base.Schema, payload string) (bool, []*liberrors.ValidationError) {
	return s.validateSchemaWithVersion(schema, []byte(payload), nil, s.logger, 3.1)
}

func (s *schemaValidator) ValidateSchemaObject(schema *base.Schema, payload interface{}) (bool, []*liberrors.ValidationError) {
	return s.validateSchemaWithVersion(schema, nil, payload, s.logger, 3.1)
}

func (s *schemaValidator) ValidateSchemaBytes(schema *base.Schema, payload []byte) (bool, []*liberrors.ValidationError) {
	return s.validateSchemaWithVersion(schema, payload, nil, s.logger, 3.1)
}

func (s *schemaValidator) ValidateSchemaStringWithVersion(schema *base.Schema, payload string, version float32) (bool, []*liberrors.ValidationError) {
	return s.validateSchemaWithVersion(schema, []byte(payload), nil, s.logger, version)
}

func (s *schemaValidator) ValidateSchemaObjectWithVersion(schema *base.Schema, payload interface{}, version float32) (bool, []*liberrors.ValidationError) {
	return s.validateSchemaWithVersion(schema, nil, payload, s.logger, version)
}

func (s *schemaValidator) ValidateSchemaBytesWithVersion(schema *base.Schema, payload []byte, version float32) (bool, []*liberrors.ValidationError) {
	return s.validateSchemaWithVersion(schema, payload, nil, s.logger, version)
}

func (s *schemaValidator) validateSchemaWithVersion(schema *base.Schema, payload []byte, decodedObject interface{}, log *slog.Logger, version float32) (bool, []*liberrors.ValidationError) {
	var validationErrors []*liberrors.ValidationError

	if schema == nil {
		log.Info("schema is empty and cannot be validated. This generally means the schema is missing from the spec, or could not be read.")
		return false, validationErrors
	}

	var renderedSchema []byte

	// render the schema, to be used for validation, stop this from running concurrently, mutations are made to state
	// and, it will cause async issues.
	// Create isolated render context for this validation to prevent false positive cycle detection
	// when multiple validations run concurrently.
	// Use validation mode to force full inlining of discriminator refs - the JSON schema compiler
	// needs a self-contained schema without unresolved $refs.
	renderCtx := base.NewInlineRenderContextForValidation()
	s.lock.Lock()
	var e error
	renderedSchema, e = schema.RenderInlineWithContext(renderCtx)
	if e != nil {
		// schema cannot be rendered, so it's not valid!
		violation := &liberrors.SchemaValidationFailure{
			Reason:          e.Error(),
			Location:        "unavailable",
			ReferenceSchema: string(renderedSchema),
			ReferenceObject: string(payload),
		}
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType:         helpers.RequestBodyValidation,
			ValidationSubType:      helpers.Schema,
			Message:                "schema does not pass validation",
			Reason:                 fmt.Sprintf("The schema cannot be decoded: %s", e.Error()),
			SpecLine:               schema.GoLow().GetRootNode().Line,
			SpecCol:                schema.GoLow().GetRootNode().Column,
			SchemaValidationErrors: []*liberrors.SchemaValidationFailure{violation},
			HowToFix:               liberrors.HowToFixInvalidSchema,
			Context:                string(renderedSchema),
		})
		s.lock.Unlock()
		return false, validationErrors

	}
	s.lock.Unlock()

	jsonSchema, _ := utils.ConvertYAMLtoJSON(renderedSchema)

	if decodedObject == nil && len(payload) > 0 {
		err := json.Unmarshal(payload, &decodedObject)
		if err != nil {

			// cannot decode the request body, so it's not valid
			violation := &liberrors.SchemaValidationFailure{
				Reason:          err.Error(),
				Location:        "unavailable",
				ReferenceSchema: string(renderedSchema),
				ReferenceObject: string(payload),
			}
			line := 1
			col := 0
			if schema.GoLow().Type.KeyNode != nil {
				line = schema.GoLow().Type.KeyNode.Line
				col = schema.GoLow().Type.KeyNode.Column
			}
			validationErrors = append(validationErrors, &liberrors.ValidationError{
				ValidationType:         helpers.RequestBodyValidation,
				ValidationSubType:      helpers.Schema,
				Message:                "schema does not pass validation",
				Reason:                 fmt.Sprintf("The schema cannot be decoded: %s", err.Error()),
				SpecLine:               line,
				SpecCol:                col,
				SchemaValidationErrors: []*liberrors.SchemaValidationFailure{violation},
				HowToFix:               liberrors.HowToFixInvalidSchema,
				Context:                string(renderedSchema),
			})
			return false, validationErrors
		}

	}

	path := ""
	if schema.GoLow().GetIndex() != nil {
		path = schema.GoLow().GetIndex().GetSpecAbsolutePath()
	}
	jsch, err := helpers.NewCompiledSchemaWithVersion(path, jsonSchema, s.options, version)

	var schemaValidationErrors []*liberrors.SchemaValidationFailure
	if err != nil {
		violation := &liberrors.SchemaValidationFailure{
			Reason:          err.Error(),
			Location:        "schema compilation",
			ReferenceSchema: string(renderedSchema),
			ReferenceObject: string(payload),
		}
		line := 1
		col := 0
		if schema.GoLow().Type.KeyNode != nil {
			line = schema.GoLow().Type.KeyNode.Line
			col = schema.GoLow().Type.KeyNode.Column
		}
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType:         helpers.Schema,
			ValidationSubType:      helpers.Schema,
			Message:                "schema compilation failed",
			Reason:                 fmt.Sprintf("Schema compilation failed: %s", err.Error()),
			SpecLine:               line,
			SpecCol:                col,
			SchemaValidationErrors: []*liberrors.SchemaValidationFailure{violation},
			HowToFix:               liberrors.HowToFixInvalidSchema,
			Context:                string(renderedSchema),
		})
		return false, validationErrors
	}

	if jsch != nil && decodedObject != nil {
		scErrs := jsch.Validate(decodedObject)
		if scErrs != nil {

			var jk *jsonschema.ValidationError
			if errors.As(scErrs, &jk) {

				// flatten the validationErrors
				schFlatErr := jk.BasicOutput().Errors
				schemaValidationErrors = extractBasicErrors(schFlatErr, renderedSchema,
					decodedObject, payload, jk, schemaValidationErrors)
			}
			line := 1
			col := 0
			if schema.GoLow().Type.KeyNode != nil {
				line = schema.GoLow().Type.KeyNode.Line
				col = schema.GoLow().Type.KeyNode.Column
			}

			validationErrors = append(validationErrors, &liberrors.ValidationError{
				ValidationType:         helpers.Schema,
				Message:                "schema does not pass validation",
				Reason:                 "Schema failed to validate against the contract requirements",
				SpecLine:               line,
				SpecCol:                col,
				SchemaValidationErrors: schemaValidationErrors,
				HowToFix:               liberrors.HowToFixInvalidSchema,
				Context:                string(renderedSchema),
			})
		}
	}
	if len(validationErrors) > 0 {
		return false, validationErrors
	}
	return true, nil
}

func extractBasicErrors(schFlatErrs []jsonschema.OutputUnit,
	renderedSchema []byte, decodedObject interface{},
	payload []byte, jk *jsonschema.ValidationError,
	schemaValidationErrors []*liberrors.SchemaValidationFailure,
) []*liberrors.SchemaValidationFailure {
	// Extract property name info once before processing errors (performance optimization)
	propertyInfo := extractPropertyNameFromError(jk)

	for q := range schFlatErrs {
		er := schFlatErrs[q]

		errMsg := er.Error.Kind.LocalizedString(message.NewPrinter(language.Tag{}))
		if helpers.IgnoreRegex.MatchString(errMsg) {
			continue // ignore this error, it's useless tbh, utter noise.
		}
		if er.Error != nil {

			// re-encode the schema.
			var renderedNode yaml.Node
			_ = yaml.Unmarshal(renderedSchema, &renderedNode)

			// locate the violated property in the schema
			located := LocateSchemaPropertyNodeByJSONPath(renderedNode.Content[0], er.KeywordLocation)

			// extract the element specified by the instance
			val := instanceLocationRegex.FindStringSubmatch(er.InstanceLocation)
			var referenceObject string

			if len(val) > 0 {
				referenceIndex, _ := strconv.Atoi(val[1])
				if reflect.ValueOf(decodedObject).Type().Kind() == reflect.Slice {
					found := decodedObject.([]any)[referenceIndex]
					recoded, _ := json.MarshalIndent(found, "", "  ")
					referenceObject = string(recoded)
				}
			}
			if referenceObject == "" {
				referenceObject = string(payload)
			}

			violation := &liberrors.SchemaValidationFailure{
				Reason:           errMsg,
				Location:         er.InstanceLocation,
				FieldName:        helpers.ExtractFieldNameFromStringLocation(er.InstanceLocation),
				FieldPath:        helpers.ExtractJSONPathFromStringLocation(er.InstanceLocation),
				InstancePath:     helpers.ConvertStringLocationToPathSegments(er.InstanceLocation),
				DeepLocation:     er.KeywordLocation,
				AbsoluteLocation: er.AbsoluteKeywordLocation,
				ReferenceSchema:  string(renderedSchema),
				ReferenceObject:  referenceObject,
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
				applyPropertyNameFallback(propertyInfo, renderedNode.Content[0], violation)
			}
			schemaValidationErrors = append(schemaValidationErrors, violation)
		}
	}
	return schemaValidationErrors
}
