// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"

	"go.opentelemetry.io/collector/confmap/internal/third_party/composehook"
)

const (
	// MapstructureTag is the struct field tag used to record marshaling/unmarshaling settings.
	// See https://pkg.go.dev/github.com/go-viper/mapstructure/v2 for supported values.
	MapstructureTag = "mapstructure"
)

// WithIgnoreUnused sets an option to ignore errors if existing
// keys in the original Conf were unused in the decoding process
// (extra keys).
func WithIgnoreUnused() UnmarshalOption {
	return UnmarshalOptionFunc(func(uo *UnmarshalOptions) {
		uo.IgnoreUnused = true
	})
}

// WithForceUnmarshaler sets an option to run a top-level Unmarshal method,
// even if the Conf being unmarshaled is already a parameter from an Unmarshal method.
// To avoid infinite recursion, this should only be used when unmarshaling into
// a different type from the current Unmarshaler.
// For instance, this should be used in wrapper types such as configoptional.Optional
// to ensure the inner type's Unmarshal method is called.
func WithForceUnmarshaler() UnmarshalOption {
	return UnmarshalOptionFunc(func(uo *UnmarshalOptions) {
		uo.ForceUnmarshaler = true
	})
}

// Decode decodes the contents of the Conf into the result argument, using a
// mapstructure decoder with the following notable behaviors. Ensures that maps whose
// values are nil pointer structs resolved to the zero value of the target struct (see
// expandNilStructPointers). Converts string to []string by splitting on ','. Ensures
// uniqueness of component IDs (see mapKeyStringToMapKeyTextUnmarshalerHookFunc).
// Decodes time.Duration from strings. Allows custom unmarshaling for structs implementing
// encoding.TextUnmarshaler. Allows custom unmarshaling for structs implementing confmap.Unmarshaler.
func Decode(input, result any, settings UnmarshalOptions, skipTopLevelUnmarshaler bool) error {
	dc := &mapstructure.DecoderConfig{
		ErrorUnused:      !settings.IgnoreUnused,
		Result:           result,
		TagName:          MapstructureTag,
		WeaklyTypedInput: false,
		MatchName:        caseSensitiveMatchName,
		DecodeNil:        true,
		DecodeHook: composehook.ComposeDecodeHookFunc(
			useExpandValue(),
			expandNilStructPointersHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
			mapKeyStringToMapKeyTextUnmarshalerHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc(),
			unmarshalerHookFunc(result, skipTopLevelUnmarshaler && !settings.ForceUnmarshaler),
			// after the main unmarshaler hook is called,
			// we unmarshal the embedded structs if present to merge with the result:
			unmarshalerEmbeddedStructsHookFunc(settings),
			zeroSliceAndMapHookFunc(),
		),
	}
	decoder, err := mapstructure.NewDecoder(dc)
	if err != nil {
		return err
	}
	if err = decoder.Decode(input); err != nil {
		if strings.HasPrefix(err.Error(), "error decoding ''") {
			return errors.Unwrap(err)
		}
		return err
	}
	return nil
}

// When a value has been loaded from an external source via a provider, we keep both the
// parsed value and the original string value. This allows us to expand the value to its
// original string representation when decoding into a string field, and use the original otherwise.
func useExpandValue() mapstructure.DecodeHookFuncType {
	return func(
		_ reflect.Type,
		to reflect.Type,
		data any,
	) (any, error) {
		if exp, ok := data.(ExpandedValue); ok {
			v := castTo(exp, to.Kind() == reflect.String)
			// See https://github.com/open-telemetry/opentelemetry-collector/issues/10949
			// If the `to.Kind` is not a string, then expandValue's original value is useless and
			// the casted-to value will be nil. In that scenario, we need to use the default value of `to`'s kind.
			if v == nil {
				return reflect.Zero(to).Interface(), nil
			}
			return v, nil
		}

		if !NewExpandedValueSanitizer.IsEnabled() {
			switch to.Kind() {
			case reflect.Array, reflect.Slice, reflect.Map:
				if isStringyStructure(to) {
					// If the target field is a stringy structure, sanitize to use the original string value everywhere.
					return sanitizeToStr(data), nil
				}

				// Otherwise, sanitize to use the parsed value everywhere.
				return sanitize(data), nil
			}
		}
		return data, nil
	}
}

