// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package validator

import (
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/cache"
	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/parameters"
	"github.com/pb33f/libopenapi-validator/paths"
	"github.com/pb33f/libopenapi-validator/requests"
	"github.com/pb33f/libopenapi-validator/responses"
	"github.com/pb33f/libopenapi-validator/schema_validation"
)

// Validator provides a coarse grained interface for validating an OpenAPI 3+ documents.
// There are three primary use-cases for validation
//
// Validating *http.Request objects against and OpenAPI 3+ document
// Validating *http.Response objects against an OpenAPI 3+ document
// Validating an OpenAPI 3+ document against the OpenAPI 3+ specification
type Validator interface {
	// ValidateHttpRequest will validate an *http.Request object against an OpenAPI 3+ document.
	// The path, query, cookie and header parameters and request body are validated.
	ValidateHttpRequest(request *http.Request) (bool, []*errors.ValidationError)
	// ValidateHttpRequestSync will validate an *http.Request object against an OpenAPI 3+ document synchronously and without spawning any goroutines.
	// The path, query, cookie and header parameters and request body are validated.
	ValidateHttpRequestSync(request *http.Request) (bool, []*errors.ValidationError)

	// ValidateHttpRequestWithPathItem will validate an *http.Request object against an OpenAPI 3+ document.
	// The path, query, cookie and header parameters and request body are validated.
	ValidateHttpRequestWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)

	// ValidateHttpRequestSyncWithPathItem will validate an *http.Request object against an OpenAPI 3+ document synchronously and without spawning any goroutines.
	// The path, query, cookie and header parameters and request body are validated.
	ValidateHttpRequestSyncWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)

	// ValidateHttpResponse will an *http.Response object against an OpenAPI 3+ document.
	// The response body is validated. The request is only used to extract the correct response from the spec.
	ValidateHttpResponse(request *http.Request, response *http.Response) (bool, []*errors.ValidationError)

	// ValidateHttpRequestResponse will validate both the *http.Request and *http.Response objects against an OpenAPI 3+ document.
	// The path, query, cookie and header parameters and request and response body are validated.
	ValidateHttpRequestResponse(request *http.Request, response *http.Response) (bool, []*errors.ValidationError)

	// ValidateDocument will validate an OpenAPI 3+ document against the 3.0 or 3.1 OpenAPI 3+ specification
	ValidateDocument() (bool, []*errors.ValidationError)

	// GetParameterValidator will return a parameters.ParameterValidator instance used to validate parameters
	GetParameterValidator() parameters.ParameterValidator

	// GetRequestBodyValidator will return a parameters.RequestBodyValidator instance used to validate request bodies
	GetRequestBodyValidator() requests.RequestBodyValidator

	// GetResponseBodyValidator will return a parameters.ResponseBodyValidator instance used to validate response bodies
	GetResponseBodyValidator() responses.ResponseBodyValidator

	// SetDocument will set the OpenAPI 3+ document to be validated
	SetDocument(document libopenapi.Document)
}

// NewValidator will create a new Validator from an OpenAPI 3+ document
func NewValidator(document libopenapi.Document, opts ...config.Option) (Validator, []error) {
	m, errs := document.BuildV3Model()
	if errs != nil {
		return nil, []error{errs}
	}
	v := NewValidatorFromV3Model(&m.Model, opts...)
	v.(*validator).document = document
	return v, nil
}

// NewValidatorFromV3Model will create a new Validator from an OpenAPI Model
func NewValidatorFromV3Model(m *v3.Document, opts ...config.Option) Validator {
	options := config.NewValidationOptions(opts...)

	v := &validator{options: options, v3Model: m}

	// create a new parameter validator
	v.paramValidator = parameters.NewParameterValidator(m, config.WithExistingOpts(options))

	// create aq new request body validator
	v.requestValidator = requests.NewRequestBodyValidator(m, config.WithExistingOpts(options))

	// create a response body validator
	v.responseValidator = responses.NewResponseBodyValidator(m, config.WithExistingOpts(options))

	// warm the schema caches by pre-compiling all schemas in the document
	// (warmSchemaCaches checks for nil cache and skips if disabled)
	warmSchemaCaches(m, options)

	return v
}

func (v *validator) SetDocument(document libopenapi.Document) {
	v.document = document
}

func (v *validator) GetParameterValidator() parameters.ParameterValidator {
	return v.paramValidator
}

func (v *validator) GetRequestBodyValidator() requests.RequestBodyValidator {
	return v.requestValidator
}

func (v *validator) GetResponseBodyValidator() responses.ResponseBodyValidator {
	return v.responseValidator
}

