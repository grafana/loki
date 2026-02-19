// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package parameters

import (
	"net/http"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/errors"
)

// ParameterValidator is an interface that defines the methods for validating parameters
// There are 4 types of parameters: query, header, cookie and path.
//
//	ValidateQueryParams will validate the query parameters for the request
//	ValidateHeaderParams will validate the header parameters for the request
//	ValidateCookieParamsWithPathItem will validate the cookie parameters for the request
//	ValidatePathParams will validate the path parameters for the request
//
// Each method accepts an *http.Request and returns true if validation passed,
// false if validation failed and a slice of ValidationError pointers.
type ParameterValidator interface {
	// ValidateQueryParams accepts an *http.Request and validates the query parameters against the OpenAPI specification.
	// The method will locate the correct path, and operation, based on the verb. The parameters for the operation
	// will be matched and validated against what has been supplied in the http.Request query string.
	ValidateQueryParams(request *http.Request) (bool, []*errors.ValidationError)

	// ValidateQueryParamsWithPathItem accepts an *http.Request and validates the query parameters against the OpenAPI specification.
	// The method will locate the correct path, and operation, based on the verb. The parameters for the operation
	// will be matched and validated against what has been supplied in the http.Request query string.
	ValidateQueryParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)

	// ValidateHeaderParams validates the header parameters contained within *http.Request. It returns a boolean
	// stating true if validation passed (false for failed), and a slice of errors if validation failed.
	ValidateHeaderParams(request *http.Request) (bool, []*errors.ValidationError)

	// ValidateHeaderParamsWithPathItem validates the header parameters contained within *http.Request. It returns a boolean
	// stating true if validation passed (false for failed), and a slice of errors if validation failed.
	ValidateHeaderParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)

	// ValidateCookieParams validates the cookie parameters contained within *http.Request.
	// It returns a boolean stating true if validation passed (false for failed), and a slice of errors if validation failed.
	ValidateCookieParams(request *http.Request) (bool, []*errors.ValidationError)

	// ValidateCookieParamsWithPathItem validates the cookie parameters contained within *http.Request.
	// It returns a boolean stating true if validation passed (false for failed), and a slice of errors if validation failed.
	ValidateCookieParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)

	// ValidatePathParams validates the path parameters contained within *http.Request. It returns a boolean stating true
	// if validation passed (false for failed), and a slice of errors if validation failed.
	ValidatePathParams(request *http.Request) (bool, []*errors.ValidationError)

	// ValidatePathParamsWithPathItem validates the path parameters contained within *http.Request. It returns a boolean stating true
	// if validation passed (false for failed), and a slice of errors if validation failed.
	ValidatePathParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)

	// ValidateSecurity validates the security requirements for the operation. It returns a boolean stating true
	// if validation passed (false for failed), and a slice of errors if validation failed.
	ValidateSecurity(request *http.Request) (bool, []*errors.ValidationError)

	// ValidateSecurityWithPathItem validates the security requirements for the operation. It returns a boolean stating true
	// if validation passed (false for failed), and a slice of errors if validation failed.
	ValidateSecurityWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError)
}

// NewParameterValidator will create a new ParameterValidator from an OpenAPI 3+ document
func NewParameterValidator(document *v3.Document, opts ...config.Option) ParameterValidator {
	options := config.NewValidationOptions(opts...)

	return &paramValidator{options: options, document: document}
}

type paramValidator struct {
	options  *config.ValidationOptions
	document *v3.Document
}
