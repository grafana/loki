// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package parameters

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	lowbase "github.com/pb33f/libopenapi/datamodel/low/base"

	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/paths"
	"github.com/pb33f/libopenapi-validator/strict"
)

func (v *paramValidator) ValidateHeaderParams(request *http.Request) (bool, []*errors.ValidationError) {
	pathItem, errs, foundPath := paths.FindPath(request, v.document, v.options.RegexCache)
	if len(errs) > 0 {
		return false, errs
	}
	return v.ValidateHeaderParamsWithPathItem(request, pathItem, foundPath)
}

func (v *paramValidator) ValidateHeaderParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError) {
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
	// extract params for the operation
	params := helpers.ExtractParamsForOperation(request, pathItem)

	var validationErrors []*errors.ValidationError
	seenHeaders := make(map[string]bool)
	for _, p := range params {
		if p.In == helpers.Header {

			seenHeaders[strings.ToLower(p.Name)] = true
			if param := request.Header.Get(p.Name); param != "" {

				var sch *base.Schema
				if p.Schema != nil {
					sch = p.Schema.Schema()
				}
				pType := sch.Type

				for _, ty := range pType {
					switch ty {
					case helpers.Integer:
						if _, err := strconv.ParseInt(param, 10, 64); err != nil {
							validationErrors = append(validationErrors,
								errors.InvalidHeaderParamInteger(p, strings.ToLower(param), sch))
							break
						}
						// check if the param is within the enum
						if sch.Enum != nil {
							matchFound := false
							for _, enumVal := range sch.Enum {
								if strings.TrimSpace(param) == fmt.Sprint(enumVal.Value) {
									matchFound = true
									break
								}
							}
							if !matchFound {
								validationErrors = append(validationErrors,
									errors.IncorrectCookieParamEnum(p, strings.ToLower(param), sch))
							}
						}

					case helpers.Number:
						if _, err := strconv.ParseFloat(param, 64); err != nil {
							validationErrors = append(validationErrors,
								errors.InvalidHeaderParamNumber(p, strings.ToLower(param), sch))
							break
						}
						// check if the param is within the enum
						if sch.Enum != nil {
							matchFound := false
							for _, enumVal := range sch.Enum {
								if strings.TrimSpace(param) == fmt.Sprint(enumVal.Value) {
									matchFound = true
									break
								}
							}
							if !matchFound {
								validationErrors = append(validationErrors,
									errors.IncorrectCookieParamEnum(p, strings.ToLower(param), sch))
							}
						}

					case helpers.Boolean:
						if _, err := strconv.ParseBool(param); err != nil {
							validationErrors = append(validationErrors,
								errors.IncorrectHeaderParamBool(p, strings.ToLower(param), sch))
						}

					case helpers.Object:

						// check if the header is default encoded or not
						var encodedObj map[string]interface{}
						// we have found our header, check the explode type.
						if p.IsDefaultHeaderEncoding() {
							encodedObj = helpers.ConstructMapFromCSV(param)
						} else {
							if p.IsExploded() { // only option is to be exploded for KV extraction.
								encodedObj = helpers.ConstructKVFromCSV(param)
							}
						}

						if len(encodedObj) == 0 {
							validationErrors = append(validationErrors,
								errors.HeaderParameterCannotBeDecoded(p, strings.ToLower(param)))
							break
						}

						// if a schema was extracted
						if sch != nil {
							validationErrors = append(validationErrors,
								ValidateParameterSchema(sch,
									encodedObj,
									"",
									"Header parameter",
									"The header parameter",
									p.Name,
									helpers.ParameterValidation,
									helpers.ParameterValidationQuery, v.options)...)
						}

					case helpers.Array:
						if !p.IsExploded() { // only unexploded arrays are supported for cookie params
							if sch.Items.IsA() {
								validationErrors = append(validationErrors,
									ValidateHeaderArray(sch, p, param)...)
							}
						}

					case helpers.String:

						// check if the schema has an enum, and if so, match the value against one of
						// the defined enum values.
						if sch.Enum != nil {
							matchFound := false
							for _, enumVal := range sch.Enum {
								if strings.TrimSpace(param) == fmt.Sprint(enumVal.Value) {
									matchFound = true
									break
								}
							}
							if !matchFound {
								validationErrors = append(validationErrors,
									errors.IncorrectHeaderParamEnum(p, strings.ToLower(param), sch))
								break
							}
						}
						validationErrors = append(validationErrors,
							ValidateSingleParameterSchema(
								sch,
								param,
								"Header parameter",
								"The header parameter",
								p.Name,
								helpers.ParameterValidation,
								helpers.ParameterValidationHeader,
								v.options,
							)...)
					}
				}
				if len(pType) == 0 {
					// validate schema as there is no type information.
					validationErrors = append(validationErrors, ValidateSingleParameterSchema(sch,
						param,
						p.Name,
						lowbase.SchemaLabel, p.Name, helpers.ParameterValidation, helpers.ParameterValidationHeader, v.options)...)
				}
			} else {
				if p.Required != nil && *p.Required {
					validationErrors = append(validationErrors, errors.HeaderParameterMissing(p))
				}
			}
		}
	}

	errors.PopulateValidationErrors(validationErrors, request, pathValue)

	if len(validationErrors) > 0 {
		return false, validationErrors
	}

	// strict mode: check for undeclared headers
	if v.options.StrictMode {
		// Extract security headers applicable to this operation
		var securityHeaders []string
		if v.document.Components != nil && v.document.Components.SecuritySchemes != nil {
			security := helpers.ExtractSecurityForOperation(request, pathItem)
			// Convert orderedmap to regular map for the helper
			schemesMap := make(map[string]*v3.SecurityScheme)
			for pair := v.document.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
				schemesMap[pair.Key()] = pair.Value()
			}
			securityHeaders = helpers.ExtractSecurityHeaderNames(security, schemesMap)
		}

		undeclaredHeaders := strict.ValidateRequestHeaders(request.Header, params, securityHeaders, v.options)
		for _, undeclared := range undeclaredHeaders {
			validationErrors = append(validationErrors,
				errors.UndeclaredHeaderError(
					undeclared.Name,
					undeclared.Value.(string),
					undeclared.DeclaredProperties,
					undeclared.Direction.String(),
					request.URL.Path,
					request.Method,
				))
		}
	}

	if len(validationErrors) > 0 {
		return false, validationErrors
	}
	return true, nil
}
