package schema_validation

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

var rxReserved = regexp.MustCompile(`[:/?#\[\]@!$&'()*+,;=]`)

func TransformURLEncodedToSchemaJSON(bodyString string, schema *base.Schema, encoding *orderedmap.Map[string, *v3.Encoding]) (map[string]any, []*errors.ValidationError) {
	rawValues, err := url.ParseQuery(bodyString)
	if err != nil {
		return nil, []*errors.ValidationError{errors.InvalidURLEncodedParsing("empty form-urlencoded context", bodyString)}
	}

	jsonMap := unflattenValues(rawValues)

	var validationErrors []*errors.ValidationError

	if schema != nil {
		if schema.Properties != nil {
			for pair := orderedmap.First(schema.Properties); pair != nil; pair = pair.Next() {
				propName := pair.Key()
				propSchema := pair.Value().Schema()

				var contentEncoding *v3.Encoding
				var allowReserved bool

				if encoding != nil {
					contentEncoding = encoding.GetOrZero(propName)

					if contentEncoding != nil {
						allowReserved = contentEncoding.AllowReserved
					}
				}

				if val, exists := jsonMap[propName]; exists {
					newVal, err := applyEncodingRules(val, contentEncoding, propSchema)
					if err != nil {
						contentType := ""
						if contentEncoding != nil {
							contentType = contentEncoding.ContentType
						}

						validationErrors = append(validationErrors, errors.InvalidTypeEncoding(propSchema, propName, contentType))
					} else {
						jsonMap[propName] = newVal
						val = newVal
					}

					validateEncodingRecursive(propName, val, allowReserved, &validationErrors, propSchema)
				}
			}
		}

		coerced := coerceValue(jsonMap, schema)
		if asMap, ok := coerced.(map[string]any); ok {
			jsonMap = asMap
		}
	}

	return jsonMap, validationErrors
}

func applyEncodingRules(data any, encoding *v3.Encoding, schema *base.Schema) (any, error) {
	style := "form"
	explode := true
	contentType := ""

	if encoding != nil {
		contentType = encoding.ContentType

		if encoding.Style != "" {
			style = encoding.Style
			contentType = ""
		}

		if encoding.AllowReserved {
			contentType = ""
		}

		if encoding.Explode != nil {
			explode = *encoding.Explode
			contentType = ""
		} else if style != "form" {
			explode = false
		}
	}

	if contentType != "" && !IsURLEncodedContentType(contentType) && !strings.Contains(contentType, "text/plain") {
		if strVal, ok := data.(string); ok {
			if strings.Contains(contentType, helpers.JSONContentType) {
				var parsed any
				if err := json.Unmarshal([]byte(strVal), &parsed); err == nil {
					return parsed, nil
				}
				return nil, fmt.Errorf("value matches content-type '%s' but could not be parsed", contentType)
			}
		}
	}

	if isArraySchema(schema) {
		if strVal, ok := data.(string); ok {
			if !explode {
				switch style {
				case helpers.Form:
					return strings.Split(strVal, ","), nil
				case helpers.SpaceDelimited:
					return strings.Split(strVal, " "), nil
				case helpers.PipeDelimited:
					return strings.Split(strVal, "|"), nil
				}
			}
		}
	}

	if style == helpers.DeepObject {
		if _, ok := data.(map[string]any); !ok {
			return data, nil
		}
	}

	return data, nil
}

func unflattenValues(values url.Values) map[string]any {
	result := make(map[string]any)

	for k, v := range values {
		if strings.Contains(k, "[") {
			buildDeepMap(result, k, v)
		} else {
			if len(v) == 1 {
				result[k] = v[0]
			} else {
				result[k] = v
			}
		}
	}
	return result
}

func buildDeepMap(root map[string]any, key string, value []string) {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '[' || r == ']'
	})

	current := root
	for i, part := range parts {
		isLeaf := i == len(parts)-1

		if isLeaf {
			if len(value) == 1 {
				current[part] = value[0]
			} else {
				current[part] = value
			}
		} else {
			if _, ok := current[part]; !ok {
				current[part] = make(map[string]any)
			}
			if nextMap, ok := current[part].(map[string]any); ok {
				current = nextMap
			} else {
				return
			}
		}
	}
}

func validateEncodingRecursive(path string, val any, allowReserved bool, errs *[]*errors.ValidationError, schema *base.Schema) {
	if allowReserved {
		return
	}

	switch v := val.(type) {
	case string:
		if rxReserved.MatchString(v) {
			*errs = append(*errs, errors.ReservedURLEncodedValue(schema, path, v))
		}
	case []any:
		for i, item := range v {
			validateEncodingRecursive(fmt.Sprintf("%s[%d]", path, i), item, allowReserved, errs, schema)
		}
	case map[string]any:
		for k, item := range v {
			validateEncodingRecursive(fmt.Sprintf("%s[%s]", path, k), item, allowReserved, errs, schema)
		}
	case []string:
		for i, item := range v {
			if rxReserved.MatchString(item) {
				*errs = append(*errs, errors.ReservedURLEncodedValue(schema, fmt.Sprintf("%s[%d]", path, i), item))
			}
		}
	}
}

