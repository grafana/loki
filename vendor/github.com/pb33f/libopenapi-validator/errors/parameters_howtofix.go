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
	HowToFixInvalidSchema                           string = "Ensure that the object being submitted, matches the schema correctly"
	HowToFixParamInvalidSpaceDelimitedObjectExplode string = "When using 'explode' with space delimited parameters, " +
		"they should be separated by spaces. For example: '%s'"
	HowToFixParamInvalidPipeDelimitedObjectExplode string = "When using 'explode' with pipe delimited parameters, " +
		"they should be separated by pipes '|'. For example: '%s'"
	HowToFixParamInvalidDeepObjectMultipleValues string = "There can only be a single value per property name, " +
		"deepObject parameters should contain the property key in square brackets next to the parameter name. For example: '%s'"
	HowToFixInvalidJSON         string = "The JSON submitted is invalid, please check the syntax"
	HowToFixDecodingError              = "The object can't be decoded, so make sure it's being encoded correctly according to the spec."
	HowToFixInvalidContentType         = "The content type is invalid, Use one of the %d supported types for this operation: %s"
	HowToFixInvalidResponseCode        = "The service is responding with a code that is not defined in the spec, fix the service or add the code to the specification"
	HowToFixInvalidEncoding            = "Ensure the correct encoding has been used on the object"
	HowToFixMissingValue               = "Ensure the value has been set"
	HowToFixPath                       = "Check the path is correct, and check that the correct HTTP method has been used (e.g. GET, POST, PUT, DELETE)"
	HowToFixPathMethod                 = "Add the missing operation to the contract for the path"
	HowToFixInvalidMaxItems            = "Reduce the number of items in the array to %d or less"
	HowToFixInvalidMinItems            = "Increase the number of items in the array to %d or more"
	HowToFixMissingHeader              = "Make sure the service responding sets the required headers with this response code"
)