func (v *validator) ValidateDocument() (bool, []*errors.ValidationError) {
	if v.document == nil {
		return false, []*errors.ValidationError{{
			ValidationType:    helpers.DocumentValidation,
			ValidationSubType: helpers.ValidationMissing,
			Message:           "Document is not set",
			Reason:            "The document cannot be validated as it is not set",
			SpecLine:          1,
			SpecCol:           1,
			HowToFix:          "Set the document via `SetDocument` before validating",
		}}
	}
	var validationOpts []config.Option
	if v.options != nil {
		validationOpts = append(validationOpts, config.WithRegexEngine(v.options.RegexEngine))
	}
	return schema_validation.ValidateOpenAPIDocument(v.document, validationOpts...)
}

func (v *validator) ValidateHttpResponse(
	request *http.Request,
	response *http.Response,
) (bool, []*errors.ValidationError) {
	var pathItem *v3.PathItem
	var pathValue string
	var errs []*errors.ValidationError

	pathItem, errs, pathValue = paths.FindPath(request, v.v3Model, v.options.RegexCache)
	if pathItem == nil || errs != nil {
		return false, errs
	}

	responseBodyValidator := v.responseValidator

	// validate response
	_, responseErrors := responseBodyValidator.ValidateResponseBodyWithPathItem(request, response, pathItem, pathValue)

	if len(responseErrors) > 0 {
		return false, responseErrors
	}
	return true, nil
}

func (v *validator) ValidateHttpRequestResponse(
	request *http.Request,
	response *http.Response,
) (bool, []*errors.ValidationError) {
	var pathItem *v3.PathItem
	var pathValue string
	var errs []*errors.ValidationError

	pathItem, errs, pathValue = paths.FindPath(request, v.v3Model, v.options.RegexCache)
	if pathItem == nil || errs != nil {
		return false, errs
	}

	responseBodyValidator := v.responseValidator

	// validate request and response
	_, requestErrors := v.ValidateHttpRequestWithPathItem(request, pathItem, pathValue)
	_, responseErrors := responseBodyValidator.ValidateResponseBodyWithPathItem(request, response, pathItem, pathValue)

	if len(requestErrors) > 0 || len(responseErrors) > 0 {
		return false, append(requestErrors, responseErrors...)
	}
	return true, nil
}

func (v *validator) ValidateHttpRequest(request *http.Request) (bool, []*errors.ValidationError) {
	pathItem, errs, foundPath := paths.FindPath(request, v.v3Model, v.options.RegexCache)
	if len(errs) > 0 {
		return false, errs
	}
	return v.ValidateHttpRequestWithPathItem(request, pathItem, foundPath)
}

func (v *validator) ValidateHttpRequestWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError) {
	// create a new parameter validator
	paramValidator := v.paramValidator

	// create a new request body validator
	reqBodyValidator := v.requestValidator

	// create some channels to handle async validation
	doneChan := make(chan struct{})
	errChan := make(chan []*errors.ValidationError)
	controlChan := make(chan struct{})

	// async param validation function.
	parameterValidationFunc := func(control chan struct{}, errorChan chan []*errors.ValidationError) {
		paramErrs := make(chan []*errors.ValidationError)
		paramControlChan := make(chan struct{})
		paramFunctionControlChan := make(chan struct{})
		var paramValidationErrors []*errors.ValidationError

		validations := []validationFunction{
			paramValidator.ValidatePathParamsWithPathItem,
			paramValidator.ValidateCookieParamsWithPathItem,
			paramValidator.ValidateHeaderParamsWithPathItem,
			paramValidator.ValidateQueryParamsWithPathItem,
			paramValidator.ValidateSecurityWithPathItem,
		}

		// listen for validation errors on parameters. everything will run async.
		paramListener := func(control chan struct{}, errorChan chan []*errors.ValidationError) {
			completedValidations := 0
			for {
				select {
				case vErrs := <-errorChan:
					paramValidationErrors = append(paramValidationErrors, vErrs...)
				case <-control:
					completedValidations++
					if completedValidations == len(validations) {
						paramFunctionControlChan <- struct{}{}
						return
					}
				}
			}
		}

		validateParamFunction := func(
			control chan struct{},
			errorChan chan []*errors.ValidationError,
			validatorFunc validationFunction,
		) {
			valid, pErrs := validatorFunc(request, pathItem, pathValue)
			if !valid {
				errorChan <- pErrs
			}
			control <- struct{}{}
		}
		go paramListener(paramControlChan, paramErrs)
		for i := range validations {
			go validateParamFunction(paramControlChan, paramErrs, validations[i])
		}

		// wait for all the validations to complete
		<-paramFunctionControlChan
		if len(paramValidationErrors) > 0 {
			errorChan <- paramValidationErrors
		}

		// let runValidation know we are done with this part.
		controlChan <- struct{}{}
	}

	requestBodyValidationFunc := func(control chan struct{}, errorChan chan []*errors.ValidationError) {
		valid, pErrs := reqBodyValidator.ValidateRequestBodyWithPathItem(request, pathItem, pathValue)
		if !valid {
			errorChan <- pErrs
		}
		control <- struct{}{}
	}

	// build async functions
	asyncFunctions := []validationFunctionAsync{
		parameterValidationFunc,
		requestBodyValidationFunc,
	}

	var validationErrors []*errors.ValidationError

	// sit and wait for everything to report back.
	go runValidation(controlChan, doneChan, errChan, &validationErrors, len(asyncFunctions))

	// run async functions
	for i := range asyncFunctions {
		go asyncFunctions[i](controlChan, errChan)
	}

	// wait for all the validations to complete
	<-doneChan

	// sort errors for deterministic ordering (async validation can return errors in any order)
	sortValidationErrors(validationErrors)

	return len(validationErrors) == 0, validationErrors
}

