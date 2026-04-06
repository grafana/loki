// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package errors

const (
	HowToFixReservedValues string = "parameter values need to URL Encoded to ensure reserved " +
		"values are correctly encoded, for example: '%s'"
	HowToFixParamInvalidInteger                     string = "Convert the value '%s' into an integer"
	HowToFixParamInvalidNumber                      string = "Convert the value '%s' into a number"
	HowToFixParamInvalidString                      string = "Convert the value '%s' into a string (cannot start with a number, or be a floating point)"
	HowToFixParamInvalidBoolean                     string = "Convert the value '%s' into a true/false value"
	HowToFixParamInvalidEnum                        string = "Instead of '%s', use one of the allowed values: '%s'"
	HowToFixParamInvalidFormEncode                  string = "Use a form style encoding for parameter values, for example: '%s'"
	HowToFixInvalidXml                              string = "Ensure xml is well-formed and matches schema structure"
	HowToFixXmlPrefix                               string = "Make sure to prepend the correct prefix '%s' to the declared fields"
	HowToFixXmlNamespace                            string = "Make sure to declare the 'xmlns:%s' with the correct namespace URI"
	HowToFixFormDataReservedCharacters              string = "Make sure to correcly encode specials characters to percent encoding, or set allowReserved to true"
	HowToFixInvalidSchema                           string = "Ensure that the object being submitted, matches the schema correctly"
	HowToFixInvalidTypeEncoding                     string = "Ensure that the object being submitted matches the property encoding Content-Type"
	HowToFixParamInvalidSpaceDelimitedObjectExplode string = "When using 'explode' with space delimited parameters, " +
		"they should be separated by spaces. For example: '%s'"
	HowToFixParamInvalidPipeDelimitedObjectExplode string = "When using 'explode' with pipe delimited parameters, " +
		"they should be separated by pipes '|'. For example: '%s'"
	HowToFixParamInvalidDeepObjectMultipleValues string = "There can only be a single value per property name, " +
		"deepObject parameters should contain the property key in square brackets next to the parameter name. For example: '%s'"
	HowToFixInvalidJSON           string = "The JSON submitted is invalid, please check the syntax"
	HowToFixInvalidUrlEncoded     string = "Ensure URL Encoded submitted is well-formed and matches schema structure"
	HowToFixDecodingError         string = "The object can't be decoded, so make sure it's being encoded correctly according to the spec."
	HowToFixInvalidContentType    string = "The content type is invalid, Use one of the %d supported types for this operation: %s"
	HowToFixInvalidResponseCode   string = "The service is responding with a code that is not defined in the spec, fix the service or add the code to the specification"
	HowToFixInvalidEncoding       string = "Ensure the correct encoding has been used on the object"
	HowToFixMissingValue          string = "Ensure the value has been set"
	HowToFixPath                  string = "Check the path is correct, and check that the correct HTTP method has been used (e.g. GET, POST, PUT, DELETE)"
	HowToFixPathMethod            string = "Add the missing operation to the contract for the path"
	HowToFixInvalidMaxItems       string = "Reduce the number of items in the array to %d or less"
	HowToFixInvalidMinItems       string = "Increase the number of items in the array to %d or more"
	HowToFixMissingHeader         string = "Make sure the service responding sets the required headers with this response code"
	HowToFixInvalidRenderedSchema string = "Check the request schema for circular references or invalid structures"
	HowToFixInvalidJsonSchema     string = "Check the request schema for invalid JSON Schema syntax, complex regex patterns, or unsupported schema constructs"
)
