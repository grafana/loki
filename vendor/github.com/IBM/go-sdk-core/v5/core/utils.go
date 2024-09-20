package core

// (C) Copyright IBM Corp. 2019.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	validator "github.com/go-playground/validator/v10"
)

// Validate is a shared validator instance used to perform validation of structs.
var Validate *validator.Validate

func init() {
	Validate = validator.New()
}

const (
	jsonMimePattern      = "(?i)^application\\/((json)|(merge\\-patch\\+json)|(vnd\\..*\\+json))(\\s*;.*)?$"
	jsonPatchMimePattern = "(?i)^application\\/json\\-patch\\+json(\\s*;.*)?$"
)

// IsNil checks if the specified object is nil or not.
func IsNil(object interface{}) bool {
	if object == nil {
		return true
	}

	switch reflect.TypeOf(object).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflect.ValueOf(object).IsNil()
	}

	return false
}

// ValidateNotNil returns the specified error if 'object' is nil, nil otherwise.
func ValidateNotNil(object interface{}, errorMsg string) error {
	if IsNil(object) {
		err := fmt.Errorf(errorMsg)
		return SDKErrorf(err, "", "obj-is-nil", getComponentInfo())
	}
	return nil
}

// ValidateStruct validates 'param' (assumed to be a ptr to a struct) according to the
// annotations attached to its fields.
func ValidateStruct(param interface{}, paramName string) error {
	err := ValidateNotNil(param, paramName+" cannot be nil")
	if err != nil {
		err = RepurposeSDKProblem(err, "struct-is-nil")
		return err
	}

	err = Validate.Struct(param)
	if err != nil {
		// If there were validation errors then return an error containing the field errors
		if fieldErrors, ok := err.(validator.ValidationErrors); ok {
			err = fmt.Errorf("%s failed validation:\n%s", paramName, fieldErrors.Error())
			return SDKErrorf(err, "", "struct-validation-errors", getComponentInfo())
		}
		return SDKErrorf(err, "", "struct-validate-unknown-error", getComponentInfo())
	}

	return nil
}

// StringPtr returns a pointer to string literal.
func StringPtr(literal string) *string {
	return &literal
}

// BoolPtr returns a pointer to boolean literal.
func BoolPtr(literal bool) *bool {
	return &literal
}

// Int64Ptr returns a pointer to int64 literal.
func Int64Ptr(literal int64) *int64 {
	return &literal
}

// Float32Ptr returns a pointer to float32 literal.
func Float32Ptr(literal float32) *float32 {
	return &literal
}

// Float64Ptr returns a pointer to float64 literal.
func Float64Ptr(literal float64) *float64 {
	return &literal
}

// UUIDPtr returns a pointer to strfmt.UUID literal.
func UUIDPtr(literal strfmt.UUID) *strfmt.UUID {
	return &literal
}

// ByteArrayPtr returns a pointer to []byte literal.
func ByteArrayPtr(literal []byte) *[]byte {
	return &literal
}

// IsJSONMimeType Returns true iff the specified mimeType value represents a
// "JSON" mimetype.
func IsJSONMimeType(mimeType string) bool {
	if mimeType == "" {
		return false
	}
	matched, err := regexp.MatchString(jsonMimePattern, mimeType)
	if err != nil {
		return false
	}
	return matched
}

// IsJSONPatchMimeType returns true iff the specified mimeType value represents
// a "JSON Patch" mimetype.
func IsJSONPatchMimeType(mimeType string) bool {
	if mimeType == "" {
		return false
	}
	matched, err := regexp.MatchString(jsonPatchMimePattern, mimeType)
	if err != nil {
		return false
	}
	return matched
}

// StringNilMapper de-references the parameter 's' and returns the result, or ""
// if 's' is nil.
func StringNilMapper(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// HasBadFirstOrLastChar checks if the string starts with `{` or `"`
// or ends with `}` or `"`.
func HasBadFirstOrLastChar(str string) bool {
	return strings.HasPrefix(str, "{") || strings.HasPrefix(str, "\"") ||
		strings.HasSuffix(str, "}") || strings.HasSuffix(str, "\"")
}

// UserHomeDir returns the user home directory.
func UserHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return os.Getenv("HOME")
}

// SystemInfo returns the system information.
func SystemInfo() string {
	return fmt.Sprintf("(arch=%s; os=%s; go.version=%s)", runtime.GOARCH, runtime.GOOS, runtime.Version())
}

// PrettyPrint print pretty.
func PrettyPrint(result interface{}, resultName string) {
	output, err := json.MarshalIndent(result, "", "    ")

	if err == nil {
		fmt.Printf("%v:\n%+v\n\n", resultName, string(output))
	}
}

// GetCurrentTime returns the current Unix time.
func GetCurrentTime() int64 {
	return time.Now().Unix()
}

// Pre-compiled regular expression used to remove the surrounding
// [] characters from a JSON string containing a slice (e.g. `["str1", "str2", "str3"]`).
var reJsonSlice = regexp.MustCompile(`(?s)\[(\S*)\]`)

