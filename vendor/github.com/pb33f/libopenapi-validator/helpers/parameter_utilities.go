// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package helpers

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// QueryParam is a struct that holds the key, values and property name for a query parameter
// it's used for complex query types that need to be parsed and tracked differently depending
// on the encoding styles used.
type QueryParam struct {
	Key      string
	Values   []string
	Property string
}

// ExtractParamsForOperation will extract the parameters for the operation based on the request method.
// Both the path level params and the method level params will be returned.
func ExtractParamsForOperation(request *http.Request, item *v3.PathItem) []*v3.Parameter {
	params := item.Parameters
	switch request.Method {
	case http.MethodGet:
		if item.Get != nil {
			params = append(params, item.Get.Parameters...)
		}
	case http.MethodPost:
		if item.Post != nil {
			params = append(params, item.Post.Parameters...)
		}
	case http.MethodPut:
		if item.Put != nil {
			params = append(params, item.Put.Parameters...)
		}
	case http.MethodDelete:
		if item.Delete != nil {
			params = append(params, item.Delete.Parameters...)
		}
	case http.MethodOptions:
		if item.Options != nil {
			params = append(params, item.Options.Parameters...)
		}
	case http.MethodHead:
		if item.Head != nil {
			params = append(params, item.Head.Parameters...)
		}
	case http.MethodPatch:
		if item.Patch != nil {
			params = append(params, item.Patch.Parameters...)
		}
	case http.MethodTrace:
		if item.Trace != nil {
			params = append(params, item.Trace.Parameters...)
		}
	}
	return params
}

// ExtractSecurityForOperation will extract the security requirements for the operation based on the request method.
func ExtractSecurityForOperation(request *http.Request, item *v3.PathItem) []*base.SecurityRequirement {
	var schemes []*base.SecurityRequirement
	switch request.Method {
	case http.MethodGet:
		if item.Get != nil {
			schemes = append(schemes, item.Get.Security...)
		}
	case http.MethodPost:
		if item.Post != nil {
			schemes = append(schemes, item.Post.Security...)
		}
	case http.MethodPut:
		if item.Put != nil {
			schemes = append(schemes, item.Put.Security...)
		}
	case http.MethodDelete:
		if item.Delete != nil {
			schemes = append(schemes, item.Delete.Security...)
		}
	case http.MethodOptions:
		if item.Options != nil {
			schemes = append(schemes, item.Options.Security...)
		}
	case http.MethodHead:
		if item.Head != nil {
			schemes = append(schemes, item.Head.Security...)
		}
	case http.MethodPatch:
		if item.Patch != nil {
			schemes = append(schemes, item.Patch.Security...)
		}
	case http.MethodTrace:
		if item.Trace != nil {
			schemes = append(schemes, item.Trace.Security...)
		}
	}
	return schemes
}

// ExtractSecurityHeaderNames extracts header names from applicable security schemes.
// Returns header names from apiKey schemes with in:"header", plus "Authorization"
// for http/oauth2/openIdConnect schemes.
//
// This function is used by strict mode validation to recognize security headers
// as "declared" headers that should not trigger undeclared header errors.
func ExtractSecurityHeaderNames(
	security []*base.SecurityRequirement,
	securitySchemes map[string]*v3.SecurityScheme,
) []string {
	if security == nil || securitySchemes == nil {
		return nil
	}

	seen := make(map[string]bool)
	var headers []string

	for _, sec := range security {
		if sec == nil || sec.ContainsEmptyRequirement {
			continue // No security required for this option
		}

		if sec.Requirements == nil {
			continue
		}

		for pair := sec.Requirements.First(); pair != nil; pair = pair.Next() {
			schemeName := pair.Key()
			scheme, ok := securitySchemes[schemeName]
			if !ok || scheme == nil {
				continue
			}

			var headerName string
			switch strings.ToLower(scheme.Type) {
			case "apikey":
				if strings.ToLower(scheme.In) == Header {
					headerName = scheme.Name
				}
			case "http", "oauth2", "openidconnect":
				headerName = "Authorization"
			}

			if headerName != "" && !seen[strings.ToLower(headerName)] {
				seen[strings.ToLower(headerName)] = true
				headers = append(headers, headerName)
			}
		}
	}

	return headers
}

