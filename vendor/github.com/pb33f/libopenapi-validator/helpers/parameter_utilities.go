// Copyright 2023-2026 Princess Beef Heavy Industries, LLC / Dave Shanley
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
//
// Deprecated: use EffectiveSecurityForOperation instead, which also handles
// document-level global security inheritance per the OpenAPI specification.
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

// EffectiveSecurityForOperation returns the security requirements that apply to the
// operation matched by request method. It implements OpenAPI's inheritance rule:
//   - If the operation defines security (even an empty array), use that.
//   - Otherwise, fall back to the document-level global security.
//   - Returns nil only when neither level defines security.
func EffectiveSecurityForOperation(request *http.Request, item *v3.PathItem, docSecurity []*base.SecurityRequirement) []*base.SecurityRequirement {
	op := ExtractOperation(request, item)
	if op != nil && op.Security != nil {
		return op.Security // operation-level (may be empty [] = "no security")
	}
	return docSecurity // nil when no global security either
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

// getPropertySchema looks up a property's schema from an object schema's Properties map.
// Returns nil if objectSchema is nil, has no Properties, or the property is not found.
func getPropertySchema(objectSchema *base.Schema, propertyName string) *base.Schema {
	if objectSchema == nil || objectSchema.Properties == nil {
		return nil
	}
	proxy := objectSchema.Properties.GetOrZero(propertyName)
	if proxy == nil {
		return nil
	}
	return proxy.Schema()
}

// castWithSchema casts a string value consulting the schema for the property's declared type.
// If the schema says the property is a string, the value is returned as-is (no numeric/bool guessing).
// For other declared types, it falls back to cast() which produces correct results for integer,
// number, and boolean values. The explicit string check prevents the most common miscast: numeric-
// looking strings like "10" being converted to int64 when the schema declares type: string.
func castWithSchema(v string, objectSchema *base.Schema, propertyName string) any {
	propSchema := getPropertySchema(objectSchema, propertyName)
	if propSchema != nil {
		for _, t := range propSchema.Type {
			if t == String {
				return v
			}
		}
	}
	return cast(v)
}

// constructKVFromDelimited is the shared implementation for constructing key=value maps
// from delimited strings (comma, period, semicolon). The delimiter determines how to split
// entries, and each entry is further split on '=' to extract key-value pairs.
func constructKVFromDelimited(values string, delimiter string, sch *base.Schema) map[string]interface{} {
	props := make(map[string]interface{})
	exploded := strings.Split(values, delimiter)
	for i := range exploded {
		obK := strings.Split(exploded[i], Equals)
		if len(obK) == 2 {
			props[obK[0]] = castWithSchema(obK[1], sch, obK[0])
		}
	}
	return props
}

// constructParamMapFromDelimitedEncoding is the shared implementation for constructing
// parameter maps from pipe-delimited or space-delimited query parameter values.
// Entries alternate between keys and values (key|value|key|value or key value key value).
func constructParamMapFromDelimitedEncoding(values []*QueryParam, delimiter string, sch *base.Schema) map[string]interface{} {
	decoded := make(map[string]interface{})
	for _, v := range values {
		props := make(map[string]interface{})
		exploded := strings.Split(v.Values[0], delimiter)
		for i := range exploded {
			if i%2 == 0 && i+1 < len(exploded) {
				props[exploded[i]] = castWithSchema(exploded[i+1], sch, exploded[i])
			}
		}
		decoded[v.Key] = props
	}
	return decoded
}

// ConstructParamMapFromDeepObjectEncoding will construct a map from the query parameters that are encoded as
// deep objects. It's kind of a crazy way to do things, but hey, each to their own.
func ConstructParamMapFromDeepObjectEncoding(values []*QueryParam, sch *base.Schema) map[string]interface{} {
	decoded := make(map[string]interface{})
	for _, v := range values {
		castForProp := func(val string) any {
			return castWithSchema(val, sch, v.Property)
		}

		if decoded[v.Key] == nil {
			props := make(map[string]interface{})
			rawValues := make([]interface{}, len(v.Values))
			for i := range v.Values {
				rawValues[i] = castForProp(v.Values[i])
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
				props[v.Property] = castForProp(v.Values[0])
			}
			decoded[v.Key] = props
		} else {
			added := false
			rawValues := make([]interface{}, len(v.Values))
			for i := range v.Values {
				rawValues[i] = castForProp(v.Values[i])
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
				decoded[v.Key].(map[string]interface{})[v.Property] = castForProp(v.Values[0])
			}
		}
	}
	return decoded
}

// ConstructParamMapFromQueryParamInput will construct a param map from an existing map of *QueryParam slices.
//
// Deprecated: use ConstructParamMapFromQueryParamInputWithSchema instead.
func ConstructParamMapFromQueryParamInput(values map[string][]*QueryParam) map[string]interface{} {
	return ConstructParamMapFromQueryParamInputWithSchema(values, nil)
}

// ConstructParamMapFromPipeEncoding will construct a map from the query parameters that are encoded as
// pipe separated values.
//
// Deprecated: use ConstructParamMapFromPipeEncodingWithSchema instead.
func ConstructParamMapFromPipeEncoding(values []*QueryParam) map[string]interface{} {
	return ConstructParamMapFromPipeEncodingWithSchema(values, nil)
}

// ConstructParamMapFromSpaceEncoding will construct a map from the query parameters that are encoded as
// space delimited values.
//
// Deprecated: use ConstructParamMapFromSpaceEncodingWithSchema instead.
func ConstructParamMapFromSpaceEncoding(values []*QueryParam) map[string]interface{} {
	return ConstructParamMapFromSpaceEncodingWithSchema(values, nil)
}

// ConstructMapFromCSV will construct a map from a comma separated value string.
//
// Deprecated: use ConstructMapFromCSVWithSchema instead.
func ConstructMapFromCSV(csv string) map[string]interface{} {
	return ConstructMapFromCSVWithSchema(csv, nil)
}

// ConstructKVFromCSV will construct a map from a comma separated value string that denotes key value pairs.
//
// Deprecated: use ConstructKVFromCSVWithSchema instead.
func ConstructKVFromCSV(values string) map[string]interface{} {
	return ConstructKVFromCSVWithSchema(values, nil)
}

// ConstructKVFromLabelEncoding will construct a map from a period separated value string that denotes key value pairs.
//
// Deprecated: use ConstructKVFromLabelEncodingWithSchema instead.
func ConstructKVFromLabelEncoding(values string) map[string]interface{} {
	return ConstructKVFromLabelEncodingWithSchema(values, nil)
}

// ConstructKVFromMatrixCSV will construct a map from a semicolon separated value string that denotes key value pairs.
//
// Deprecated: use ConstructKVFromMatrixCSVWithSchema instead.
func ConstructKVFromMatrixCSV(values string) map[string]interface{} {
	return ConstructKVFromMatrixCSVWithSchema(values, nil)
}

// ConstructParamMapFromFormEncodingArray will construct a map from the query parameters that are encoded as
// form encoded values.
//
// Deprecated: use ConstructParamMapFromFormEncodingArrayWithSchema instead.
func ConstructParamMapFromFormEncodingArray(values []*QueryParam) map[string]interface{} {
	return ConstructParamMapFromFormEncodingArrayWithSchema(values, nil)
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

// ConstructParamMapFromQueryParamInputWithSchema constructs a param map from an existing map of
// *QueryParam slices, using the object schema to determine property types before casting.
func ConstructParamMapFromQueryParamInputWithSchema(values map[string][]*QueryParam, sch *base.Schema) map[string]interface{} {
	decoded := make(map[string]interface{})
	for _, q := range values {
		for _, v := range q {
			decoded[v.Key] = castWithSchema(v.Values[0], sch, v.Key)
		}
	}
	return decoded
}

// ConstructParamMapFromPipeEncodingWithSchema constructs a map from pipe-delimited query parameters,
// using the object schema to determine property types before casting.
func ConstructParamMapFromPipeEncodingWithSchema(values []*QueryParam, sch *base.Schema) map[string]interface{} {
	return constructParamMapFromDelimitedEncoding(values, Pipe, sch)
}

// ConstructParamMapFromSpaceEncodingWithSchema constructs a map from space-delimited query parameters,
// using the object schema to determine property types before casting.
func ConstructParamMapFromSpaceEncodingWithSchema(values []*QueryParam, sch *base.Schema) map[string]interface{} {
	return constructParamMapFromDelimitedEncoding(values, Space, sch)
}

// ConstructMapFromCSVWithSchema constructs a map from a comma separated value string,
// using the object schema to determine property types before casting.
func ConstructMapFromCSVWithSchema(csv string, sch *base.Schema) map[string]interface{} {
	decoded := make(map[string]interface{})
	exploded := strings.Split(csv, Comma)
	for i := range exploded {
		if i%2 == 0 {
			if len(exploded) == i+1 {
				break
			}
			decoded[exploded[i]] = castWithSchema(exploded[i+1], sch, exploded[i])
		}
	}
	return decoded
}

// ConstructKVFromCSVWithSchema constructs a map from a comma-separated key=value string,
// using the object schema to determine property types before casting.
func ConstructKVFromCSVWithSchema(values string, sch *base.Schema) map[string]interface{} {
	return constructKVFromDelimited(values, Comma, sch)
}

// ConstructKVFromLabelEncodingWithSchema constructs a map from a period-separated key=value string,
// using the object schema to determine property types before casting.
func ConstructKVFromLabelEncodingWithSchema(values string, sch *base.Schema) map[string]interface{} {
	return constructKVFromDelimited(values, Period, sch)
}

// ConstructKVFromMatrixCSVWithSchema constructs a map from a semicolon-separated key=value string,
// using the object schema to determine property types before casting.
func ConstructKVFromMatrixCSVWithSchema(values string, sch *base.Schema) map[string]interface{} {
	return constructKVFromDelimited(values, SemiColon, sch)
}

// ConstructParamMapFromFormEncodingArrayWithSchema constructs a map from form-encoded query parameters,
// using the object schema to determine property types before casting.
func ConstructParamMapFromFormEncodingArrayWithSchema(values []*QueryParam, sch *base.Schema) map[string]interface{} {
	decoded := make(map[string]interface{})
	for _, v := range values {
		props := make(map[string]interface{})
		exploded := strings.Split(v.Values[0], Comma)
		for i := range exploded {
			if i%2 == 0 {
				if len(exploded) > i+1 {
					props[exploded[i]] = castWithSchema(exploded[i+1], sch, exploded[i])
				}
			}
		}
		decoded[v.Key] = props
	}
	return decoded
}