// In cases where a config has a mapping of something to a struct pointers
// we want nil values to resolve to a pointer to the zero value of the
// underlying struct just as we want nil values of a mapping of something
// to a struct to resolve to the zero value of that struct.
//
// e.g. given a config type:
// type Config struct { Thing *SomeStruct `mapstructure:"thing"` }
//
// and yaml of:
// config:
//
//	thing:
//
// we want an unmarshaled Config to be equivalent to
// Config{Thing: &SomeStruct{}} instead of Config{Thing: nil}
func expandNilStructPointersHookFunc() mapstructure.DecodeHookFuncValue {
	return safeWrapDecodeHookFunc(func(from, to reflect.Value) (any, error) {
		// ensure we are dealing with map to map comparison
		if from.Kind() == reflect.Map && to.Kind() == reflect.Map {
			toElem := to.Type().Elem()
			// ensure that map values are pointers to a struct
			// (that may be nil and require manual setting w/ zero value)
			if toElem.Kind() == reflect.Ptr && toElem.Elem().Kind() == reflect.Struct {
				fromRange := from.MapRange()
				for fromRange.Next() {
					fromKey := fromRange.Key()
					fromValue := fromRange.Value()
					// ensure that we've run into a nil pointer instance
					if fromValue.IsNil() {
						newFromValue := reflect.New(toElem.Elem())
						from.SetMapIndex(fromKey, newFromValue)
					}
				}
			}
		}
		return from.Interface(), nil
	})
}

// mapKeyStringToMapKeyTextUnmarshalerHookFunc returns a DecodeHookFuncType that checks that a conversion from
// map[string]any to map[encoding.TextUnmarshaler]any does not overwrite keys,
// when UnmarshalText produces equal elements from different strings (e.g. trims whitespaces).
//
// This is needed in combination with ComponentID, which may produce equal IDs for different strings,
// and an error needs to be returned in that case, otherwise the last equivalent ID overwrites the previous one.
func mapKeyStringToMapKeyTextUnmarshalerHookFunc() mapstructure.DecodeHookFuncType {
	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.Map || from.Key().Kind() != reflect.String {
			return data, nil
		}

		if to.Kind() != reflect.Map {
			return data, nil
		}

		// Checks that the key type of to implements the TextUnmarshaler interface.
		if _, ok := reflect.New(to.Key()).Interface().(encoding.TextUnmarshaler); !ok {
			return data, nil
		}

		// Create a map with key value of to's key to bool.
		fieldNameSet := reflect.MakeMap(reflect.MapOf(to.Key(), reflect.TypeFor[bool]()))
		for k := range data.(map[string]any) {
			// Create a new value of the to's key type.
			tKey := reflect.New(to.Key())

			// Use tKey to unmarshal the key of the map.
			if err := tKey.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(k)); err != nil {
				return nil, err
			}
			// Checks if the key has already been decoded in a previous iteration.
			if fieldNameSet.MapIndex(reflect.Indirect(tKey)).IsValid() {
				return nil, fmt.Errorf("duplicate name %q after unmarshaling %v", k, tKey)
			}
			fieldNameSet.SetMapIndex(reflect.Indirect(tKey), reflect.ValueOf(true))
		}
		return data, nil
	}
}