func cast(v string) any {
	if v == "true" || v == "false" {
		b, _ := strconv.ParseBool(v)
		return b
	}
	if i, err := strconv.ParseFloat(v, 64); err == nil {
		// check if this is an int or not
		if !strings.Contains(v, Period) {
			iv, _ := strconv.ParseInt(v, 10, 64)
			return iv
		}
		return i
	}
	return v
}

// ConstructParamMapFromDeepObjectEncoding will construct a map from the query parameters that are encoded as
// deep objects. It's kind of a crazy way to do things, but hey, each to their own.
func ConstructParamMapFromDeepObjectEncoding(values []*QueryParam, sch *base.Schema) map[string]interface{} {
	// deepObject encoding is a technique used to encode objects into query parameters. Kinda nuts.
	decoded := make(map[string]interface{})
	for _, v := range values {
		if decoded[v.Key] == nil {

			props := make(map[string]interface{})
			rawValues := make([]interface{}, len(v.Values))
			for i := range v.Values {
				rawValues[i] = cast(v.Values[i])
			}
			// check if the schema for the param is an array
			if sch != nil && slices.Contains(sch.Type, Array) {
				props[v.Property] = rawValues
			}
			// check if schema has additional properties defined as an array
			if sch != nil && sch.AdditionalProperties != nil &&
				sch.AdditionalProperties.IsA() {
				s := sch.AdditionalProperties.A.Schema()
				if s != nil &&
					slices.Contains(s.Type, Array) {
					props[v.Property] = rawValues
				}
			}

			if len(props) == 0 {
				props[v.Property] = cast(v.Values[0])
			}
			decoded[v.Key] = props
		} else {

			added := false
			rawValues := make([]interface{}, len(v.Values))
			for i := range v.Values {
				rawValues[i] = cast(v.Values[i])
			}
			// check if the schema for the param is an array
			if sch != nil && slices.Contains(sch.Type, Array) {
				decoded[v.Key].(map[string]interface{})[v.Property] = rawValues
				added = true
			}
			// check if schema has additional properties defined as an array
			if sch != nil && sch.AdditionalProperties != nil &&
				sch.AdditionalProperties.IsA() &&
				slices.Contains(sch.AdditionalProperties.A.Schema().Type, Array) {
				decoded[v.Key].(map[string]interface{})[v.Property] = rawValues
				added = true
			}
			if !added {
				decoded[v.Key].(map[string]interface{})[v.Property] = cast(v.Values[0])
			}

		}
	}
	return decoded
}

// ConstructParamMapFromQueryParamInput will construct a param map from an existing map of *QueryParam slices.
func ConstructParamMapFromQueryParamInput(values map[string][]*QueryParam) map[string]interface{} {
	decoded := make(map[string]interface{})
	for _, q := range values {
		for _, v := range q {
			decoded[v.Key] = cast(v.Values[0])
		}
	}
	return decoded
}

// ConstructParamMapFromPipeEncoding will construct a map from the query parameters that are encoded as
// pipe separated values. Perhaps the most sane way to delimit/encode properties.
func ConstructParamMapFromPipeEncoding(values []*QueryParam) map[string]interface{} {
	// Pipes are always a good alternative to commas, personally I think they're better, if I were encoding, I would
	// use pipes instead of commas, so much can go wrong with a comma, but a pipe? hardly ever.
	decoded := make(map[string]interface{})
	for _, v := range values {
		props := make(map[string]interface{})
		// explode PSV into array
		exploded := strings.Split(v.Values[0], Pipe)
		for i := range exploded {
			if i%2 == 0 {
				props[exploded[i]] = cast(exploded[i+1])
			}
		}
		decoded[v.Key] = props
	}
	return decoded
}

