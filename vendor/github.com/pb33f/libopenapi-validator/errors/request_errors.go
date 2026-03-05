// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package errors

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/pb33f/libopenapi/orderedmap"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/helpers"
)

func RequestContentTypeNotFound(op *v3.Operation, request *http.Request, specPath string) *ValidationError {
	ct := request.Header.Get(helpers.ContentTypeHeader)
	var ctypes []string
	for pair := orderedmap.First(op.RequestBody.Content); pair != nil; pair = pair.Next() {
		ctypes = append(ctypes, pair.Key())
	}
	return &ValidationError{
		ValidationType:    helpers.RequestBodyValidation,
		ValidationSubType: helpers.RequestBodyContentType,
		Message: fmt.Sprintf("%s operation request content type '%s' does not exist",
			request.Method, ct),
		Reason: fmt.Sprintf("The content type '%s' of the %s request submitted has not "+
			"been defined, it's an unknown type", ct, request.Method),
		SpecLine:      op.RequestBody.GoLow().Content.KeyNode.Line,
		SpecCol:       op.RequestBody.GoLow().Content.KeyNode.Column,
		Context:       op,
		HowToFix:      fmt.Sprintf(HowToFixInvalidContentType, orderedmap.Len(op.RequestBody.Content), strings.Join(ctypes, ", ")),
		RequestPath:   request.URL.Path,
		RequestMethod: request.Method,
		SpecPath:      specPath,
	}
}

func OperationNotFound(pathItem *v3.PathItem, request *http.Request, method string, specPath string) *ValidationError {
	return &ValidationError{
		ValidationType:    helpers.RequestValidation,
		ValidationSubType: helpers.ValidationMissingOperation,
		Message: fmt.Sprintf("%s operation request content type '%s' does not exist",
			request.Method, method),
		Reason:        fmt.Sprintf("The path was found, but there was no '%s' method found in the spec", request.Method),
		SpecLine:      pathItem.GoLow().KeyNode.Line,
		SpecCol:       pathItem.GoLow().KeyNode.Column,
		Context:       pathItem,
		HowToFix:      HowToFixPathMethod,
		RequestPath:   request.URL.Path,
		RequestMethod: request.Method,
		SpecPath:      specPath,
	}
}