// unmarshalerEmbeddedStructsHookFunc provides a mechanism for embedded structs to define their own unmarshal logic,
// by implementing the Unmarshaler interface.
func unmarshalerEmbeddedStructsHookFunc(settings UnmarshalOptions) mapstructure.DecodeHookFuncValue {
	return safeWrapDecodeHookFunc(func(from, to reflect.Value) (any, error) {
		if to.Type().Kind() != reflect.Struct {
			return from.Interface(), nil
		}
		fromAsMap, ok := from.Interface().(map[string]any)
		if !ok {
			return from.Interface(), nil
		}

		// First call Unmarshaler on squashed embedded fields, if necessary.
		var squashedUnmarshalers []int
		for i := 0; i < to.Type().NumField(); i++ {
			f := to.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			tagParts := strings.Split(f.Tag.Get(MapstructureTag), ",")
			if !slices.Contains(tagParts[1:], "squash") {
				continue
			}
			unmarshaler, ok := to.Field(i).Addr().Interface().(Unmarshaler)
			if !ok {
				continue
			}
			c := NewFromStringMap(fromAsMap)
			c.skipTopLevelUnmarshaler = true
			if err := unmarshaler.Unmarshal(c); err != nil {
				return nil, err
			}
			squashedUnmarshalers = append(squashedUnmarshalers, i)
		}

		// No squashed unmarshalers, we can let mapstructure do its job.
		if len(squashedUnmarshalers) == 0 {
			return fromAsMap, nil
		}

		// We need to unmarshal into all other fields without overwriting the output of the Unmarshal calls.
		// To do that, create a custom "partial" struct containing only the non-squashed fields.
		var fields []reflect.StructField
		var fieldValues []reflect.Value
		for i := 0; i < to.Type().NumField(); i++ {
			f := to.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			if slices.Contains(squashedUnmarshalers, i) {
				continue
			}
			fields = append(fields, f)
			fieldValues = append(fieldValues, to.Field(i))
		}
		restType := reflect.StructOf(fields)
		restValue := reflect.New(restType)

		// Copy initial values into partial struct.
		for i, fieldValue := range fieldValues {
			restValue.Elem().Field(i).Set(fieldValue)
		}

		// Decode into the partial struct.
		// This performs a recursive call into this hook, which will be handled by the "no squashed unmarshalers" case above.
		// We need to set `IgnoreUnused` to avoid errors from the map containing fields only present in the full struct.
		settings.IgnoreUnused = true
		if err := Decode(fromAsMap, restValue.Interface(), settings, true); err != nil {
			return nil, err
		}

		// Copy decoding results back to the original struct.
		for i, fieldValue := range fieldValues {
			fieldValue.Set(restValue.Elem().Field(i))
		}

		return to, nil
	})
}

// Provides a mechanism for individual structs to define their own unmarshal logic,
// by implementing the Unmarshaler interface, unless skipTopLevelUnmarshaler is
// true and the struct matches the top level object being unmarshaled.
func unmarshalerHookFunc(result any, skipTopLevelUnmarshaler bool) mapstructure.DecodeHookFuncValue {
	return safeWrapDecodeHookFunc(func(from, to reflect.Value) (any, error) {
		if !to.CanAddr() {
			return from.Interface(), nil
		}

		toPtr := to.Addr().Interface()
		// Need to ignore the top structure to avoid running into an infinite recursion
		// where Unmarshaler.Unmarshal and Conf.Unmarshal would call each other.
		if toPtr == result && skipTopLevelUnmarshaler {
			return from.Interface(), nil
		}

		unmarshaler, ok := toPtr.(Unmarshaler)
		if !ok {
			return from.Interface(), nil
		}

		if _, ok = from.Interface().(map[string]any); !ok {
			return from.Interface(), nil
		}

		// Use the current object if not nil (to preserve other configs in the object), otherwise zero initialize.
		if to.Addr().IsNil() {
			unmarshaler = reflect.New(to.Type()).Interface().(Unmarshaler)
		}

		c := NewFromStringMap(from.Interface().(map[string]any))
		c.skipTopLevelUnmarshaler = true
		if err := unmarshaler.Unmarshal(c); err != nil {
			return nil, err
		}

		return unmarshaler, nil
	})
}

