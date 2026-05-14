// Copyright 2023-2026 Princess Beef Heavy Industries, LLC / Dave Shanley
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
	var contentMap *orderedmap.Map[string, *v3.MediaType]
	specLine, specCol := 1, 0
	if op.RequestBody != nil {
		contentMap = op.RequestBody.Content
		for pair := orderedmap.First(op.RequestBody.Content); pair != nil; pair = pair.Next() {
			ctypes = append(ctypes, pair.Key())
		}
		if low := op.RequestBody.GoLow(); low != nil && low.Content.KeyNode != nil {
			specLine = low.Content.KeyNode.Line
			specCol = low.Content.KeyNode.Column
		}
	}
	return &ValidationError{
		ValidationType:    helpers.RequestBodyValidation,
		ValidationSubType: helpers.RequestBodyContentType,
		Message: fmt.Sprintf("%s operation request content type '%s' does not exist",
			request.Method, ct),
		Reason: fmt.Sprintf("The content type '%s' of the %s request submitted has not "+
			"been defined, it's an unknown type", ct, request.Method),
		SpecLine:      specLine,
		SpecCol:       specCol,
		Context:       op,
		HowToFix:      fmt.Sprintf(HowToFixInvalidContentType, orderedmap.Len(contentMap), strings.Join(ctypes, ", ")),
		RequestPath:   request.URL.Path,
		RequestMethod: request.Method,
		SpecPath:      specPath,
	}
}

func OperationNotFound(pathItem *v3.PathItem, request *http.Request, method string, specPath string) *ValidationError {
	specLine, specCol := 1, 0
	if low := pathItem.GoLow(); low != nil && low.KeyNode != nil {
		specLine = low.KeyNode.Line
		specCol = low.KeyNode.Column
	}
	return &ValidationError{
		ValidationType:    helpers.RequestValidation,
		ValidationSubType: helpers.ValidationMissingOperation,
		Message: fmt.Sprintf("%s operation request content type '%s' does not exist",
			request.Method, method),
		Reason:        fmt.Sprintf("The path was found, but there was no '%s' method found in the spec", request.Method),
		SpecLine:      specLine,
		SpecCol:       specCol,
		Context:       pathItem,
		HowToFix:      HowToFixPathMethod,
		RequestPath:   request.URL.Path,
		RequestMethod: request.Method,
		SpecPath:      specPath,
	}
}