// ConstructParamMapFromSpaceEncoding will construct a map from the query parameters that are encoded as
// space delimited values. This is perhaps the worst way to delimit anything other than a paragraph of text.
func ConstructParamMapFromSpaceEncoding(values []*QueryParam) map[string]interface{} {
	// Don't use spaces to delimit anything unless you really know what the hell you're doing. Perhaps the
	// easiest way to blow something up, unless you're tokenizing strings... don't do this.
	decoded := make(map[string]interface{})
	for _, v := range values {
		props := make(map[string]interface{})
		// explode SSV into array
		exploded := strings.Split(v.Values[0], Space)
		for i := range exploded {
			if i%2 == 0 {
				props[exploded[i]] = cast(exploded[i+1])
			}
		}
		decoded[v.Key] = props
	}
	return decoded
}

// ConstructMapFromCSV will construct a map from a comma separated value string.
func ConstructMapFromCSV(csv string) map[string]interface{} {
	decoded := make(map[string]interface{})
	// explode SSV into array
	exploded := strings.Split(csv, Comma)
	for i := range exploded {
		if i%2 == 0 {
			if len(exploded) == i+1 {
				break
			}
			decoded[exploded[i]] = cast(exploded[i+1])
		}
	}
	return decoded
}

// ConstructKVFromCSV will construct a map from a comma separated value string that denotes key value pairs.
func ConstructKVFromCSV(values string) map[string]interface{} {
	props := make(map[string]interface{})
	exploded := strings.Split(values, Comma)
	for i := range exploded {
		obK := strings.Split(exploded[i], Equals)
		if len(obK) == 2 {
			props[obK[0]] = cast(obK[1])
		}
	}
	return props
}

// ConstructKVFromLabelEncoding will construct a map from a comma separated value string that denotes key value pairs.
func ConstructKVFromLabelEncoding(values string) map[string]interface{} {
	props := make(map[string]interface{})
	exploded := strings.Split(values, Period)
	for i := range exploded {
		obK := strings.Split(exploded[i], Equals)
		if len(obK) == 2 {
			props[obK[0]] = cast(obK[1])
		}
	}
	return props
}

// ConstructKVFromMatrixCSV will construct a map from a comma separated value string that denotes key value pairs.
func ConstructKVFromMatrixCSV(values string) map[string]interface{} {
	props := make(map[string]interface{})
	exploded := strings.Split(values, SemiColon)
	for i := range exploded {
		obK := strings.Split(exploded[i], Equals)
		if len(obK) == 2 {
			props[obK[0]] = cast(obK[1])
		}
	}
	return props
}

// ConstructParamMapFromFormEncodingArray will construct a map from the query parameters that are encoded as
// form encoded values.
func ConstructParamMapFromFormEncodingArray(values []*QueryParam) map[string]interface{} {
	decoded := make(map[string]interface{})
	for _, v := range values {
		props := make(map[string]interface{})
		// explode SSV into array
		exploded := strings.Split(v.Values[0], Comma)
		for i := range exploded {
			if i%2 == 0 {
				if len(exploded) > i+1 {
					props[exploded[i]] = cast(exploded[i+1])
				}
			}
		}
		decoded[v.Key] = props
	}
	return decoded
}

// DoesFormParamContainDelimiter will determine if a form parameter contains a delimiter.
func DoesFormParamContainDelimiter(value, style string) bool {
	if strings.Contains(value, Comma) && (style == "" || style == Form) {
		return true
	}
	return false
}

// ExplodeQueryValue will explode a query value based on the style (space, pipe, or form/default).
func ExplodeQueryValue(value, style string) []string {
	switch style {
	case SpaceDelimited:
		return strings.Split(value, Space)
	case PipeDelimited:
		return strings.Split(value, Pipe)
	default:
		return strings.Split(value, Comma)
	}
}

func CollapseCSVIntoFormStyle(key string, value string) string {
	return fmt.Sprintf("&%s=%s", key,
		strings.Join(strings.Split(value, ","), fmt.Sprintf("&%s=", key)))
}

func CollapseCSVIntoSpaceDelimitedStyle(key string, values []string) string {
	return fmt.Sprintf("%s=%s", key, strings.Join(values, "%20"))
}

func CollapseCSVIntoPipeDelimitedStyle(key string, values []string) string {
	return fmt.Sprintf("%s=%s", key, strings.Join(values, Pipe))
}
