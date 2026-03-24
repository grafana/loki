// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package requests

import (
	"net/http"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/errors"
)

// RequestBodyValidator is an interface that defines the methods for validating request bodies for Operations.
//
//	ValidateRequestBodyWithPathItem method accepts an *http.Request and returns true if validation passed,
//	                    false if validation failed and a slice of ValidationError pointers.
type RequestBodyValidator interface {
	// ValidateRequestBody will validate the request body for an operation. The first return value will be true if the
	// request body is valid, false if it is not. The second return value will be a slice of ValidationError pointers if
	// the body is not valid.
	ValidateRequestBody(request *http.Request) (bool, []*errors.ValidationError)

	// ValidateRequestBodyWithPathItem will validate the request body for an operation. The first return value will be true if the
	// request body is valid, false if it is not. The second return value will be a slice of ValidationError pointers if
	// the body is not valid.
	ValidateRequestBodyWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)
}

// NewRequestBodyValidator will create a new RequestBodyValidator from an OpenAPI 3+ document
func NewRequestBodyValidator(document *v3.Document, opts ...config.Option) RequestBodyValidator {
	options := config.NewValidationOptions(opts...)

	return &requestBodyValidator{options: options, document: document}
}

type requestBodyValidator struct {
	options  *config.ValidationOptions
	document *v3.Document
}