func coerceValue(data any, schema *base.Schema) any {
	if schema == nil {
		return data
	}

	targetTypes := []string{}
	if len(schema.Type) > 0 {
		targetTypes = append(targetTypes, schema.Type...)
	}

	extractTypes := func(proxies []*base.SchemaProxy) {
		for _, proxy := range proxies {
			sch := proxy.Schema()
			if len(sch.Type) > 0 {
				targetTypes = append(targetTypes, sch.Type...)
			}
		}
	}
	extractTypes(schema.AllOf)
	extractTypes(schema.OneOf)
	extractTypes(schema.AnyOf)

	if len(targetTypes) == 0 {
		return data
	}

	for _, t := range targetTypes {
		converted, ok := tryConvert(data, t, schema)
		if ok {
			return converted
		}
	}
	return data
}

func tryConvert(data any, targetType string, schema *base.Schema) (any, bool) {
	var strVal string
	var isString bool

	switch v := data.(type) {
	case string:
		strVal = v
		isString = true
	case []string:
		if len(v) > 0 {
			strVal = v[0]
			isString = true
		}
	}

	switch targetType {
	case helpers.Integer:
		if !isString || strVal == "" {
			return nil, false
		}
		i, err := strconv.ParseInt(strVal, 10, 64)
		if err == nil {
			return i, true
		}

	case helpers.Number:
		if !isString || strVal == "" {
			return nil, false
		}
		f, err := strconv.ParseFloat(strVal, 64)
		if err == nil {
			return f, true
		}

	case helpers.Boolean:
		if !isString {
			return nil, false
		}
		b, err := strconv.ParseBool(strVal)
		if err == nil {
			return b, true
		}

	case helpers.String:
		if isString {
			return strVal, true
		}
		return fmt.Sprintf("%v", data), true

	case helpers.Array:
		var arr []any
		itemSchema := getSchemaItem(schema)

		if vSlice, ok := data.([]any); ok {
			for _, s := range vSlice {
				arr = append(arr, coerceValue(s, itemSchema))
			}
			return arr, true
		}

		if vStringSlice, ok := data.([]string); ok {
			for _, s := range vStringSlice {
				arr = append(arr, coerceValue(s, itemSchema))
			}
			return arr, true
		}

		if vMap, ok := data.(map[string]any); ok {
			keys := make([]int, 0, len(vMap))
			mapIsArray := true
			for k := range vMap {
				idx, err := strconv.Atoi(k)
				if err != nil {
					mapIsArray = false
					break
				}
				keys = append(keys, idx)
			}
			if mapIsArray {
				sort.Ints(keys)
				for _, k := range keys {
					val := vMap[strconv.Itoa(k)]
					arr = append(arr, coerceValue(val, itemSchema))
				}
				return arr, true
			}
		}

		if isString {
			arr = append(arr, coerceValue(strVal, itemSchema))
			return arr, true
		}

	case helpers.Object:
		if m, ok := data.(map[string]any); ok {
			newMap := make(map[string]any)
			for k, v := range m {
				newMap[k] = v
			}
			if schema.Properties != nil {
				for pair := orderedmap.First(schema.Properties); pair != nil; pair = pair.Next() {
					propName := pair.Key()
					if val, exists := newMap[propName]; exists {
						newMap[propName] = coerceValue(val, pair.Value().Schema())
					}
				}
			}
			return newMap, true
		}
	}

	return nil, false
}

func isArraySchema(schema *base.Schema) bool {
	if schema == nil {
		return false
	}

	return slices.Contains(schema.Type, helpers.Array)
}

func getSchemaItem(schema *base.Schema) *base.Schema {
	if schema.Items != nil && schema.Items.IsA() {
		return schema.Items.A.Schema()
	}
	return nil
}

func (v *urlEncodedValidator) validateURLEncodedWithVersion(schema *base.Schema, encoding *orderedmap.Map[string, *v3.Encoding], bodyString string, log *slog.Logger, version float32) (bool, []*errors.ValidationError) {
	if schema == nil {
		log.Info("schema is empty and cannot be validated")
		return false, nil
	}

	transformedJSON, prevalidationErrors := TransformURLEncodedToSchemaJSON(bodyString, schema, encoding)
	if len(prevalidationErrors) > 0 {
		return false, prevalidationErrors
	}

	return v.schemaValidator.validateSchemaWithVersion(schema, nil, transformedJSON, log, version)
}

func IsURLEncodedContentType(mediaType string) bool {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	return strings.HasPrefix(mt, helpers.URLEncodedContentType)
}
