// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package parameters

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/paths"
	"github.com/pb33f/libopenapi-validator/strict"
)

const rx = `[:\/\?#\[\]\@!\$&'\(\)\*\+,;=]`

var rxRxp = regexp.MustCompile(rx)

func (v *paramValidator) ValidateQueryParams(request *http.Request) (bool, []*errors.ValidationError) {
	pathItem, errs, foundPath := paths.FindPath(request, v.document, v.options.RegexCache)
	if len(errs) > 0 {
		return false, errs
	}
	return v.ValidateQueryParamsWithPathItem(request, pathItem, foundPath)
}

func (v *paramValidator) ValidateQueryParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError) {
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
	queryParams := make(map[string][]*helpers.QueryParam)
	var validationErrors []*errors.ValidationError

	// build a set of spec parameter names for exact matching
	specParamNames := make(map[string]bool)
	for _, p := range params {
		if p.In == helpers.Query {
			specParamNames[p.Name] = true
		}
	}

	for qKey, qVal := range request.URL.Query() {
		// check if the query key exactly matches a spec parameter name (e.g., "match[]")
		// if so, store it literally without deepObject stripping
		if specParamNames[qKey] {
			queryParams[qKey] = append(queryParams[qKey], &helpers.QueryParam{
				Key:    qKey,
				Values: qVal,
			})
		} else if strings.IndexRune(qKey, '[') > 0 && strings.IndexRune(qKey, ']') > 0 {
			// check if the param is encoded as a property / deepObject
			stripped := qKey[:strings.IndexRune(qKey, '[')]
			value := qKey[strings.IndexRune(qKey, '[')+1 : strings.IndexRune(qKey, ']')]
			queryParams[stripped] = append(queryParams[stripped], &helpers.QueryParam{
				Key:      stripped,
				Values:   qVal,
				Property: value,
			})
		} else {
			queryParams[qKey] = append(queryParams[qKey], &helpers.QueryParam{
				Key:    qKey,
				Values: qVal,
			})
		}
	}

	// look through the params for the query key
doneLooking:
	for p := range params {
		if params[p].In == helpers.Query {

			contentWrapped := false
			var contentType string
			// check if this param is found as a set of query strings
			if jk, ok := queryParams[params[p].Name]; ok {
			skipValues:
				for _, fp := range jk {
					// let's check styles first.
					validationErrors = append(validationErrors, ValidateQueryParamStyle(params[p], jk)...)

					// there is a match, is the type correct
					// this context is extracted from the 3.1 spec to explain what is going on here:
					// For more complex scenarios, the content property can define the media type and schema of the
					// parameter. A parameter MUST contain either a schema property, or a content property, but not both.
					// The map MUST only contain one entry. (for content)
					var sch *base.Schema
					if params[p].Schema != nil {
						sch = params[p].Schema.Schema()
					} else {
						// ok, no schema, check for a content type
						for pair := orderedmap.First(params[p].Content); pair != nil; pair = pair.Next() {
							sch = pair.Value().Schema.Schema()
							contentWrapped = true
							contentType = pair.Key()
							break
						}
					}
					pType := sch.Type

					// for each param, check each type
					for _, ef := range fp.Values {

						// check allowReserved values. If this is set to true, then we can allow the
						// following characters
						//  :/?#[]@!$&'()*+,;=
						// to be present as they are, without being URLEncoded.
						if !params[p].AllowReserved {
							if rxRxp.MatchString(ef) && params[p].IsExploded() {
								validationErrors = append(validationErrors,
									errors.IncorrectReservedValues(params[p], ef, sch))
							}
						}
						for _, ty := range pType {
							switch ty {

							case helpers.String:
								validationErrors = append(validationErrors, v.validateSimpleParam(sch, ef, ef, params[p])...)
							case helpers.Integer:
								efF, err := strconv.ParseInt(ef, 10, 64)
								if err != nil {
									validationErrors = append(validationErrors,
										errors.InvalidQueryParamInteger(params[p], ef, sch))
									break
								}
								validationErrors = append(validationErrors, v.validateSimpleParam(sch, ef, efF, params[p])...)
							case helpers.Number:
								efF, err := strconv.ParseFloat(ef, 64)
								if err != nil {
									validationErrors = append(validationErrors,
										errors.InvalidQueryParamNumber(params[p], ef, sch))
									break
								}
								validationErrors = append(validationErrors, v.validateSimpleParam(sch, ef, efF, params[p])...)
							case helpers.Boolean:
								if _, err := strconv.ParseBool(ef); err != nil {
									validationErrors = append(validationErrors,
										errors.IncorrectQueryParamBool(params[p], ef, sch))
								}
							case helpers.Object:

								// check what style of encoding was used and then construct a map[string]interface{}
								// and pass that in as encoded JSON.
								var encodedObj map[string]interface{}

								switch params[p].Style {
								case helpers.DeepObject:
									encodedObj = helpers.ConstructParamMapFromDeepObjectEncoding(jk, sch)
								case helpers.PipeDelimited:
									encodedObj = helpers.ConstructParamMapFromPipeEncoding(jk)
								case helpers.SpaceDelimited:
									encodedObj = helpers.ConstructParamMapFromSpaceEncoding(jk)
								default:
									// form encoding is default.
									if contentWrapped {
										switch contentType {
										case helpers.JSONContentType:
											// we need to unmarshal the JSON into a map[string]interface{}
											encodedParams := make(map[string]interface{})
											encodedObj = make(map[string]interface{})
											if err := json.Unmarshal([]byte(ef), &encodedParams); err != nil {
												validationErrors = append(validationErrors,
													errors.IncorrectParamEncodingJSON(params[p], ef, sch))
												break skipValues
											}
											encodedObj[params[p].Name] = encodedParams
										}
									} else {
										encodedObj = helpers.ConstructParamMapFromFormEncodingArray(jk)
									}
								}

								numErrors := len(validationErrors)
								validationErrors = append(validationErrors,
									ValidateParameterSchema(sch, encodedObj[params[p].Name].(map[string]interface{}),
										ef,
										"Query parameter",
										"The query parameter",
										params[p].Name,
										helpers.ParameterValidation,
										helpers.ParameterValidationQuery, v.options)...)
								if len(validationErrors) > numErrors {
									// we've already added an error for this, so we can skip the rest of the values
									break skipValues
								}

							case helpers.Array:
								// well we're already in an array, so we need to check the items schema
								// to ensure this array items matches the type
								// only check if items is a schema, not a boolean
								if sch.Items != nil && sch.Items.IsA() {
									validationErrors = append(validationErrors,
										ValidateQueryArray(sch, params[p], ef, contentWrapped, v.options)...)
								}
							}
						}
					}
				}
			} else {
				// if the param is not in the requests, so let's check if this param is an
				// object, and if we should use default encoding and explode values.
				if params[p].Schema != nil {
					sch := params[p].Schema.Schema()

					if len(sch.Type) > 0 && sch.Type[0] == helpers.Object && params[p].IsDefaultFormEncoding() {
						// if the param is an object, and we're using default encoding, then we need to
						// validate the schema.
						decoded := helpers.ConstructParamMapFromQueryParamInput(queryParams)
						validationErrors = append(validationErrors,
							ValidateParameterSchema(sch,
								decoded,
								"",
								"Query array parameter",
								"The query parameter (which is an array)",
								params[p].Name,
								helpers.ParameterValidation,
								helpers.ParameterValidationQuery, v.options)...)
						break doneLooking
					}
				}
				// if there is no match, check if the param is required or not.
				if params[p].Required != nil && *params[p].Required {
					validationErrors = append(validationErrors, errors.QueryParameterMissing(params[p]))
				}
			}
		}
	}

	errors.PopulateValidationErrors(validationErrors, request, pathValue)

	if len(validationErrors) > 0 {
		return false, validationErrors
	}

	// strict mode: check for undeclared query parameters
	if v.options.StrictMode {
		undeclaredParams := strict.ValidateQueryParams(request, params, v.options)
		for _, undeclared := range undeclaredParams {
			validationErrors = append(validationErrors,
				errors.UndeclaredQueryParamError(
					undeclared.Path,
					undeclared.Name,
					undeclared.Value,
					undeclared.DeclaredProperties,
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

func (v *paramValidator) validateSimpleParam(sch *base.Schema, rawParam string, parsedParam any, parameter *v3.Parameter) (validationErrors []*errors.ValidationError) {
	// check if the param is within an enum
	if sch.Enum != nil {
		matchFound := false
		for _, enumVal := range sch.Enum {
			if strings.TrimSpace(rawParam) == fmt.Sprint(enumVal.Value) {
				matchFound = true
				break
			}
		}
		if !matchFound {
			return []*errors.ValidationError{errors.IncorrectQueryParamEnum(parameter, rawParam, sch)}
		}
	}

	return ValidateSingleParameterSchema(
		sch,
		parsedParam,
		"Query parameter",
		"The query parameter",
		parameter.Name,
		helpers.ParameterValidation,
		helpers.ParameterValidationQuery,
		v.options,
	)
}
