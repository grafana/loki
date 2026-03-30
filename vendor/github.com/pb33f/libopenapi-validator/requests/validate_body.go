// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package requests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/paths"
	"github.com/pb33f/libopenapi-validator/schema_validation"
)

func (v *requestBodyValidator) ValidateRequestBody(request *http.Request) (bool, []*errors.ValidationError) {
	pathItem, errs, foundPath := paths.FindPath(request, v.document, v.options)
	if len(errs) > 0 {
		return false, errs
	}
	return v.ValidateRequestBodyWithPathItem(request, pathItem, foundPath)
}

func (v *requestBodyValidator) ValidateRequestBodyWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError) {
	if pathItem == nil {
		return false, []*errors.ValidationError{{
			ValidationType:    helpers.PathValidation,
			ValidationSubType: helpers.ValidationMissing,
			Message:           fmt.Sprintf("%s Path '%s' not found", request.Method, request.URL.Path),
			Reason: fmt.Sprintf("The %s request contains a path of '%s' "+
				"however that path, or the %s method for that path does not exist in the specification",
				request.Method, request.URL.Path, request.Method),
			SpecLine: -1,
			SpecCol:  -1,
			HowToFix: errors.HowToFixPath,
		}}
	}
	operation := helpers.ExtractOperation(request, pathItem)
	if operation == nil {
		return false, []*errors.ValidationError{errors.OperationNotFound(pathItem, request, request.Method, pathValue)}
	}
	if operation.RequestBody == nil {
		return true, nil
	}

	// extract the content type from the request
	contentType := request.Header.Get(helpers.ContentTypeHeader)
	required := false
	if operation.RequestBody.Required != nil {
		required = *operation.RequestBody.Required
	}
	if contentType == "" {
		if !required {
			// request body is not required, the validation stop there.
			return true, nil
		}
		return false, []*errors.ValidationError{errors.RequestContentTypeNotFound(operation, request, pathValue)}
	}

	// extract the media type from the content type header.
	mediaType, ok := v.extractContentType(contentType, operation)
	if !ok {
		return false, []*errors.ValidationError{errors.RequestContentTypeNotFound(operation, request, pathValue)}
	}

	// Nothing to validate
	if mediaType.Schema == nil {
		return true, nil
	}

	// extract schema from media type
	schema := mediaType.Schema.Schema()

	isJson := strings.Contains(strings.ToLower(contentType), helpers.JSONType)

	// we currently only support JSON, XML and URLEncoded validation for request bodies
	if !isJson {
		isXml := schema_validation.IsXMLContentType(contentType)
		isUrlEncoded := schema_validation.IsURLEncodedContentType(contentType)

		xmlValid := isXml && v.options.AllowXMLBodyValidation
		urlEncodedValid := isUrlEncoded && v.options.AllowURLEncodedBodyValidation

		if !xmlValid && !urlEncodedValid {
			return true, nil
		}

		if request != nil && request.Body != nil {
			requestBody, _ := io.ReadAll(request.Body)
			_ = request.Body.Close()

			stringedBody := string(requestBody)
			var jsonBody any
			var prevalidationErrors []*errors.ValidationError

			switch {
			case xmlValid:
				jsonBody, prevalidationErrors = schema_validation.TransformXMLToSchemaJSON(stringedBody, schema)
			case urlEncodedValid:
				jsonBody, prevalidationErrors = schema_validation.TransformURLEncodedToSchemaJSON(stringedBody, schema, mediaType.Encoding)
			}

			if len(prevalidationErrors) > 0 {
				return false, prevalidationErrors
			}

			transformedBytes, err := json.Marshal(jsonBody)
			if err != nil {
				switch {
				case isXml:
					return false, []*errors.ValidationError{errors.InvalidXMLParsing(err.Error(), stringedBody)}
				case isUrlEncoded:
					return false, []*errors.ValidationError{errors.InvalidURLEncodedParsing(err.Error(), stringedBody)}
				}
			}

			request.Body = io.NopCloser(bytes.NewBuffer(transformedBytes))
		}
	}

	validationSucceeded, validationErrors := ValidateRequestSchema(&ValidateRequestSchemaInput{
		Request: request,
		Schema:  schema,
		Version: helpers.VersionToFloat(v.document.Version),
		Options: []config.Option{config.WithExistingOpts(v.options)},
	})

	errors.PopulateValidationErrors(validationErrors, request, pathValue)

	return validationSucceeded, validationErrors
}

func (v *requestBodyValidator) extractContentType(contentType string, operation *v3.Operation) (*v3.MediaType, bool) {
	ct, _, _ := helpers.ExtractContentType(contentType)
	mediaType, ok := operation.RequestBody.Content.Get(ct)
	if ok {
		return mediaType, true
	}
	ctMediaRange := strings.SplitN(ct, "/", 2)
	for contentPair := operation.RequestBody.Content.First(); contentPair != nil; contentPair = contentPair.Next() {
		s := contentPair.Key()
		mediaTypeValue := contentPair.Value()
		opMediaRange := strings.SplitN(s, "/", 2)
		if (opMediaRange[0] == "*" || opMediaRange[0] == ctMediaRange[0]) &&
			(opMediaRange[1] == "*" || opMediaRange[1] == ctMediaRange[1]) {
			return mediaTypeValue, true
		}
	}
	return nil, false
}