func (v *validator) ValidateHttpRequestSync(request *http.Request) (bool, []*errors.ValidationError) {
	pathItem, errs, foundPath := paths.FindPath(request, v.v3Model, v.options.RegexCache)
	if len(errs) > 0 {
		return false, errs
	}
	return v.ValidateHttpRequestSyncWithPathItem(request, pathItem, foundPath)
}

func (v *validator) ValidateHttpRequestSyncWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError) {
	// create a new parameter validator
	paramValidator := v.paramValidator

	// create a new request body validator
	reqBodyValidator := v.requestValidator

	validationErrors := make([]*errors.ValidationError, 0)

	paramValidationErrors := make([]*errors.ValidationError, 0)
	for _, validateFunc := range []validationFunction{
		paramValidator.ValidatePathParamsWithPathItem,
		paramValidator.ValidateCookieParamsWithPathItem,
		paramValidator.ValidateHeaderParamsWithPathItem,
		paramValidator.ValidateQueryParamsWithPathItem,
		paramValidator.ValidateSecurityWithPathItem,
	} {
		valid, pErrs := validateFunc(request, pathItem, pathValue)
		if !valid {
			paramValidationErrors = append(paramValidationErrors, pErrs...)
		}
	}

	valid, pErrs := reqBodyValidator.ValidateRequestBodyWithPathItem(request, pathItem, pathValue)
	if !valid {
		paramValidationErrors = append(paramValidationErrors, pErrs...)
	}

	validationErrors = append(validationErrors, paramValidationErrors...)
	return len(validationErrors) == 0, validationErrors
}

type validator struct {
	options           *config.ValidationOptions
	v3Model           *v3.Document
	document          libopenapi.Document
	paramValidator    parameters.ParameterValidator
	requestValidator  requests.RequestBodyValidator
	responseValidator responses.ResponseBodyValidator
}

func runValidation(control, doneChan chan struct{},
	errorChan chan []*errors.ValidationError,
	validationErrors *[]*errors.ValidationError,
	total int,
) {
	var validationLock sync.Mutex
	completedValidations := 0
	for {
		select {
		case vErrs := <-errorChan:
			validationLock.Lock()
			*validationErrors = append(*validationErrors, vErrs...)
			validationLock.Unlock()
		case <-control:
			completedValidations++
			if completedValidations == total {
				doneChan <- struct{}{}
				return
			}
		}
	}
}

type (
	validationFunction      func(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)
	validationFunctionAsync func(control chan struct{}, errorChan chan []*errors.ValidationError)
)

// sortValidationErrors sorts validation errors for deterministic ordering.
// Errors are sorted by validation type first, then by message.
func sortValidationErrors(errs []*errors.ValidationError) {
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].ValidationType != errs[j].ValidationType {
			return errs[i].ValidationType < errs[j].ValidationType
		}
		return errs[i].Message < errs[j].Message
	})
}