// ConvertSlice marshals 'slice' to a json string, performs
// string manipulation on the resulting string, and converts
// the string to a '[]string'. If 'slice' is nil, not a 'slice' type,
// or an error occurred during conversion, an error will be returned
func ConvertSlice(slice interface{}) (s []string, err error) {
	inputIsSlice := false

	if IsNil(slice) {
		err = fmt.Errorf(ERRORMSG_NIL_SLICE)
		err = SDKErrorf(err, "", "nil-slice", getComponentInfo())
		return
	}

	// Reflect on 'slice' to validate the input is in fact a slice
	rResultType := reflect.TypeOf(slice)

	switch rResultType.Kind() {
	case reflect.Slice:
		inputIsSlice = true
	default:
	}

	// If it's not a slice, just return an error
	if !inputIsSlice {
		err = fmt.Errorf(ERRORMSG_PARAM_NOT_SLICE)
		err = SDKErrorf(err, "", "param-not-slice", getComponentInfo())
		return
	} else if reflect.ValueOf(slice).Len() == 0 {
		s = []string{}
		return
	}

	jsonBuffer, err := json.Marshal(slice)
	if err != nil {
		err = fmt.Errorf(ERRORMSG_MARSHAL_SLICE, err.Error())
		err = SDKErrorf(err, "", "slice-marshal-error", getComponentInfo())
		return
	}

	jsonString := string(jsonBuffer)

	// Use regex to convert the json string to a string slice
	match := reJsonSlice.FindStringSubmatch(jsonString)
	if match != nil && match[1] != "" {
		newString := match[1]
		s = strings.Split(newString, ",")
		// For each slice element, attempt to remove any surrounding quotes
		// added by marshaling into a json string
		for i := range s {
			unquotedString, unquoteErr := strconv.Unquote(s[i])
			if unquoteErr == nil && unquotedString != "" {
				s[i] = unquotedString
			}
		}
		return
	}

	// If we returned a plain string that's not in "slice format",
	// then attempt to just convert it to a string slice.
	if jsonString != "" {
		s = strings.Split(jsonString, ",")
		return
	}

	err = fmt.Errorf(ERRORMSG_CONVERT_SLICE)
	return nil, SDKErrorf(err, "", "cant-convert-slice", getComponentInfo())
}

// SliceContains returns true iff "contains" is an element of "slice"
func SliceContains(slice []string, contains string) bool {
	for _, elem := range slice {
		if elem == contains {
			return true
		}
	}
	return false
}

// GetQueryParam returns a pointer to the value of query parameter `param` from urlStr,
// or nil if not found.
func GetQueryParam(urlStr *string, param string) (value *string, err error) {
	if urlStr == nil || *urlStr == "" {
		return
	}

	urlObj, err := url.Parse(*urlStr)
	if err != nil {
		err = SDKErrorf(err, "", "url-parse-error", getComponentInfo())
		return
	}

	query, err := url.ParseQuery(urlObj.RawQuery)
	if err != nil {
		err = SDKErrorf(err, "", "url-parse-query-error", getComponentInfo())
		return
	}

	v := query.Get(param)
	if v == "" {
		return
	}

	value = &v

	return
}

// GetQueryParamAsInt returns a pointer to the value of query parameter `param` from urlStr
// converted to an int64 value, or nil if not found.
func GetQueryParamAsInt(urlStr *string, param string) (value *int64, err error) {
	strValue, err := GetQueryParam(urlStr, param)
	if err != nil {
		err = RepurposeSDKProblem(err, "get-query-error")
		return
	}

	if strValue == nil {
		return
	}

	intValue, err := strconv.ParseInt(*strValue, 10, 64)
	if err != nil {
		err = SDKErrorf(err, "", "parse-int-query-error", getComponentInfo())
		return nil, err
	}

	value = &intValue

	return
}

// keywords that are redacted
var redactedKeywords = []string{
	"apikey",
	"api_key",
	"passcode",
	"password",
	"token",

	"aadClientId",
	"aadClientSecret",
	"auth",
	"auth_provider_x509_cert_url",
	"auth_uri",
	"client_email",
	"client_id",
	"client_x509_cert_url",
	"key",
	"project_id",
	"secret",
	"subscriptionId",
	"tenantId",
	"thumbprint",
	"token_uri",

	// Information from issue: https://github.com/IBM/go-sdk-core/issues/190
	// // Redhat
	// "ibm-cos-access-key",
	// "ibm-cos-secret-key",
	// "iam-api-key",
	// "kms-root-key",
	// "kms-api-key",

	// // AWS
	// "aws-access-key",
	// "aws-secret-access-key",

	// // Azure
	// "tenantId",
	// "subscriptionId",
	// "aadClientId",
	// "aadClientSecret",

	// // Google
	// "project_id",
	// "private_key_id",
	// "private_key",
	// "client_email",
	// "client_id",
	// "auth_uri",
	// "token_uri",
	// "auth_provider_x509_cert_url",
	// "client_x509_cert_url",

	// // IBM
	// "primary-gui-api-user",
	// "primary-gui-api-password",
	// "owning-gui-api-user",
	// "owning-gui-api-password",
	// "g2_api_key",

	// // NetApp
	// "username",
	// "password",

	// // VMware
	// "vcenter-username",
	// "vcenter-password",
	// "thumbprint",
}

var redactedTokens = strings.Join(redactedKeywords, "|")

// Pre-compiled regular expressions used by RedactSecrets().
var reAuthHeader = regexp.MustCompile(`(?m)^(Authorization|X-Auth\S*): .*`)
var rePropertySetting = regexp.MustCompile(`(?i)(` + redactedTokens + `)=[^&]*(&|$)`)
var reJsonField = regexp.MustCompile(`(?i)"([^"]*(` + redactedTokens + `)[^"_]*)":\s*"[^\,]*"`)

// RedactSecrets() returns the input string with secrets redacted.
func RedactSecrets(input string) string {
	var redacted = "[redacted]"

	redactedString := input
	redactedString = reAuthHeader.ReplaceAllString(redactedString, "$1: "+redacted)
	redactedString = rePropertySetting.ReplaceAllString(redactedString, "$1="+redacted+"$2")
	redactedString = reJsonField.ReplaceAllString(redactedString, fmt.Sprintf(`"$1":"%s"`, redacted))

	return redactedString
}