// safeWrapDecodeHookFunc wraps a DecodeHookFuncValue to ensure fromVal is a valid `reflect.Value`
// object and therefore it is safe to call `reflect.Value` methods on fromVal.
//
// Use this only if the hook does not need to be called on untyped nil values.
// Typed nil values are safe to call and will be passed to the hook.
// See https://github.com/golang/go/issues/51649
func safeWrapDecodeHookFunc(
	f mapstructure.DecodeHookFuncValue,
) mapstructure.DecodeHookFuncValue {
	return func(fromVal, toVal reflect.Value) (any, error) {
		if !fromVal.IsValid() {
			return nil, nil
		}
		return f(fromVal, toVal)
	}
}

// This hook is used to solve the issue: https://github.com/open-telemetry/opentelemetry-collector/issues/4001
// We adopt the suggestion provided in this issue: https://github.com/mitchellh/mapstructure/issues/74#issuecomment-279886492
// We should empty every slice before unmarshalling unless user provided slice is nil.
// Assume that we had a struct with a field of type slice called `keys`, which has default values of ["a", "b"]
//
//	type Config struct {
//	  Keys []string `mapstructure:"keys"`
//	}
//
// The configuration provided by users may have following cases
// 1. configuration have `keys` field and have a non-nil values for this key, the output should be overridden
//   - for example, input is {"keys", ["c"]}, then output is Config{ Keys: ["c"]}
//
// 2. configuration have `keys` field and have an empty slice for this key, the output should be overridden by empty slices
//   - for example, input is {"keys", []}, then output is Config{ Keys: []}
//
// 3. configuration have `keys` field and have nil value for this key, the output should be default config
//   - for example, input is {"keys": nil}, then output is Config{ Keys: ["a", "b"]}
//
// 4. configuration have no `keys` field specified, the output should be default config
//   - for example, input is {}, then output is Config{ Keys: ["a", "b"]}
//
// This hook is also used to solve https://github.com/open-telemetry/opentelemetry-collector/issues/13117.
// Since v0.127.0, we decode nil values to avoid creating empty map objects.
// The nil value is not well understood when layered on top of a default map non-nil value.
// The fix is to avoid the assignment and return the previous value.
func zeroSliceAndMapHookFunc() mapstructure.DecodeHookFuncValue {
	return safeWrapDecodeHookFunc(func(from, to reflect.Value) (any, error) {
		if to.CanSet() && to.Kind() == reflect.Slice && from.Kind() == reflect.Slice {
			if !from.IsNil() {
				// input slice is not nil, set the output slice to a new slice of the same type.
				to.Set(reflect.MakeSlice(to.Type(), from.Len(), from.Cap()))
			}
		}
		if to.CanSet() && to.Kind() == reflect.Map && from.Kind() == reflect.Map {
			if from.IsNil() {
				return to.Interface(), nil
			}
		}

		return from.Interface(), nil
	})
}

// case-sensitive version of the callback to be used in the MatchName property
// of the DecoderConfig. The default for MatchEqual is to use strings.EqualFold,
// which is case-insensitive.
func caseSensitiveMatchName(a, b string) bool {
	return a == b
}

func castTo(exp ExpandedValue, useOriginal bool) any {
	// If the target field is a string, use `exp.Original` or fail if not available.
	if useOriginal {
		return exp.Original
	}
	// Otherwise, use the parsed value (previous behavior).
	return exp.Value
}

// Check if a reflect.Type is of the form T, where:
// X is any type or interface
// T = string | map[X]T | []T | [n]T
func isStringyStructure(t reflect.Type) bool {
	if t.Kind() == reflect.String {
		return true
	}
	if t.Kind() == reflect.Map {
		return isStringyStructure(t.Elem())
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return isStringyStructure(t.Elem())
	}
	return false
}
