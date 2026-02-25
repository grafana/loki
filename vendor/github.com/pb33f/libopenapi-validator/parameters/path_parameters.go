// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package parameters

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi-validator/paths"
)

func (v *paramValidator) ValidatePathParams(request *http.Request) (bool, []*errors.ValidationError) {
	pathItem, errs, foundPath := paths.FindPath(request, v.document, v.options.RegexCache)
	if len(errs) > 0 {
		return false, errs
	}
	return v.ValidatePathParamsWithPathItem(request, pathItem, foundPath)
}

func (v *paramValidator) ValidatePathParamsWithPathItem(request *http.Request, pathItem *v3.PathItem, pathValue string) (bool, []*errors.ValidationError) {
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
	// split the path into segments
	submittedSegments := strings.Split(paths.StripRequestPath(request, v.document), helpers.Slash)
	pathSegments := strings.Split(pathValue, helpers.Slash)

	// extract params for the operation
	params := helpers.ExtractParamsForOperation(request, pathItem)
	var validationErrors []*errors.ValidationError
	for _, p := range params {
		if p.In == helpers.Path {
			// var paramTemplate string
			for x := range pathSegments {
				if pathSegments[x] == "" { // skip empty segments
					continue
				}

				var rgx *regexp.Regexp

				if v.options.RegexCache != nil {
					if cachedRegex, found := v.options.RegexCache.Load(pathSegments[x]); found {
						rgx = cachedRegex.(*regexp.Regexp)
					}
				}

				if rgx == nil {

					r, err := helpers.GetRegexForPath(pathSegments[x])
					if err != nil {
						continue
					}

					rgx = r

					if v.options.RegexCache != nil {
						v.options.RegexCache.Store(pathSegments[x], r)
					}
				}

				matches := rgx.FindStringSubmatch(submittedSegments[x])
				matches = matches[1:]

				// Check if it is well-formed.
				idxs, errBraces := helpers.BraceIndices(pathSegments[x])
				if errBraces != nil {
					continue
				}

				idx := 0

				for _, match := range matches {

					isMatrix := false
					isLabel := false
					// isExplode := false
					isSimple := true
					paramTemplate := pathSegments[x][idxs[idx]+1 : idxs[idx+1]-1]
					idx += 2 // move to the next brace pair
					paramName := paramTemplate

					// check for an asterisk on the end of the parameter (explode)
					if strings.HasSuffix(paramTemplate, helpers.Asterisk) {
						// isExplode = true
						paramName = paramTemplate[:len(paramTemplate)-1]
					}
					if strings.HasPrefix(paramTemplate, helpers.Period) {
						isLabel = true
						isSimple = false
						paramName = paramName[1:]
					}
					if strings.HasPrefix(paramTemplate, helpers.SemiColon) {
						isMatrix = true
						isSimple = false
						paramName = paramName[1:]
					}

					// does this param name match the current path segment param name
					if paramName != p.Name {
						continue
					}

					paramValue := match

					// URL decode the parameter value before validation
					decodedParamValue, _ := url.PathUnescape(paramValue)

					if decodedParamValue == "" {
						// Mandatory path parameter cannot be empty
						if p.Required != nil && *p.Required {
							validationErrors = append(validationErrors, errors.PathParameterMissing(p))
							break
						}
						continue
					}

					// extract the schema from the parameter
					sch := p.Schema.Schema()

					// check enum (if present)
					enumCheck := func(decodedValue string) {
						matchFound := false
						for _, enumVal := range sch.Enum {
							if strings.TrimSpace(decodedValue) == fmt.Sprint(enumVal.Value) {
								matchFound = true
								break
							}
						}
						if !matchFound {
							validationErrors = append(validationErrors,
								errors.IncorrectPathParamEnum(p, strings.ToLower(decodedValue), sch))
						}
					}

					// for each type, check the value.
					if sch != nil && sch.Type != nil {
						for typ := range sch.Type {
							switch sch.Type[typ] {
							case helpers.String:

								// TODO: label and matrix style validation

								// check if the param is within the enum
								if sch.Enum != nil {
									enumCheck(decodedParamValue)
									break
								}
								validationErrors = append(validationErrors,
									ValidateSingleParameterSchema(
										sch,
										decodedParamValue,
										"Path parameter",
										"The path parameter",
										p.Name,
										helpers.ParameterValidation,
										helpers.ParameterValidationPath,
										v.options,
									)...)

							case helpers.Integer:
								// simple use case is already handled in find param.
								rawParamValue, paramValueParsed, err := v.resolveInteger(sch, p, isLabel, isMatrix, decodedParamValue)
								if err != nil {
									validationErrors = append(validationErrors, err...)
									break
								}
								// check if the param is within the enum
								if sch.Enum != nil {
									enumCheck(rawParamValue)
									break
								}
								validationErrors = append(validationErrors, ValidateSingleParameterSchema(
									sch,
									paramValueParsed,
									"Path parameter",
									"The path parameter",
									p.Name,
									helpers.ParameterValidation,
									helpers.ParameterValidationPath,
									v.options,
								)...)

							case helpers.Number:
								// simple use case is already handled in find param.
								rawParamValue, paramValueParsed, err := v.resolveNumber(sch, p, isLabel, isMatrix, decodedParamValue)
								if err != nil {
									validationErrors = append(validationErrors, err...)
									break
								}
								// check if the param is within the enum
								if sch.Enum != nil {
									enumCheck(rawParamValue)
									break
								}
								validationErrors = append(validationErrors, ValidateSingleParameterSchema(
									sch,
									paramValueParsed,
									"Path parameter",
									"The path parameter",
									p.Name,
									helpers.ParameterValidation,
									helpers.ParameterValidationPath,
									v.options,
								)...)

							case helpers.Boolean:
								if isLabel && p.Style == helpers.LabelStyle {
									if _, err := strconv.ParseBool(decodedParamValue[1:]); err != nil {
										validationErrors = append(validationErrors,
											errors.IncorrectPathParamBool(p, decodedParamValue[1:], sch))
									}
								}
								if isSimple {
									if _, err := strconv.ParseBool(decodedParamValue); err != nil {
										validationErrors = append(validationErrors,
											errors.IncorrectPathParamBool(p, decodedParamValue, sch))
									}
								}
								if isMatrix && p.Style == helpers.MatrixStyle {
									// strip off the colon and the parameter name
									decodedForMatrix := strings.Replace(decodedParamValue[1:], fmt.Sprintf("%s=", p.Name), "", 1)
									if _, err := strconv.ParseBool(decodedForMatrix); err != nil {
										validationErrors = append(validationErrors,
											errors.IncorrectPathParamBool(p, decodedForMatrix, sch))
									}
								}
							case helpers.Object:
								var encodedObject interface{}

								if p.IsDefaultPathEncoding() {
									encodedObject = helpers.ConstructMapFromCSV(decodedParamValue)
								} else {
									switch p.Style {
									case helpers.LabelStyle:
										if !p.IsExploded() {
											encodedObject = helpers.ConstructMapFromCSV(decodedParamValue[1:])
										} else {
											encodedObject = helpers.ConstructKVFromLabelEncoding(decodedParamValue)
										}
									case helpers.MatrixStyle:
										if !p.IsExploded() {
											decodedForMatrix := strings.Replace(decodedParamValue[1:], fmt.Sprintf("%s=", p.Name), "", 1)
											encodedObject = helpers.ConstructMapFromCSV(decodedForMatrix)
										} else {
											decodedForMatrix := strings.Replace(decodedParamValue[1:], fmt.Sprintf("%s=", p.Name), "", 1)
											encodedObject = helpers.ConstructKVFromMatrixCSV(decodedForMatrix)
										}
									default:
										if p.IsExploded() {
											encodedObject = helpers.ConstructKVFromCSV(decodedParamValue)
										}
									}
								}
								// if a schema was extracted
								if sch != nil {
									validationErrors = append(validationErrors,
										ValidateParameterSchema(sch,
											encodedObject,
											"",
											"Path parameter",
											"The path parameter",
											p.Name,
											helpers.ParameterValidation,
											helpers.ParameterValidationPath, v.options)...)
								}

							case helpers.Array:

								// extract the items schema in order to validate the array items.
								if sch.Items != nil && sch.Items.IsA() {
									iSch := sch.Items.A.Schema()
									for n := range iSch.Type {
										// determine how to explode the array
										var arrayValues []string
										if isSimple {
											arrayValues = strings.Split(decodedParamValue, helpers.Comma)
										}
										if isLabel {
											if !p.IsExploded() {
												arrayValues = strings.Split(decodedParamValue[1:], helpers.Comma)
											} else {
												arrayValues = strings.Split(decodedParamValue[1:], helpers.Period)
											}
										}
										if isMatrix {
											if !p.IsExploded() {
												decodedForMatrix := strings.Replace(decodedParamValue[1:], fmt.Sprintf("%s=", p.Name), "", 1)
												arrayValues = strings.Split(decodedForMatrix, helpers.Comma)
											} else {
												decodedForMatrix := strings.ReplaceAll(decodedParamValue[1:], fmt.Sprintf("%s=", p.Name), "")
												arrayValues = strings.Split(decodedForMatrix, helpers.SemiColon)
											}
										}
										switch iSch.Type[n] {
										case helpers.Integer:
											for pv := range arrayValues {
												if _, err := strconv.ParseInt(arrayValues[pv], 10, 64); err != nil {
													validationErrors = append(validationErrors,
														errors.IncorrectPathParamArrayInteger(p, arrayValues[pv], sch, iSch))
												}
											}
										case helpers.Number:
											for pv := range arrayValues {
												if _, err := strconv.ParseFloat(arrayValues[pv], 64); err != nil {
													validationErrors = append(validationErrors,
														errors.IncorrectPathParamArrayNumber(p, arrayValues[pv], sch, iSch))
												}
											}
										case helpers.Boolean:
											for pv := range arrayValues {
												bc := len(validationErrors)
												if _, err := strconv.ParseBool(arrayValues[pv]); err != nil {
													validationErrors = append(validationErrors,
														errors.IncorrectPathParamArrayBoolean(p, arrayValues[pv], sch, iSch))
													continue
												}
												if len(validationErrors) == bc {
													// ParseBool will parse 0 or 1 as false/true to we
													// need to catch this edge case.
													if arrayValues[pv] == "0" || arrayValues[pv] == "1" {
														validationErrors = append(validationErrors,
															errors.IncorrectPathParamArrayBoolean(p, arrayValues[pv], sch, iSch))
														continue
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	errors.PopulateValidationErrors(validationErrors, request, pathValue)

	if len(validationErrors) > 0 {
		return false, validationErrors
	}
	return true, nil
}

func (v *paramValidator) resolveNumber(sch *base.Schema, p *v3.Parameter, isLabel bool, isMatrix bool, paramValue string) (string, float64, []*errors.ValidationError) {
	if isLabel && p.Style == helpers.LabelStyle {
		paramValueParsed, err := strconv.ParseFloat(paramValue[1:], 64)
		if err != nil {
			return "", 0, []*errors.ValidationError{errors.IncorrectPathParamNumber(p, paramValue[1:], sch)}
		}
		return paramValue[1:], paramValueParsed, nil
	}
	if isMatrix && p.Style == helpers.MatrixStyle {
		// strip off the colon and the parameter name
		paramValue = strings.Replace(paramValue[1:], fmt.Sprintf("%s=", p.Name), "", 1)
		paramValueParsed, err := strconv.ParseFloat(paramValue, 64)
		if err != nil {
			return "", 0, []*errors.ValidationError{errors.IncorrectPathParamNumber(p, paramValue[1:], sch)}
		}
		return paramValue, paramValueParsed, nil
	}
	paramValueParsed, err := strconv.ParseFloat(paramValue, 64)
	if err != nil {
		return "", 0, []*errors.ValidationError{errors.IncorrectPathParamNumber(p, paramValue, sch)}
	}
	return paramValue, paramValueParsed, nil
}

func (v *paramValidator) resolveInteger(sch *base.Schema, p *v3.Parameter, isLabel bool, isMatrix bool, paramValue string) (string, int64, []*errors.ValidationError) {
	if isLabel && p.Style == helpers.LabelStyle {
		paramValueParsed, err := strconv.ParseInt(paramValue[1:], 10, 64)
		if err != nil {
			return "", 0, []*errors.ValidationError{errors.IncorrectPathParamInteger(p, paramValue[1:], sch)}
		}
		return paramValue[1:], paramValueParsed, nil
	}
	if isMatrix && p.Style == helpers.MatrixStyle {
		// strip off the colon and the parameter name
		paramValue = strings.Replace(paramValue[1:], fmt.Sprintf("%s=", p.Name), "", 1)
		paramValueParsed, err := strconv.ParseInt(paramValue, 10, 64)
		if err != nil {
			return "", 0, []*errors.ValidationError{errors.IncorrectPathParamInteger(p, paramValue[1:], sch)}
		}
		return paramValue, paramValueParsed, nil
	}
	paramValueParsed, err := strconv.ParseInt(paramValue, 10, 64)
	if err != nil {
		return "", 0, []*errors.ValidationError{errors.IncorrectPathParamInteger(p, paramValue, sch)}
	}
	return paramValue, paramValueParsed, nil
}
