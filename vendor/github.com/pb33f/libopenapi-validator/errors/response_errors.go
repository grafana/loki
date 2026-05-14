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

func ResponseContentTypeNotFound(op *v3.Operation,
	request *http.Request,
	response *http.Response,
	code string,
	isDefault bool,
) *ValidationError {
	ct := response.Header.Get(helpers.ContentTypeHeader)
	mediaTypeString, _, _ := helpers.ExtractContentType(ct)
	var ctypes []string
	specLine, specCol := 1, 0
	var contentMap *orderedmap.Map[string, *v3.MediaType]

	// check for a default type (applies to all codes without a match)
	if !isDefault {
		resp := op.Responses.Codes.GetOrZero(code)
		if resp != nil {
			for pair := orderedmap.First(resp.Content); pair != nil; pair = pair.Next() {
				ctypes = append(ctypes, pair.Key())
			}
			contentMap = resp.Content
			if low := resp.GoLow(); low != nil && low.Content.KeyNode != nil {
				specLine = low.Content.KeyNode.Line
				specCol = low.Content.KeyNode.Column
			}
		}
	} else {
		if op.Responses.Default != nil {
			for pair := orderedmap.First(op.Responses.Default.Content); pair != nil; pair = pair.Next() {
				ctypes = append(ctypes, pair.Key())
			}
			contentMap = op.Responses.Default.Content
			if low := op.Responses.Default.GoLow(); low != nil && low.Content.KeyNode != nil {
				specLine = low.Content.KeyNode.Line
				specCol = low.Content.KeyNode.Column
			}
		}
	}
	return &ValidationError{
		ValidationType:    helpers.ResponseBodyValidation,
		ValidationSubType: helpers.RequestBodyContentType,
		Message: fmt.Sprintf("%s / %s operation response content type '%s' does not exist",
			request.Method, code, mediaTypeString),
		Reason: fmt.Sprintf("The content type '%s' of the %s response received has not "+
			"been defined, it's an unknown type", mediaTypeString, request.Method),
		SpecLine: specLine,
		SpecCol:  specCol,
		Context:  op,
		HowToFix: fmt.Sprintf(HowToFixInvalidContentType,
			orderedmap.Len(contentMap), strings.Join(ctypes, ", ")),
	}
}

func ResponseCodeNotFound(op *v3.Operation, request *http.Request, code int) *ValidationError {
	specLine, specCol := 1, 0
	if low := op.GoLow(); low != nil && low.Responses.KeyNode != nil {
		specLine = low.Responses.KeyNode.Line
		specCol = low.Responses.KeyNode.Column
	}
	return &ValidationError{
		ValidationType:    helpers.ResponseBodyValidation,
		ValidationSubType: helpers.ResponseBodyResponseCode,
		Message: fmt.Sprintf("%s operation request response code '%d' does not exist",
			request.Method, code),
		Reason: fmt.Sprintf("The response code '%d' of the %s request submitted has not "+
			"been defined, it's an unknown type", code, request.Method),
		SpecLine: specLine,
		SpecCol:  specCol,
		Context:  op,
		HowToFix: HowToFixInvalidResponseCode,
	}
}