// warmSchemaCaches pre-compiles all schemas in the OpenAPI document and stores them in the validator caches.
// This frontloads the compilation cost so that runtime validation doesn't need to compile schemas.
func warmSchemaCaches(
	doc *v3.Document,
	options *config.ValidationOptions,
) {
	// Skip warming if cache is nil (explicitly disabled via WithSchemaCache(nil))
	if doc == nil || doc.Paths == nil || doc.Paths.PathItems == nil || options.SchemaCache == nil {
		return
	}

	schemaCache := options.SchemaCache

	// Walk through all paths and operations
	for pathPair := doc.Paths.PathItems.First(); pathPair != nil; pathPair = pathPair.Next() {
		pathItem := pathPair.Value()

		// Get all operations for this path (handles all HTTP methods including OpenAPI 3.2+ extensions)
		operations := pathItem.GetOperations()
		if operations == nil {
			continue
		}

		for opPair := operations.First(); opPair != nil; opPair = opPair.Next() {
			operation := opPair.Value()
			if operation == nil {
				continue
			}

			// Warm request body schemas
			if operation.RequestBody != nil && operation.RequestBody.Content != nil {
				for contentPair := operation.RequestBody.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
					mediaType := contentPair.Value()
					if mediaType.Schema != nil {
						warmMediaTypeSchema(mediaType, schemaCache, options)
					}
				}
			}

			// Warm response body schemas
			if operation.Responses != nil {
				// Warm status code responses
				if operation.Responses.Codes != nil {
					for codePair := operation.Responses.Codes.First(); codePair != nil; codePair = codePair.Next() {
						response := codePair.Value()
						if response != nil && response.Content != nil {
							for contentPair := response.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
								mediaType := contentPair.Value()
								if mediaType.Schema != nil {
									warmMediaTypeSchema(mediaType, schemaCache, options)
								}
							}
						}
					}
				}

				// Warm default response schemas
				if operation.Responses.Default != nil && operation.Responses.Default.Content != nil {
					for contentPair := operation.Responses.Default.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
						mediaType := contentPair.Value()
						if mediaType.Schema != nil {
							warmMediaTypeSchema(mediaType, schemaCache, options)
						}
					}
				}
			}

			// Warm parameter schemas
			if operation.Parameters != nil {
				for _, param := range operation.Parameters {
					if param != nil {
						warmParameterSchema(param, schemaCache, options)
					}
				}
			}
		}

		// Warm path-level parameters
		if pathItem.Parameters != nil {
			for _, param := range pathItem.Parameters {
				if param != nil {
					warmParameterSchema(param, schemaCache, options)
				}
			}
		}
	}
}

// warmMediaTypeSchema warms the cache for a media type schema
func warmMediaTypeSchema(mediaType *v3.MediaType, schemaCache cache.SchemaCache, options *config.ValidationOptions) {
	if mediaType != nil && mediaType.Schema != nil {
		hash := mediaType.GoLow().Schema.Value.Hash()

		if _, exists := schemaCache.Load(hash); !exists {
			schema := mediaType.Schema.Schema()
			if schema != nil {
				renderCtx := base.NewInlineRenderContext()
				renderedInline, _ := schema.RenderInlineWithContext(renderCtx)
				referenceSchema := string(renderedInline)
				renderedJSON, _ := utils.ConvertYAMLtoJSON(renderedInline)
				if len(renderedInline) > 0 {
					compiledSchema, _ := helpers.NewCompiledSchema(fmt.Sprintf("%x", hash), renderedJSON, options)

					// Pre-parse YAML node for error reporting (avoids re-parsing on each error)
					var renderedNode yaml.Node
					_ = yaml.Unmarshal(renderedInline, &renderedNode)

					schemaCache.Store(hash, &cache.SchemaCacheEntry{
						Schema:          schema,
						RenderedInline:  renderedInline,
						ReferenceSchema: referenceSchema,
						RenderedJSON:    renderedJSON,
						CompiledSchema:  compiledSchema,
						RenderedNode:    &renderedNode,
					})
				}
			}
		}
	}
}

// warmParameterSchema warms the cache for a parameter schema
func warmParameterSchema(param *v3.Parameter, schemaCache cache.SchemaCache, options *config.ValidationOptions) {
	if param != nil {
		var schema *base.Schema
		var hash uint64

		// Parameters can have schemas in two places: schema property or content property
		if param.Schema != nil {
			schema = param.Schema.Schema()
			if schema != nil {
				hash = param.GoLow().Schema.Value.Hash()
			}
		} else if param.Content != nil {
			// Check content for schema
			for contentPair := param.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
				mediaType := contentPair.Value()
				if mediaType.Schema != nil {
					schema = mediaType.Schema.Schema()
					if schema != nil {
						hash = mediaType.GoLow().Schema.Value.Hash()
					}
					break // Only process first content type
				}
			}
		}

		if schema != nil {
			if _, exists := schemaCache.Load(hash); !exists {
				renderCtx := base.NewInlineRenderContext()
				renderedInline, _ := schema.RenderInlineWithContext(renderCtx)
				referenceSchema := string(renderedInline)
				renderedJSON, _ := utils.ConvertYAMLtoJSON(renderedInline)
				if len(renderedInline) > 0 {
					compiledSchema, _ := helpers.NewCompiledSchema(fmt.Sprintf("%x", hash), renderedJSON, options)

					// Pre-parse YAML node for error reporting (avoids re-parsing on each error)
					var renderedNode yaml.Node
					_ = yaml.Unmarshal(renderedInline, &renderedNode)

					// Store in cache using the shared SchemaCache type
					schemaCache.Store(hash, &cache.SchemaCacheEntry{
						Schema:          schema,
						RenderedInline:  renderedInline,
						ReferenceSchema: referenceSchema,
						RenderedJSON:    renderedJSON,
						CompiledSchema:  compiledSchema,
						RenderedNode:    &renderedNode,
					})
				}
			}
		}
	}
}
